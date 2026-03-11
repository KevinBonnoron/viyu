import { isRecord } from './guards';
import type { HistoryEntry } from './youtube/parse';

/** The device as Viyu describes it to the extension. */
export interface Device {
  id: string;
  name: string;
  profileName: string;
}

export interface SubscriptionCounts {
  created: number;
  removed: number;
  unchanged: number;
  skipped: number;
}

export interface HistoryCounts {
  created: number;
  updated: number;
  unchanged: number;
  skipped: number;
}

/** A failed call to Viyu: the HTTP status (0 when the server could not be reached) and Viyu's error code and message. */
export class ViyuError extends Error {
  public constructor(
    public readonly status: number,
    public readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

const tokenHeader = 'X-Viyu-Companion';

async function call(server: string, path: string, init: { method: string; body?: unknown; token?: string }): Promise<unknown> {
  const headers: Record<string, string> = {};
  if (init.body !== undefined) {
    headers['Content-Type'] = 'application/json';
  }
  if (init.token) {
    headers[tokenHeader] = init.token;
  }
  let response: Response;
  try {
    response = await fetch(`${server}${path}`, {
      method: init.method,
      headers,
      body: init.body === undefined ? undefined : JSON.stringify(init.body),
      credentials: 'omit',
      cache: 'no-store',
    });
  } catch {
    throw new ViyuError(0, 'NETWORK', 'Viyu could not be reached.');
  }
  const payload: unknown = await response.json().catch(() => undefined);
  if (!response.ok) {
    const code = isRecord(payload) && typeof payload.code === 'string' ? payload.code : 'HTTP';
    const message = isRecord(payload) && typeof payload.message === 'string' ? payload.message : `HTTP ${response.status}`;
    throw new ViyuError(response.status, code, message);
  }
  return payload;
}

function device(value: unknown): Device {
  if (!isRecord(value) || typeof value.id !== 'string' || typeof value.name !== 'string' || typeof value.profileName !== 'string') {
    throw new ViyuError(200, 'BAD_RESPONSE', 'Viyu answered with an unexpected device.');
  }
  return { id: value.id, name: value.name, profileName: value.profileName };
}

function counts<K extends string>(value: unknown, keys: readonly K[]): Record<K, number> {
  if (!isRecord(value)) {
    throw new ViyuError(200, 'BAD_RESPONSE', 'Viyu answered with unexpected counts.');
  }
  const result = {} as Record<K, number>;
  for (const key of keys) {
    const count = value[key];
    result[key] = typeof count === 'number' ? count : 0;
  }
  return result;
}

/** Exchanges a pairing code for the device token, which only this extension keeps. */
export async function pair(server: string, code: string, name: string): Promise<{ token: string; device: Device }> {
  const payload = await call(server, '/api/companion/pair', { method: 'POST', body: { code, name } });
  if (!isRecord(payload) || typeof payload.token !== 'string' || payload.token === '') {
    throw new ViyuError(200, 'BAD_RESPONSE', 'Viyu answered without a token.');
  }
  return { token: payload.token, device: device(payload.device) };
}

/** The device the token belongs to; a revoked token fails with status 401. */
export async function session(server: string, token: string): Promise<Device> {
  const payload = await call(server, '/api/companion/session', { method: 'GET', token });
  return device(isRecord(payload) ? payload.device : undefined);
}

export async function pushSubscriptions(server: string, token: string, channels: string[], complete: boolean): Promise<SubscriptionCounts> {
  const payload = await call(server, '/api/companion/youtube/subscriptions', { method: 'POST', token, body: { channels, complete } });
  return counts(payload, ['created', 'removed', 'unchanged', 'skipped']);
}

/** Tells Viyu to forget this device, so its token stops working there too. */
export async function forget(server: string, token: string): Promise<void> {
  await call(server, '/api/companion/session', { method: 'DELETE', token });
}

export async function pushHistory(server: string, token: string, entries: HistoryEntry[]): Promise<HistoryCounts> {
  const payload = await call(server, '/api/companion/youtube/history', { method: 'POST', token, body: { entries } });
  return counts(payload, ['created', 'updated', 'unchanged', 'skipped']);
}
