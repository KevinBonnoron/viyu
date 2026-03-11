import type { Request, Response, Status } from './messages';
import type { ErrorKey } from './state';

const message = (key: string, substitutions?: string[]): string => chrome.i18n.getMessage(key, substitutions) || key;
const errorMessage = (key: ErrorKey): string => message(`error${key[0]?.toUpperCase()}${key.slice(1)}`);
const dates = new Intl.DateTimeFormat(chrome.i18n.getUILanguage(), { dateStyle: 'medium', timeStyle: 'short' });

function byId<T extends HTMLElement>(id: string): T {
  const element = document.getElementById(id);
  if (!element) {
    throw new Error(`missing #${id}`);
  }
  // The popup's own markup defines these ids with the element types asked for.
  return element as T;
}

const notice = byId<HTMLParagraphElement>('notice');
const unpaired = byId<HTMLElement>('unpaired');
const paired = byId<HTMLElement>('paired');
const pairForm = byId<HTMLFormElement>('pair-form');
const pairing = byId<HTMLTextAreaElement>('pairing');
const syncButton = byId<HTMLButtonElement>('sync');
const unpairButton = byId<HTMLButtonElement>('unpair');
let poll: ReturnType<typeof setTimeout> | undefined;
/** The status last rendered, so a click can show its own effect before the background answers. */
let current: Status = { syncing: false, unpairedNotice: false };

for (const element of document.querySelectorAll<HTMLElement>('[data-i18n]')) {
  element.textContent = message(element.dataset.i18n ?? '');
}
document.title = message('extensionName');
document.documentElement.lang = chrome.i18n.getUILanguage();

function send(request: Request): Promise<Response> {
  return chrome.runtime.sendMessage(request);
}

function showNotice(text: string | undefined, error: boolean): void {
  notice.hidden = !text;
  notice.textContent = text ?? '';
  notice.classList.toggle('error', error);
}

function render(status: Status): void {
  current = status;
  const connection = status.connection;
  unpaired.hidden = connection !== undefined;
  paired.hidden = connection === undefined;
  if (!connection) {
    return;
  }
  byId('profile').textContent = `${connection.profileName} (${connection.deviceName})`;
  byId('server').textContent = connection.server.replace(/^https?:\/\//, '');
  const last = status.lastSync;
  byId('last-sync').textContent = status.syncing ? message('syncing') : last ? dates.format(new Date(last.at)) : message('neverSynced');
  const summary = byId<HTMLParagraphElement>('summary');
  const report = !status.syncing ? last?.report : undefined;
  summary.hidden = !report;
  if (report) {
    summary.textContent = message('syncSummary', [
      String(report.subscriptions.total),
      String(report.subscriptions.created),
      String(report.subscriptions.removed),
      String(report.history.created),
      String(report.history.updated),
    ]);
  }
  syncButton.disabled = status.syncing;
  unpairButton.disabled = status.syncing;
  if (!status.syncing && last?.error) {
    showNotice([errorMessage(last.error), last.detail].filter(Boolean).join(' '), true);
  }
  clearTimeout(poll);
  if (status.syncing) {
    poll = setTimeout(() => void refresh(), 1000);
  }
}

async function refresh(): Promise<void> {
  const response = await send({ type: 'status' });
  if (!response.status.syncing && !response.status.lastSync?.error) {
    showNotice(undefined, false);
  }
  render(response.status);
}

function handle(response: Response): void {
  if (response.ok) {
    showNotice(undefined, false);
  } else {
    showNotice([errorMessage(response.error), response.detail].filter(Boolean).join(' '), true);
  }
  render(response.status);
}

pairForm.addEventListener('submit', (event) => {
  event.preventDefault();
  const button = pairForm.querySelector('button');
  if (button) {
    button.disabled = true;
  }
  void send({ type: 'pair', pairing: pairing.value })
    .then((response) => {
      if (response.ok) {
        pairing.value = '';
      }
      handle(response);
    })
    .finally(() => {
      if (button) {
        button.disabled = false;
      }
    });
});

syncButton.addEventListener('click', () => {
  showNotice(undefined, false);
  render({ ...current, syncing: true });
  void send({ type: 'sync' }).then(handle);
});

unpairButton.addEventListener('click', () => {
  if (confirm(message('unpairConfirm'))) {
    void send({ type: 'unpair' }).then(handle);
  }
});

void send({ type: 'status' }).then((response) => {
  if (response.status.unpairedNotice && !response.status.connection) {
    showNotice(errorMessage('unpaired'), true);
  }
  render(response.status);
});
