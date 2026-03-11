import { isRecord } from './guards';

/** What a Viyu pairing string carries: the server to talk to (its base, path prefix included) and a single-use code. */
export interface Pairing {
  server: string;
  code: string;
}

/** The reason a pasted pairing string cannot be used, as a message key of the popup. */
export class PairingError extends Error {
  public constructor(public readonly key: 'pairingInvalid') {
    super(key);
  }
}

const prefix = 'VIYU-PAIR:';

/**
 * Reads the pairing string Viyu shows on its Sources page: `VIYU-PAIR:` followed by the unpadded base64url encoding of
 * `{"v":1,"server":"…","code":"…"}`. Only an http(s) server is accepted, so a pasted string can never make the extension
 * send its token anywhere else; the server keeps its path prefix and loses any trailing slash, query or fragment.
 */
export function parsePairing(input: string): Pairing {
  const text = input.trim();
  if (!text.startsWith(prefix)) {
    throw new PairingError('pairingInvalid');
  }
  let payload: unknown;
  try {
    payload = JSON.parse(decodeBase64Url(text.slice(prefix.length)));
  } catch {
    throw new PairingError('pairingInvalid');
  }
  if (!isRecord(payload) || payload.v !== 1 || typeof payload.server !== 'string' || typeof payload.code !== 'string') {
    throw new PairingError('pairingInvalid');
  }
  if (!/^[A-Za-z0-9_-]{1,200}$/.test(payload.code)) {
    throw new PairingError('pairingInvalid');
  }
  let server: URL;
  try {
    server = new URL(payload.server);
  } catch {
    throw new PairingError('pairingInvalid');
  }
  if ((server.protocol !== 'https:' && server.protocol !== 'http:') || server.username || server.password) {
    throw new PairingError('pairingInvalid');
  }
  return { server: `${server.origin}${server.pathname.replace(/\/+$/, '')}`, code: payload.code };
}

function decodeBase64Url(value: string): string {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) {
    throw new Error('not base64url');
  }
  const base64 = value
    .replaceAll('-', '+')
    .replaceAll('_', '/')
    .padEnd(Math.ceil(value.length / 4) * 4, '=');
  const bytes = Uint8Array.from(atob(base64), (char) => char.charCodeAt(0));
  return new TextDecoder('utf-8', { fatal: true }).decode(bytes);
}
