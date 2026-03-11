# Watching

## The player

Viyu plays videos in its own player, streamed through your Viyu server.

- **Controls**: play/pause, a seekable timeline that shows what is buffered, volume and fullscreen. Controls hide shortly after
  the pointer stops moving and when it leaves the player.
- **Shortcuts** (player focused): `Space` or `K` play/pause, `←`/`→` seek five seconds, `M` mute, `F` fullscreen.
- **Settings menu**: separate **Quality**, **Audio language** and **Playback speed** (0.25× to 2×) sub-menus. Changing quality
  or audio keeps your position and paused state.
- **Quality**: _Auto_ means the highest available resolution, not bandwidth-adaptive. A fixed resolution picks the best
  available quality up to that limit. Set your default in Settings → Playback.
- **Audio** starts with the video's original language.
- **Autoplay** is on by default and can be switched off. If the browser blocks sound, Viyu plays muted and shows **Unmute**.
- **Chapters**: when a YouTube video has chapters, their boundaries show on the timeline and the current chapter opens a list
  for direct seeking.
- **Preview**: hovering the timeline shows a small preview frame when the platform provides one.
- **Resume**: Viyu saves your position per profile and resumes from it, also after a reload. Videos you watched at least 90 %
  of show a **Watched** badge.

### Private playback

The eye button in the top bar turns on **private playback** for the current browser tab. While it is on, new videos leave no
history, resume positions or YouTube watch marks, and a dashed frame around the application shows the mode. Earlier activity
is not erased. The mode survives reloads and ends when you close the tab.

### SponsorBlock

For YouTube videos Viyu asks [SponsorBlock](https://sponsor.ajay.app) for community-reported sponsor segments. A **Skip
sponsor** button appears inside a segment; **Automatically skip sponsored segments** (Settings → Playback, off by default)
skips it for you. The data is licensed [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/). If SponsorBlock
has nothing or is unreachable, playback is unaffected.

## Side panel

Next to the video (below it on narrow or tall windows) a panel offers the sections the video has: the **series** episode list,
the **description** (timestamps seek) and the **comments** or **chat**. It opens by itself for a live stream's chat and for an
episode of a series, and Viyu remembers whether you closed it, separately for streams, series episodes and plain videos.

## Comments and chat

- **YouTube** shows top-level comments, loaded separately from the video so a failure can be retried without interrupting
  playback. **PeerTube** shows top-level comments.
- **Twitch** shows the chat. For a live stream it is live; for a VOD it replays in sync with the playhead. The chat is
  read-only, and shows emotes, colours, replies and a badge counting messages received while the panel is closed. If Twitch
  cannot be reached, the section says the chat is unavailable.
- Web addresses (`http://` and `https://`) in comments, chat and descriptions are links that open in a new tab.

## Live streams

A channel that is broadcasting shows a red **LIVE** badge and a **Watch live** button. On **Subscriptions**, live channels get
a red ring on their avatar and their streams come first in the video list.

A live stream starts at its live edge and never resumes a saved position. The time display becomes a **LIVE** indicator and a
**Back to live** button appears when you are more than 25 seconds behind. Twitch keeps only about 40 seconds of rewind; YouTube
keeps an hour. Streams leave no history entry. A stream that ended no longer shows as live, and opening its page says the
channel is not live right now.

## Series playback

When you watch an episode, the controls gain a **Next episode** button, and playback advances only after the media ends. Opening
an episode from History brings back its series.

## Video cards

Cards show the title and channel, a compact view count (exact on hover), a relative publication date (full date on hover) and
the platform logo. Titles are the original ones by default; Settings → Playback has the **Original titles** switch. The card
menu offers **Mark as watched/unwatched** (Viyu profile only, YouTube history is untouched), **Not interested**, **Copy
original link** and **Remove from history** where relevant.
