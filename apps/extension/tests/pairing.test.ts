import { PairingError, parsePairing } from '../src/pairing';
import { describe, expect, test } from 'bun:test';

const encode = (payload: unknown): string => `VIYU-PAIR:${Buffer.from(JSON.stringify(payload)).toString('base64url')}`;

describe('parsePairing', () => {
  test('reads the string Viyu shows (shared fixture with the client)', () => {
    expect(parsePairing('  VIYU-PAIR:eyJ2IjoxLCJzZXJ2ZXIiOiJodHRwczovL3ZpeXUuZXhhbXBsZS9zdWIiLCJjb2RlIjoiYWJjX0RFRi0xMjMifQ\n')).toEqual({
      server: 'https://viyu.example/sub',
      code: 'abc_DEF-123',
    });
  });

  test('keeps the path prefix but drops a trailing slash, query and fragment', () => {
    expect(parsePairing(encode({ v: 1, server: 'http://127.0.0.1:8090/viyu/?x=1#y', code: 'c' })).server).toBe('http://127.0.0.1:8090/viyu');
  });

  test.each([
    ['another prefix', 'VIYU:abc'],
    ['broken base64', 'VIYU-PAIR:***'],
    ['not JSON', `VIYU-PAIR:${Buffer.from('nope').toString('base64url')}`],
    ['unknown version', encode({ v: 2, server: 'https://viyu.example', code: 'c' })],
    ['a non-http server', encode({ v: 1, server: 'javascript:alert(1)', code: 'c' })],
    ['credentials in the server', encode({ v: 1, server: 'https://user:pass@viyu.example', code: 'c' })],
    ['a code with other characters', encode({ v: 1, server: 'https://viyu.example', code: 'a b' })],
  ])('refuses %s', (_case, input) => {
    expect(() => parsePairing(input)).toThrow(PairingError);
  });
});
