/** A query-string value: `undefined`, `null`, `false` and `''` leave the parameter out, `true` is `1`, anything else is stringified. */
export type QueryValue = string | number | boolean | null | undefined;
export type Query = Record<string, QueryValue>;

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
  query?: Query;
  /** A string is sent as is; anything else is JSON-encoded. Either way the request is declared as JSON. */
  body?: string | object;
  signal?: AbortSignal;
  cache?: RequestCache;
  /** How a successful answer is read: `json` (the default; a 204 reads as `undefined`) or the raw `blob` of a download. */
  responseType?: 'json' | 'blob';
}

/** The `?a=1&b=2` suffix of a query, or `''` when no parameter is left. */
export function queryString(query: Query = {}): string {
  const params = new URLSearchParams();
  for (const [name, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === false || value === '') {
      continue;
    }
    params.set(name, value === true ? '1' : String(value));
  }
  const encoded = params.toString();
  return encoded ? `?${encoded}` : '';
}

/** Tagged template for a URL path: every interpolated value is encoded as one path segment. */
export function path(strings: TemplateStringsArray, ...segments: (string | number)[]): string {
  return strings.reduce((url, part, index) => url + encodeURIComponent(String(segments[index - 1])) + part);
}

/** The `fetch` init of a request: the session travels as a Bearer token only, never as a cookie, so cross-origin native clients behave like the web one. */
export function requestInit({ method, body, signal, cache }: RequestOptions, token?: string): RequestInit {
  const headers = new Headers();
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  if (body !== undefined) {
    headers.set('Content-Type', 'application/json');
  }
  return { method, headers, body: typeof body === 'object' ? JSON.stringify(body) : body, signal, cache, credentials: 'omit' };
}

/** The successful answer in the requested shape. */
export function readResponse(response: Response, responseType: RequestOptions['responseType'] = 'json'): Promise<unknown> {
  if (responseType === 'blob') {
    return response.blob();
  }
  return response.status === 204 ? Promise.resolve(undefined) : response.json();
}
