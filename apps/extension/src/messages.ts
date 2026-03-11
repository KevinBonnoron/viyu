import type { Connection, ErrorKey, LastSync } from './state';

/** What the popup asks the background. */
export type Request = { type: 'status' } | { type: 'pair'; pairing: string } | { type: 'sync' } | { type: 'unpair' };

/** What the popup may show: never the token. */
export interface Status {
  connection?: Omit<Connection, 'token'>;
  lastSync?: LastSync;
  syncing: boolean;
  unpairedNotice: boolean;
}

export type Response = { ok: true; status: Status } | { ok: false; error: ErrorKey; detail?: string; status: Status };
