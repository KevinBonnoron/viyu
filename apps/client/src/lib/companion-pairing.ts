const PREFIX = 'VIYU-PAIR:';

/**
 * The string a viewer pastes into the extension: the server to talk to and the one-time code, so the extension needs
 * no other setup. The JSON is written in a fixed key order without whitespace, and base64url without padding keeps the
 * string free of characters that a paste could mangle.
 */
export function pairingString(server: string, code: string): string {
  const json = JSON.stringify({ v: 1, server, code });
  const bytes = new TextEncoder().encode(json);
  let binary = '';
  for (const byte of bytes) {
    binary += String.fromCharCode(byte);
  }
  return PREFIX + btoa(binary).replaceAll('+', '-').replaceAll('/', '_').replace(/=+$/, '');
}
