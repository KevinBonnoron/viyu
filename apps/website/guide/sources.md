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

### Importing your YouTube subscriptions and history

The **YouTube companion** browser extension (Chrome or Firefox, in the repository's `apps/extension`) copies the subscriptions
and the watch history of the YouTube account signed in in your browser into one of your profiles. It reads YouTube inside a
youtube.com tab and sends Viyu only channel and video identifiers: your YouTube cookies stay in the browser, and no
connected account is needed.

1. On **Sources**, in the YouTube card, choose **Pair an extension**. The pairing is for the profile in use.
2. Copy the pairing code (valid 10 minutes, single use) and paste it into the extension's popup.
3. The first sync starts at once. Later ones run when you click **Sync now**, and on their own at most every 6 hours while
   YouTube is open in that browser.

The sync goes one way, from YouTube to Viyu. A channel you unsubscribe from on YouTube leaves Viyu too, if the extension
added it; subscriptions you made in Viyu are never removed. A channel you remove in Viyu but keep on YouTube comes back with
the next sync. History entries keep YouTube's day (the time within the day is approximate) and the progress YouTube shows;
what you watched in Viyu keeps Viyu's own progress. Paired extensions are listed in the YouTube card, where you can revoke
them; **Unpair** in the popup does the same from the extension's side.

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
