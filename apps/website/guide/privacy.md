# Privacy and security

Viyu is built for a server you or someone you trust runs. This page states what it stores, who can see it and what leaves
your server.

## Accounts

An account is required for everything that reaches the network: search, opening links, videos, channels, live status, chat and
media. Only the health check, the registration flag and the login endpoints are anonymous. Sessions travel as an
`Authorization: Bearer` header, never as a cookie or in a URL, so tokens do not end up in logs or shared links.

Rate limits protect the server from bursts, and the administrator can close public registration.

## What the server stores

- Your profiles, subscriptions, history, resume positions, likes, notification markers and settings.
- A cache of video and channel metadata fetched on behalf of users.
- Optionally, your connected-source cookies (below).

The server operator and anyone with access to its data directory can read your viewing data. Export files contain it too.

## Who can see a video or channel record

Records fetched through a connected source belong to the accounts that fetched them, and other accounts cannot read them. A
record fetched without a connected source is public: every account on the server can read it. Once public, a record stays
public.

## Connected-source cookies

When you connect YouTube or Twitch, the server stores your session cookies:

- They are encrypted at rest with AES-256-GCM. The key is a file next to the database (or wherever the operator configured),
  not inside the database.
- No client can read them back, including you; they can only be replaced or disconnected.
- To make a request for you, the server writes them to a temporary private file for yt-dlp and deletes it afterwards.
- The operator controls the server and therefore the key: **the operator can act as you on that platform.** Connect only to a
  server you trust.
- Using a logged-in session through automated tooling may violate YouTube's or Twitch's terms of service and can lead to the
  account being restricted or banned. Prefer a secondary account.
- Never share cookie exports, the key file or data-directory backups.

## Third parties contacted by your browser

The web UI only allows connections to its own server plus the services below.

| Service                                                        | When                                          | What it receives                         |
| -------------------------------------------------------------- | --------------------------------------------- | ---------------------------------------- |
| [SponsorBlock](https://sponsor.ajay.app)                       | Playing a YouTube video                       | The video id.                            |
| [Return YouTube Dislike](https://returnyoutubedislike.com)     | A YouTube video whose dislike count is hidden | The video id.                            |
| `i.ytimg.com`, `static-cdn.jtvnw.net`, PeerTube instance hosts | Showing thumbnails, preview frames and emotes | The image request, with your IP address. |

Your account token, cookies and reactions are never sent to them. The server itself contacts the platforms (through yt-dlp
and their public interfaces) and, for PeerTube search, [Sepia Search](https://sepiasearch.org).

## Network safety

The server only contacts PeerTube instances its administrator listed. Its own HTTP fetches refuse private, loopback and
link-local addresses, so a crafted link cannot make it reach internal services. The web UI is served with a strict
Content-Security-Policy and cannot be framed.

## Native apps

The desktop and Android apps keep a separate sign-in for each server. The Android app opts out of system backups.
