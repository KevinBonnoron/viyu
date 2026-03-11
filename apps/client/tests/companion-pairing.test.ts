import { pairingString } from '../src/lib/companion-pairing';
import { describe, expect, test } from 'bun:test';

describe('companion pairing string', () => {
  test('encodes the server and code exactly as the extension decodes them', () => {
    expect(pairingString('https://viyu.example/sub', 'abc_DEF-123')).toBe(
      'VIYU-PAIR:eyJ2IjoxLCJzZXJ2ZXIiOiJodHRwczovL3ZpeXUuZXhhbXBsZS9zdWIiLCJjb2RlIjoiYWJjX0RFRi0xMjMifQ',
    );
  });

  test('stays within the URL-safe alphabet and carries non-ASCII server paths as UTF-8', () => {
    const encoded = pairingString('https://viyu.example/été', '??>>~~');
    expect(encoded).toMatch(/^VIYU-PAIR:[A-Za-z0-9_-]+$/);
    const body = encoded.slice('VIYU-PAIR:'.length).replaceAll('-', '+').replaceAll('_', '/');
    const json = new TextDecoder().decode(Uint8Array.from(atob(body + '='.repeat((4 - (body.length % 4)) % 4)), (char) => char.charCodeAt(0)));
    expect(JSON.parse(json)).toEqual({ v: 1, server: 'https://viyu.example/été', code: '??>>~~' });
  });
});
