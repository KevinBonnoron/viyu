import { ApiError, apiErrorFrom, requestError } from '../src/lib/api-error';
import { expect, test } from 'bun:test';

test('a custom route error carries its code and message', () => {
  const error = apiErrorFrom(409, { code: 'ADMIN_LAST_ADMIN', message: 'The last administrator cannot be removed.' });
  expect(error).toBeInstanceOf(ApiError);
  expect(error.code).toBe('ADMIN_LAST_ADMIN');
  expect(error.status).toBe(409);
  expect(error.message).toBe('The last administrator cannot be removed.');
});

test('an `error` field is not an error message: only `message` is read', () => {
  const error = apiErrorFrom(400, { code: 'INVALID_SOURCE_INPUT', error: 'bad cookies' });
  expect(error.code).toBe('INVALID_SOURCE_INPUT');
  expect(error.message).toBe('INVALID_SOURCE_INPUT');
});

test('a body without a code falls back to a generic one and keeps the status', () => {
  const error = apiErrorFrom(502, { detail: 'upstream' });
  expect(error.code).toBe('REQUEST_FAILED');
  expect(error.status).toBe(502);
  expect(error.message).toBe('REQUEST_FAILED');
});

test('a body that is not an object is ignored', () => {
  for (const body of [null, undefined, 'oops', 42, ['code']]) {
    expect(apiErrorFrom(500, body).code).toBe('REQUEST_FAILED');
  }
  expect(apiErrorFrom(500, null, 'EXPORT_FAILED').code).toBe('EXPORT_FAILED');
});

test('non-string or empty fields are ignored', () => {
  const error = apiErrorFrom(400, { code: 12, message: '' });
  expect(error.code).toBe('REQUEST_FAILED');
  expect(error.message).toBe('REQUEST_FAILED');
});

test('a failure maps to the translation that explains it', () => {
  expect(requestError(new ApiError('SOURCE_SESSION_INVALID', 502))).toBe('errors.sourceSessionInvalid');
  expect(requestError(new ApiError('REQUEST_FAILED', 401))).toBe('errors.sessionExpired');
  expect(requestError(new ApiError('REQUEST_FAILED', 500))).toBe('errors.requestFailed');
  expect(requestError(new ApiError('toString', 500))).toBe('errors.requestFailed');
  expect(requestError(new TypeError('network'))).toBe('errors.serverUnreachable');
});

test('Retry-After accepts seconds or an HTTP date, ignoring invalid and elapsed delays', () => {
  expect(apiErrorFrom(429, null, 'REQUEST_FAILED', '90').retryAfterMs).toBe(90000);
  const later = new Date(Date.now() + 120000).toUTCString();
  const remaining = apiErrorFrom(503, null, 'REQUEST_FAILED', later).retryAfterMs;
  expect(remaining).toBeGreaterThan(118000);
  expect(remaining).toBeLessThanOrEqual(120000);
  for (const value of [null, '', 'bad', '-10', 'Infinity', 'Thu, 01 Jan 1970 00:00:00 GMT']) {
    expect(apiErrorFrom(429, null, 'REQUEST_FAILED', value).retryAfterMs).toBe(0);
  }
});
