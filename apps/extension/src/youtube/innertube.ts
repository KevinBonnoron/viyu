/** One browse request of YouTube's InnerTube API: a first page (`browseId`) or the next one (`continuation`). */
export interface BrowseRequest {
  browseId?: string;
  continuation?: string;
}

export type BrowseResult = { ok: true; data: unknown } | { ok: false; error: 'noConfig' | 'signedOut' | 'http'; status?: number };

declare global {
  interface Window {
    /** The configuration youtube.com keeps on its pages; read with `get`, or `data_` on older builds. */
    ytcfg?: { get?: (key: string) => unknown; data_?: Record<string, unknown> };
  }
}

/**
 * Runs inside a youtube.com page (`world: 'MAIN'`), as the page itself would: same origin, the browser's own cookies,
 * and the `SAPISIDHASH` authorization youtube.com computes from them. The cookies are read here and never leave the
 * page; only the JSON answer is returned. The browser serializes this function into the page, so it must not refer to
 * anything outside its own body. History headers are asked in English (`hl=en`) so their dates can be read.
 */
export async function innertubeBrowse(request: BrowseRequest): Promise<BrowseResult> {
  const config = window.ytcfg;
  const get = (key: string): unknown => config?.get?.(key) ?? config?.data_?.[key];
  const text = (key: string): string => {
    const value = get(key);
    return typeof value === 'string' ? value : '';
  };
  const context = get('INNERTUBE_CONTEXT');
  if (typeof context !== 'object' || context === null || !('client' in context) || typeof context.client !== 'object' || context.client === null) {
    return { ok: false, error: 'noConfig' };
  }
  if (get('LOGGED_IN') !== true) {
    return { ok: false, error: 'signedOut' };
  }

  const cookies = new Map<string, string>();
  for (const pair of document.cookie.split('; ')) {
    const at = pair.indexOf('=');
    if (at > 0) {
      cookies.set(pair.slice(0, at), pair.slice(at + 1));
    }
  }
  const origin = location.origin;
  const [first = '', second = ''] = text('DATASYNC_ID').split('||');
  const delegatedSessionId = text('DELEGATED_SESSION_ID') || (second ? first : '');
  const userSessionId = text('USER_SESSION_ID') || (second ? second : first);
  const timestamp = String(Math.round(Date.now() / 1000));
  const sha1 = async (value: string): Promise<string> => {
    const digest = await crypto.subtle.digest('SHA-1', new TextEncoder().encode(value));
    return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, '0')).join('');
  };
  const sapisid = cookies.get('SAPISID') || cookies.get('__Secure-3PAPISID');
  const authorizations: string[] = [];
  for (const [scheme, sid] of [
    ['SAPISIDHASH', sapisid],
    ['SAPISID1PHASH', cookies.get('__Secure-1PAPISID')],
    ['SAPISID3PHASH', cookies.get('__Secure-3PAPISID')],
  ] as const) {
    if (!sid) {
      continue;
    }
    const parts = userSessionId ? [userSessionId, timestamp, sid, origin] : [timestamp, sid, origin];
    const hash = await sha1(parts.join(' '));
    authorizations.push(`${scheme} ${timestamp}_${hash}${userSessionId ? '_u' : ''}`);
  }
  if (authorizations.length === 0) {
    return { ok: false, error: 'signedOut' };
  }

  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    Authorization: authorizations.join(' '),
    'X-Origin': origin,
    'X-Goog-AuthUser': String(get('SESSION_INDEX') ?? 0),
    'X-Youtube-Bootstrap-Logged-In': 'true',
  };
  const clientName = get('INNERTUBE_CONTEXT_CLIENT_NAME');
  const clientVersion = get('INNERTUBE_CLIENT_VERSION');
  const visitorData = get('VISITOR_DATA');
  if (clientName !== undefined) {
    headers['X-Youtube-Client-Name'] = String(clientName);
  }
  if (typeof clientVersion === 'string') {
    headers['X-Youtube-Client-Version'] = clientVersion;
  }
  if (typeof visitorData === 'string') {
    headers['X-Goog-Visitor-Id'] = visitorData;
  }
  if (delegatedSessionId) {
    headers['X-Goog-PageId'] = delegatedSessionId;
  }
  const key = get('INNERTUBE_API_KEY');
  const url = `/youtubei/v1/browse?prettyPrint=false${typeof key === 'string' ? `&key=${encodeURIComponent(key)}` : ''}`;
  const body = JSON.stringify({ ...request, context: { ...context, client: { ...context.client, hl: 'en' } } });
  const response = await fetch(url, { method: 'POST', credentials: 'include', headers, body });
  if (!response.ok) {
    return { ok: false, error: response.status === 401 || response.status === 403 ? 'signedOut' : 'http', status: response.status };
  }
  return { ok: true, data: await response.json() };
}
