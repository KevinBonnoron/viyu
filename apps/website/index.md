# Viyu

Viyu is a self-hosted place to follow YouTube, Twitch and PeerTube channels and watch their videos in one player. You run
a server, create an account, subscribe to channels and watch from a browser, a Linux desktop app or an Android app.

::: warning Pre-alpha
Viyu is under heavy development. Features and the database layout may change, and a server upgrade can require starting
from an empty data directory.
:::

## What you can do

- **Follow channels** from three platforms and see their new videos in a single timeline and home feed.
- **Watch** in Viyu's own player with quality and audio choices, speed control, chapters, resume and SponsorBlock skipping.
- **Watch live streams** with a LIVE badge, a jump-to-live button and the Twitch chat next to the video.
- **Search** the platforms you enabled, or paste a video link.
- **Binge series**: Viyu groups episodes of a YouTube or Twitch channel into series and plays the next one for you.
- **Keep profiles** under one account, each with its own subscriptions, history, likes and colour.
- **Stay informed** with notifications of new videos and statistics about what you watch.
- **Connect a YouTube or Twitch session** (optional) for personalised feeds. Read [Privacy and security](/guide/privacy)
  first.

## First steps

1. Open your server's address and **register** (or log in). If registration is closed, ask the administrator to create an
   account for you.
2. Open **Sources** and check which platforms are on. YouTube and Twitch are on by default; PeerTube is off until you
   enable it.
3. Use **Search** to find a channel and subscribe to it. Your feed and **Subscriptions** fill as videos are fetched.
4. Open a video. Viyu remembers your position and shows it in **History**.

On the desktop or Android app, enter the address of your server on first launch instead. See
[Settings and accounts](/guide/settings) for switching servers.

## Guide

| Page                                     | Contents                                                                           |
| ---------------------------------------- | ---------------------------------------------------------------------------------- |
| [Sources](/guide/sources)                | YouTube, Twitch and PeerTube, turning sources on or off, connecting an account.    |
| [Watching](/guide/watching)              | The player, live streams, chat, comments and playback preferences.                 |
| [Library](/guide/library)                | Feed, search, subscriptions, series, history, likes, notifications and statistics. |
| [Settings and accounts](/guide/settings) | Profiles, appearance, language, export/import and administration.                  |
| [Privacy and security](/guide/privacy)   | What is stored, who can see it and which third parties are contacted.              |
| [Running a server](/guide/self-hosting)  | Installing, operating and backing up a Viyu server.                                |

Developers: see the [README](https://github.com/KevinBonnoron/viyu#readme) for development setup and the
[architecture decision records](https://github.com/KevinBonnoron/viyu/tree/main/docs/adr).
