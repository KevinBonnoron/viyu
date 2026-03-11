# Watching

## The player

Viyu plays videos in its own player, streamed through your Viyu server.

- **Controls**: play/pause, a seekable timeline that shows what is buffered, volume and fullscreen. With a mouse, controls hide
  shortly after the pointer stops moving or leaves the player. They remain available while paused, seeking or using the settings.
- **Touch controls**: one tap on the video shows the controls without pausing playback; another tap on the video hides them.
  Use the play/pause button to change playback. Controls hide after three seconds of inactivity during playback, including
  after a tap or scrub ends while the timeline still has focus. Keyboard-focused controls remain visible until focus leaves.
  Hidden buttons cannot take taps. On narrow screens, use your device's hardware volume buttons.
- **Seeking**: dragging the timeline previews the target time without moving the video. Releasing commits one seek; cancelling
  the gesture leaves playback where it was. Keyboard seeking still takes effect immediately.
- **Shortcuts**: `Space` or `K` play/pause, `←`/`→` seek five seconds, `M` mute, `F` fullscreen, unless a field or another overlay owns the keys.
- **Settings menu**: separate **Quality**, **Audio language** and **Playback speed** (0.25× to 2×) sub-menus. Changing quality
  or audio keeps your position and paused state.
- **Quality**: _Auto_ means the highest available resolution, not bandwidth-adaptive. A fixed resolution picks the best
  available quality up to that limit. Set your default in Settings → Playback.
- **Audio** starts with the video's original language.
- **Autoplay** is on by default and can be switched off. If the browser blocks sound, Viyu plays muted and shows **Unmute**.
- **Chapters**: when a YouTube video has chapters, their boundaries show on the timeline and the current chapter opens a list
  for direct seeking.
- **Preview**: on a desktop with a hover-capable pointer, hovering the timeline shows a small preview frame when the platform
  provides one. Mobile and touch interfaces do not show hover previews or tooltips.
- **Resume**: Viyu saves your position per profile and resumes from it, also after a reload. Videos you watched at least 90 %
  of show a **Watched** badge.
- **Fullscreen**: the player fills the screen without rounded corners or page margins. The video's proportions are preserved,
  so rotate the device to landscape for a widescreen video rather than cropping its image. The Android app keeps immersive
  system bars hidden after fullscreen too; an edge swipe reveals them temporarily. Android's Back button exits fullscreen without leaving the video.

On a video page, the player comes first; the title, which can wrap to two lines on mobile, and the video details follow below it.
Android fullscreen support is part of the native app: installing a newly built APK is required to get native-shell changes;
updating only the Viyu server does not update an already installed APK.

### Private playback

The eye button in the player controls turns on **private playback** for the current browser tab; the command palette offers the same switch.
While it is on, new videos leave no history, resume positions or YouTube watch marks. The button stays highlighted and a short message
confirms each change. It hides with the other controls, including in fullscreen, so nothing stays over the picture.
Earlier activity is not erased. The mode survives reloads and ends when you close the tab.

### SponsorBlock

For YouTube videos Viyu asks [SponsorBlock](https://sponsor.ajay.app) for community-reported sponsor segments. A **Skip
sponsor** button appears inside a segment; **Automatically skip sponsored segments** (Settings → Playback, off by default)
skips it for you. The data is licensed [CC BY-NC-SA 4.0](https://creativecommons.org/licenses/by-nc-sa/4.0/). If SponsorBlock
has nothing or is unreachable, playback is unaffected.

## Side panel

Next to the video (below it on narrow or tall windows) a panel offers the sections the video has: the **series** episode list,
the **description** (timestamps seek), **comments** or **chat**, and **recommendations**. It opens by itself for a live stream's chat and for an
episode of a series, and Viyu remembers whether you closed it, separately for streams, series episodes and plain videos.

Below the video, the sections use one compact row of text buttons with the selected section highlighted. Icons remain in the
vertical rail beside the video on wide layouts. Touch targets stay at least 44 pixels high; long labels truncate rather than wrapping to a second row.

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

When no next episode follows, the end screen offers **Replay** and recommended videos. Cards use a wider layout with larger
titles on large players. The column count follows the player's width rather than the browser window; short players scroll
instead of shrinking the recommendations.

## Video cards

Cards show the title and channel, a compact view count (exact on hover), a relative publication date (full date on hover) and
the platform logo. Titles are the original ones by default; Settings → Playback has the **Original titles** switch. The card
menu offers **Mark as watched/unwatched** (Viyu profile only, YouTube history is untouched), **Not interested**, **Copy
original link** and **Remove from history** where relevant.
