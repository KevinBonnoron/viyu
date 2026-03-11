import type { Request, Response, Status } from './messages';
import { parsePairing } from './pairing';
import { clearConnection, type ErrorKey, loadState, saveState } from './state';
import { synchronize } from './sync';
import { forget, pair, pushHistory, pushSubscriptions, ViyuError } from './viyu';
import { type BrowseRequest, innertubeBrowse } from './youtube/innertube';

const youtubeTabs = 'https://www.youtube.com/*';
/** An automatic synchronization runs at most this often, and only while a YouTube tab is open. */
const autoSyncInterval = 6 * 60 * 60 * 1000;
const tabLoadTimeout = 30_000;

/** A synchronization step that failed, with the popup message key that explains it. */
class SyncFailure extends Error {
  public constructor(
    public readonly key: ErrorKey,
    public readonly detail?: string,
  ) {
    super(key);
  }
}

let running: Promise<Response> | undefined;

async function status(): Promise<Status> {
  const state = await loadState();
  const connection = state.connection
    ? { server: state.connection.server, deviceName: state.connection.deviceName, profileName: state.connection.profileName }
    : undefined;
  return { connection, lastSync: state.lastSync, syncing: running !== undefined, unpairedNotice: state.unpairedNotice === true };
}

async function failure(error: ErrorKey, detail?: string): Promise<Response> {
  return { ok: false, error, detail, status: await status() };
}

/** Maps any failure to what the popup says about it. A token Viyu refuses means the device was revoked there. */
function classify(error: unknown): SyncFailure {
  if (error instanceof SyncFailure) {
    return error;
  }
  if (error instanceof ViyuError) {
    if (error.status === 401) {
      return new SyncFailure('unpaired');
    }
    return error.status === 0 ? new SyncFailure('viyuUnreachable') : new SyncFailure('viyuRefused', error.message);
  }
  return new SyncFailure('youtubeUnavailable', error instanceof Error ? error.message : undefined);
}

/** A loaded tab of the YouTube site, where the browse requests can run as the signed-in page. */
async function findYoutubeTab(): Promise<number | undefined> {
  const tabs = await chrome.tabs.query({ url: youtubeTabs, status: 'complete' });
  return tabs.find((tab) => tab.id !== undefined && !tab.discarded)?.id;
}

/** Opens YouTube's home page in a background tab, for a synchronization the user asked for. */
async function openYoutubeTab(): Promise<number> {
  const tab = await chrome.tabs.create({ url: 'https://www.youtube.com/', active: false });
  if (tab.id === undefined) {
    throw new SyncFailure('youtubeUnavailable');
  }
  return tab.id;
}

/**
 * Waits until the tab has loaded, and checks it stayed on youtube.com: a sign-in or consent page elsewhere (which the
 * extension may not read, nor wants to) means the browser has no usable YouTube session.
 */
async function waitForYoutube(id: number): Promise<void> {
  await new Promise<void>((resolve, reject) => {
    const listener = (tabId: number, info: { status?: string }): void => {
      if (tabId === id && info.status === 'complete') {
        clearTimeout(timer);
        chrome.tabs.onUpdated.removeListener(listener);
        resolve();
      }
    };
    const timer = setTimeout(() => {
      chrome.tabs.onUpdated.removeListener(listener);
      reject(new SyncFailure('youtubeUnavailable'));
    }, tabLoadTimeout);
    chrome.tabs.onUpdated.addListener(listener);
  });
  const { url } = await chrome.tabs.get(id);
  if (!url?.startsWith('https://www.youtube.com/')) {
    throw new SyncFailure('signedOut');
  }
}

async function browseIn(tabId: number, request: BrowseRequest): Promise<unknown> {
  let results: chrome.scripting.InjectionResult<Awaited<ReturnType<typeof innertubeBrowse>>>[];
  try {
    results = await chrome.scripting.executeScript({ target: { tabId }, world: 'MAIN', func: innertubeBrowse, args: [request] });
  } catch (error) {
    throw new SyncFailure('youtubeUnavailable', error instanceof Error ? error.message : undefined);
  }
  const result = results[0]?.result;
  if (!result) {
    throw new SyncFailure('youtubeUnavailable');
  }
  if (!result.ok) {
    throw new SyncFailure(result.error === 'signedOut' ? 'signedOut' : 'youtubeUnavailable', result.status ? `HTTP ${result.status}` : undefined);
  }
  return result.data;
}

async function runSync(manual: boolean): Promise<Response> {
  const state = await loadState();
  const connection = state.connection;
  if (!connection) {
    return failure('unpaired');
  }
  let tabId = await findYoutubeTab();
  let opened = false;
  try {
    if (tabId === undefined) {
      if (!manual) {
        return { ok: true, status: await status() };
      }
      tabId = await openYoutubeTab();
      opened = true;
      await waitForYoutube(tabId);
    }
    const tab = tabId;
    const report = await synchronize(
      {
        browse: (request) => browseIn(tab, request),
        pushSubscriptions: (channels, complete) => pushSubscriptions(connection.server, connection.token, channels, complete),
        pushHistory: (entries) => pushHistory(connection.server, connection.token, entries),
        now: () => new Date(),
      },
      { historyComplete: state.historyComplete === true },
    );
    await saveState({ lastSync: { at: new Date().toISOString(), report }, historyComplete: report.historyComplete });
    return { ok: true, status: await status() };
  } catch (error) {
    const reason = classify(error);
    if (reason.key === 'unpaired') {
      await clearConnection(true);
    } else {
      await saveState({ lastSync: { at: new Date().toISOString(), error: reason.key, detail: reason.detail } });
    }
    return failure(reason.key, reason.detail);
  } finally {
    if (opened && tabId !== undefined) {
      await chrome.tabs.remove(tabId).catch(() => undefined);
    }
  }
}

/** One synchronization at a time: a request while one runs joins it. */
function sync(manual: boolean): Promise<Response> {
  running ??= runSync(manual).finally(() => {
    running = undefined;
  });
  return running;
}

/** A recognizable name for the device in Viyu's list of paired extensions. */
function deviceName(): string {
  const agent = navigator.userAgent;
  const browser = /Firefox\//.test(agent) ? 'Firefox' : /Edg\//.test(agent) ? 'Edge' : /Chrome\//.test(agent) ? 'Chrome' : 'Browser';
  const system = /Android/.test(agent) ? 'Android' : /Windows/.test(agent) ? 'Windows' : /Mac OS/.test(agent) ? 'macOS' : /Linux/.test(agent) ? 'Linux' : '';
  return system ? `${browser} · ${system}` : browser;
}

async function pairWith(text: string): Promise<Response> {
  let pairing: ReturnType<typeof parsePairing>;
  try {
    pairing = parsePairing(text);
  } catch {
    return failure('pairingInvalid');
  }
  try {
    const { token, device } = await pair(pairing.server, pairing.code, deviceName());
    await clearConnection(false);
    await saveState({ connection: { server: pairing.server, token, deviceName: device.name, profileName: device.profileName } });
  } catch (error) {
    if (error instanceof ViyuError && error.status !== 0) {
      return failure('pairingInvalid', error.message);
    }
    return failure('viyuUnreachable');
  }
  // The first synchronization starts at once; the popup follows it through the status.
  void sync(true);
  return { ok: true, status: await status() };
}

async function unpair(): Promise<Response> {
  const { connection } = await loadState();
  if (connection) {
    // Viyu forgets the device too when it can be reached; the local token goes either way.
    await forget(connection.server, connection.token).catch(() => undefined);
  }
  await clearConnection(false);
  return { ok: true, status: await status() };
}

async function handle(request: Request): Promise<Response> {
  switch (request.type) {
    case 'status':
      return { ok: true, status: await status() };
    case 'pair':
      return pairWith(request.pairing);
    case 'sync':
      return sync(true);
    case 'unpair':
      return unpair();
  }
}

async function autoSync(): Promise<void> {
  const state = await loadState();
  if (!state.connection || running) {
    return;
  }
  const last = state.lastSync ? Date.parse(state.lastSync.at) : 0;
  if (Date.now() - last >= autoSyncInterval) {
    await sync(false);
  }
}

function scheduleAlarm(): void {
  void chrome.alarms.create('sync', { periodInMinutes: 60 });
}

chrome.runtime.onInstalled.addListener(scheduleAlarm);
chrome.runtime.onStartup.addListener(scheduleAlarm);
chrome.alarms.onAlarm.addListener((alarm) => {
  if (alarm.name === 'sync') {
    void autoSync();
  }
});
chrome.tabs.onUpdated.addListener((_tabId, info, tab) => {
  if (info.status === 'complete' && tab.url?.startsWith('https://www.youtube.com/')) {
    void autoSync();
  }
});
chrome.runtime.onMessage.addListener((request: Request, sender, respond) => {
  if (sender.id !== chrome.runtime.id) {
    return false;
  }
  void handle(request).then(respond);
  return true;
});
