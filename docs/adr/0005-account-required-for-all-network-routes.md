# ADR 0005: An account is required for every route that reaches the network

- Status: accepted
- Scope: custom HTTP routes of the server and the web/native clients

## Context

Search, resolve, video, channel, chat and stream routes can spawn yt-dlp or fetch from third-party hosts. On a server
reachable from the internet, anonymous access would turn Viyu into an open extraction proxy: it spends the operator's CPU and
bandwidth, exposes the operator's IP to the platforms and invites abuse and bans. Media streams were previously authorised by
a cookie minted by a separate endpoint, which added state and a second authentication path.

## Decision

- Every custom route that can start yt-dlp or perform an outbound fetch is bound to `apis.RequireAuth("users")`. Anonymous
  access remains for `/api/health`, `/api/registration` (whether sign-up is open) and PocketBase's own auth endpoints.
- Authentication is the PocketBase token sent as `Authorization: Bearer`, on every route. HLS playlists and segments use the
  same header (hls.js `xhrSetup`/`fetchSetup`), and live chat reads its Server-Sent Events with `fetch` and a stream reader
  because `EventSource` cannot set headers. There is no playback cookie and no `POST /api/playback-session`.
- Public registration is an administrator switch; an operator who does not want strangers on the server turns it off and
  creates users from the administration page.
- PocketBase's built-in rate limiter is enabled at bootstrap, with a larger budget for `/api/stream/`.
- CORS grants credentialed access only to the fixed Wails and Capacitor origins.
- Failures use a single JSON envelope `{ "code", "message" }`.

## Consequences

- A server is a private service by default; there is no anonymous browsing mode.
- Tokens never appear in URLs, so they do not leak into logs, history or shared links.
- Media elements cannot play a stream URL on their own: playback must go through hls.js, which suits the proxying design.
- Every new route that reaches the network MUST bind `RequireAuth`; a missing binding is a security bug.
- Accounts are the unit for rate limiting and for per-account switches such as which sources are active.
