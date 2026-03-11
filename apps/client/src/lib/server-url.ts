export class ServerConfigError extends Error {
  public constructor(public readonly code: 'invalid' | 'unreachable' | 'storage') {
    super(code);
    this.name = 'ServerConfigError';
  }
}

/**
 * The server base a typed address stands for: any `http` or `https` address (`https://` is assumed when no scheme is
 * typed), without credentials, query or fragment.
 * A path prefix is kept (`https://host/viyu`): every request is built relative to the base, so a server mounted under a
 * prefix keeps working. Trailing slashes are dropped.
 */
export function normalizeServerUrl(value: string): string {
  const typed = value.trim();
  let url: URL;
  try {
    url = new URL(/^[a-z][a-z0-9+.-]*:\/\//i.test(typed) ? typed : `https://${typed}`);
  } catch {
    throw new ServerConfigError('invalid');
  }
  if (url.username || url.password || url.search || url.hash || (url.protocol !== 'https:' && url.protocol !== 'http:')) {
    throw new ServerConfigError('invalid');
  }
  return url.toString().replace(/\/+$/, '');
}

/** An absolute URL for a server path (`/api/health`) under a base, keeping the base's own path prefix. */
export function joinServerUrl(base: string, path: string): string {
  return new URL(path.replace(/^\/+/, ''), `${base.replace(/\/+$/, '')}/`).toString();
}
