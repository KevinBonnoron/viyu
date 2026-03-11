# ADR 0004: Connected-source cookies are stored on the server, encrypted

- Status: accepted
- Scope: `source_connections` collection and the server's source service

## Context

A connected source gives personalised YouTube or Twitch data (home feed, followed live channels, watch marking, members-only
access). The only credential yt-dlp accepts for that is a browser session exported as a Netscape cookie file. Extraction
runs on the server, so the server must be able to use the cookies on the user's behalf at any time, including background
work, not only while the user's browser is open. Desktop and Android clients cannot run a local extractor.

## Decision

- The user imports a cookie export on the Sources page. The server validates the session with the platform, then stores it.
- Cookies are encrypted with AES-256-GCM using a random 32-byte master key. The additional authenticated data binds each
  ciphertext to its user and platform, so a row cannot be replayed for another account.
- The key is a private (mode 0600) regular file: `<data dir>/source_connections.key`, or the absolute path in
  `VIYU_SOURCE_KEY_FILE` so that it can live on another volume or in a secret mount. It is created on first use and never
  overwritten.
- The ciphertext field is hidden and the collection has no API rules: no client, including the owner, can read it back.
  Disconnecting deletes the row.
- Per process, the server writes the decrypted cookies to a temporary file in a 0700 directory inside the data directory and
  deletes it afterwards. When yt-dlp rotated the session, the new jar replaces the stored one (re-encrypted). The directory is
  emptied on startup.
- Content fetched through a connection is private to the accounts that fetched it (`owners` on videos and channels);
  public fetches stay public.

## Consequences

- Whoever controls the server and its key can use every stored session. Users are told to connect only to a server they
  trust; this is stated in the README.
- Losing the key makes saved connections unreadable; users reconnect. Backups must include the key, and keeping it outside
  the data directory means a stolen database copy alone does not contain both ciphertext and key.
- Using a logged-in session with automated tooling can violate the platform's terms of service and risks the account being
  restricted. Viyu documents this plainly and cannot mitigate it.
- Rotating the master key is not supported; it would require re-encrypting every row.
