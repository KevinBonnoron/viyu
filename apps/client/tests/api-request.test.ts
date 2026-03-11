import { path, queryString, readResponse, requestInit } from '../src/lib/api-request';
import { expect, test } from 'bun:test';

test('a query drops undefined, null, false and empty values', () => {
  expect(queryString({ a: undefined, b: null, c: false, d: '' })).toBe('');
  expect(queryString()).toBe('');
  expect(queryString({ q: 'x', source: undefined, platform: '' })).toBe('?q=x');
});

test('a true flag is 1 and numbers are stringified, zero included', () => {
  expect(queryString({ sync: true, retry: false, page: 2, offset: 0 })).toBe('?sync=1&page=2&offset=0');
});

test('query values are encoded', () => {
  expect(queryString({ q: 'a b&c=d' })).toBe('?q=a+b%26c%3Dd');
});

test('a path encodes every interpolated segment', () => {
  expect(path`/video/${'a/b c'}/url`).toBe('/video/a%2Fb%20c/url');
  expect(path`/sources/${'twitch'}`).toBe('/sources/twitch');
  expect(path`/series/${12}/${'x?y'}`).toBe('/series/12/x%3Fy');
  expect(path`/plain`).toBe('/plain');
});

test('an object body is JSON-encoded and declared as JSON', () => {
  const init = requestInit({ method: 'POST', body: { url: 'https://example.test', skipped: undefined } });
  expect(init.body).toBe('{"url":"https://example.test"}');
  expect(new Headers(init.headers).get('Content-Type')).toBe('application/json');
  expect(init.method).toBe('POST');
});

test('a string body is sent as it is, still declared as JSON', () => {
  const init = requestInit({ method: 'POST', body: '{"profiles":[]}' });
  expect(init.body).toBe('{"profiles":[]}');
  expect(new Headers(init.headers).get('Content-Type')).toBe('application/json');
});

test('a request without a body declares no content type', () => {
  expect(new Headers(requestInit({}).headers).has('Content-Type')).toBe(false);
});

test('the session is a Bearer token and never a cookie', () => {
  const signed = requestInit({}, 'token');
  expect(new Headers(signed.headers).get('Authorization')).toBe('Bearer token');
  expect(signed.credentials).toBe('omit');
  const anonymous = requestInit({});
  expect(new Headers(anonymous.headers).has('Authorization')).toBe(false);
  expect(anonymous.credentials).toBe('omit');
});

test('signal and cache pass through', () => {
  const signal = new AbortController().signal;
  const init = requestInit({ signal, cache: 'no-store' });
  expect(init.signal).toBe(signal);
  expect(init.cache).toBe('no-store');
});

test('a response reads as JSON by default and as a blob on request', async () => {
  expect(await readResponse(new Response('{"ok":true}'))).toEqual({ ok: true });
  const blob = (await readResponse(new Response('archive'), 'blob')) as Blob;
  expect(blob).toBeInstanceOf(Blob);
  expect(await blob.text()).toBe('archive');
});

test('a 204 reads as undefined', async () => {
  expect(await readResponse(new Response(null, { status: 204 }))).toBeUndefined();
});
