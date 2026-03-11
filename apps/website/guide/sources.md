# Sources

A source is a platform Viyu can read: YouTube, Twitch or PeerTube. The **Sources** page lists them with a switch each.

## Turning sources on and off

Each account chooses which sources it uses. A source that is off is never queried for you: it is left out of search, and its
channels, subscriptions, feed entries, notifications and live status are hidden. Your existing subscriptions, history and
likes for that platform are kept and come back when you turn it on again.

YouTube and Twitch are on by default. PeerTube is **off** until you turn it on, because its content comes from other
people's servers.

## YouTube

YouTube works without any account connection: search, channel pages, playback, comments and chapters use
public data. Series are inferred from YouTube channel catalogs, and SponsorBlock and the community dislike estimate are
YouTube features.

## Twitch

Search, channel catalogs, VOD playback and live streams work without an account. Search returns channels and VODs together.
Series work like on YouTube: Viyu reads a channel's whole video list (past broadcasts, highlights and uploads), infers series
from the titles, and uses the collections the streamer made as series in the streamer's own order. Twitch lets VODs expire,
and a series shrinks with them. The player shows the chat read-only; Viyu cannot post. VODs reserved for a channel's
subscribers carry a **Subscribers only** badge. If you do not watch those, switch on **Hide subscriber-only videos** in
Settings → Playback.

Twitch's Comments section is its chat, and Viyu does not synchronise Twitch watch history; Viyu's own subscriptions, likes and resume positions work
the same as for other platforms.

## PeerTube

PeerTube is a federation of independent instances, so videos and channels are named with their instance (for example
`name@framatube.org`). The server only talks to instances its administrator listed, and search goes through Sepia Search,
keeping the results that live on a listed instance. A short list therefore gives sparser result pages. Playback needs the
instance to offer HLS. Series, SponsorBlock and dislike estimates do not apply.

## Connected accounts

A connection lets Viyu act as you on a platform for personalised data:

- **YouTube**: home recommendations, your subscription videos and watch history, and playback with your session. After
  30 seconds of playback (or the full length of a shorter video) Viyu asks YouTube to mark the video watched. Likes and
  subscription changes made in Viyu stay in Viyu.
- **Twitch** (optional): followed live channels on the home page and playback for channels you subscribe to.

To connect, open **Sources**, choose **Connect**, and use **Import cookies manually** with a Netscape-format cookie export
from a browser where you are signed in. Viyu checks that the session works before saving it. If a session expires, connect
again; a failed replacement keeps the previous connection. **Disconnect** stays available while a source is off, and turning a
source off keeps its connection unused until you turn it back on.

::: danger Read this before connecting
The server stores your session cookies (encrypted) and uses them for every request made on your behalf. The server's operator
can therefore act as you on that platform. Using a logged-in session through automated tooling may also violate the
platform's terms of service and can get the account restricted or banned. Connect only to a server you trust, and prefer a
secondary account. Details are on the [Privacy and security](/guide/privacy) page.
:::

The repository contains an experimental helper (`tools/youtube-cookie-helper`) that copies YouTube cookies from a local
browser profile to your clipboard. Clipboard contents can be read by other applications and are not cleared automatically.
