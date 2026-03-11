# ADR 0008: YouTube subscriptions and history arrive through a browser companion

- Status: accepted
- Scope: `companion_devices` collection, `subscriptions.synced`, `/api/companion/*` routes

## Context

Bringing an account's YouTube subscriptions and watch history into Viyu needed a cookie export (ADR 0004), which users find
impractical, and which hands the server a full session. The official APIs do not cover the need: the YouTube Data API lists
subscriptions but not the watch history, and Google's Data Portability API (both, with a restricted scope) needs Google's
verification of each deployment, works in some countries only and delivers daily archives. A self-hosted server has none of that.

The browser where the user is signed in to YouTube already holds a working session, and youtube.com reads both lists from
its own InnerTube API.

## Decision

- A browser extension (Chrome and Firefox, Manifest V3) runs YouTube's browse requests inside a youtube.com tab
  (`scripting.executeScript`, main world), exactly as the page does. Cookies are read there to compute YouTube's
  `SAPISIDHASH` and never leave the page; the extension sends Viyu only channel and video identifiers, plus the day,
  progress and length YouTube shows for each history entry.
- Pairing: a signed-in user asks Viyu for a single-use code (10 minutes, in memory, stored hashed) for one of their profiles
  and pastes the pairing string (server address + code) into the extension, which exchanges it for a random device token.
  The server stores only the token's SHA-256 in `companion_devices`, a collection no API client can read. The token only
  reaches the companion routes (`X-Viyu-Companion` header), for that profile; revoking the device in Viyu or unpairing in the
  extension deletes it.
- One way, YouTube to Viyu. Subscriptions the companion creates are marked `synced`; a complete list from YouTube removes
  the synced ones YouTube no longer has, never a subscription made in Viyu, and an empty or partial list removes nothing.
  History entries are created when missing; an existing one only moves forward (a re-watch on YouTube more than a day
  later, or the watched flag), so Viyu's own playback progress is not overwritten by YouTube's coarser data.
- Shared records come from identifiers only (`EnsureChannel`/`EnsureVideo` stubs, hydrated by the backfill), as for an
  account import: nothing the extension says about a title or a name is written into records every account reads.
- Synchronization runs when the user asks, after pairing, and automatically at most every six hours while a YouTube tab is
  open. The first one reads up to 50 history pages; later ones stop at the first page that brings nothing new.

## Consequences

- No cookie reaches the server for this, and the device token is useless outside the companion routes of one profile.
- The extension depends on youtube.com's private API and page layout. The parser walks the responses for the renderers it
  knows instead of following fixed paths, but a YouTube change can still break it until the extension is updated.
- YouTube gives the day of a history entry, not its time: entries are placed within their day in YouTube's order.
- Synchronization needs the browser: nothing updates while it is closed, and the Android app cannot do it.
