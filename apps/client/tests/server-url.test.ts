import { joinServerUrl, normalizeServerUrl, ServerConfigError } from '../src/lib/server-url';
import { describe, expect, test } from 'bun:test';

const rejected = (value: string) => {
  try {
    normalizeServerUrl(value);
  } catch (failure) {
    return failure instanceof ServerConfigError && failure.code === 'invalid';
  }
  return false;
};

describe('normalizeServerUrl', () => {
  test('keeps an https origin and drops trailing slashes', () => {
    expect(normalizeServerUrl('https://viyu.example.com/')).toBe('https://viyu.example.com');
    expect(normalizeServerUrl('  https://viyu.example.com  ')).toBe('https://viyu.example.com');
  });

  test('keeps a path prefix the server is mounted under', () => {
    expect(normalizeServerUrl('https://example.com/viyu')).toBe('https://example.com/viyu');
    expect(normalizeServerUrl('https://example.com/apps/viyu///')).toBe('https://example.com/apps/viyu');
  });

  test('accepts plain http for any host', () => {
    expect(normalizeServerUrl('http://localhost:8090')).toBe('http://localhost:8090');
    expect(normalizeServerUrl('http://127.0.0.1:8090/')).toBe('http://127.0.0.1:8090');
    expect(normalizeServerUrl('http://viyu.lan:8090')).toBe('http://viyu.lan:8090');
  });

  test('assumes https when no scheme is typed', () => {
    expect(normalizeServerUrl('viyu.example.com')).toBe('https://viyu.example.com');
  });

  test('refuses credentials, query, fragment and anything that is not an address', () => {
    expect(rejected('https://user:secret@viyu.example.com')).toBe(true);
    expect(rejected('https://viyu.example.com/?token=1')).toBe(true);
    expect(rejected('https://viyu.example.com/#frag')).toBe(true);
    expect(rejected('https://')).toBe(true);
    expect(rejected('')).toBe(true);
    expect(rejected('ftp://viyu.example.com')).toBe(true);
  });
});

describe('joinServerUrl', () => {
  test('builds a server path under the origin', () => {
    expect(joinServerUrl('https://viyu.example.com', '/api/health')).toBe('https://viyu.example.com/api/health');
  });

  test('keeps the base path prefix instead of resetting to the origin', () => {
    expect(joinServerUrl('https://example.com/viyu', '/api/stream/abc')).toBe('https://example.com/viyu/api/stream/abc');
    expect(joinServerUrl('https://example.com/viyu/', 'api/stream/abc?source=twitch')).toBe('https://example.com/viyu/api/stream/abc?source=twitch');
  });

  test('a base without a path stays at the root', () => {
    expect(joinServerUrl('http://localhost:8090', 'api/health')).toBe('http://localhost:8090/api/health');
  });
});
