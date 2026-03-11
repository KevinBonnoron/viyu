import type { SyncReport } from './sync';

/** The Viyu server and device this extension is paired with. The token is the only credential it holds for Viyu. */
export interface Connection {
  server: string;
  token: string;
  deviceName: string;
  profileName: string;
}

/** The outcome of the last synchronization, shown in the popup. `error` is a message key, `detail` Viyu's own words. */
export interface LastSync {
  at: string;
  report?: SyncReport;
  error?: ErrorKey;
  detail?: string;
}

export type ErrorKey = 'signedOut' | 'youtubeUnavailable' | 'viyuUnreachable' | 'viyuRefused' | 'unpaired' | 'pairingInvalid';

export interface State {
  connection?: Connection;
  lastSync?: LastSync;
  historyComplete?: boolean;
  /** The last message of a revoked pairing, kept after the connection is dropped so the popup can say why. */
  unpairedNotice?: boolean;
}

export async function loadState(): Promise<State> {
  // Only this extension writes its local storage, always in this shape.
  const stored: State = await chrome.storage.local.get(['connection', 'lastSync', 'historyComplete', 'unpairedNotice']);
  return stored;
}

export async function saveState(state: Partial<State>): Promise<void> {
  await chrome.storage.local.set(state);
}

export async function clearConnection(notice: boolean): Promise<void> {
  await chrome.storage.local.remove(['connection', 'lastSync', 'historyComplete']);
  await chrome.storage.local.set({ unpairedNotice: notice });
}
