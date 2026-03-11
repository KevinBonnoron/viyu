# Library

Everything here is **per profile**: switching profile changes your subscriptions, history, likes, feed and statistics.

## Feed

The home feed combines Viyu's own recommendations (built from your subscriptions and likes) with, once a source is
connected, that source's home feed. Source pills select the local feed or one connected feed; _All_ merges them and removes
duplicates. A platform filter narrows what is loaded. Pages load automatically as you scroll, with skeletons while waiting
and a retry on failure. Videos you marked **Not interested** are hidden from the feed and from recommendations next to the
player, but not from search or history.

## Search

Search starts after a 400 ms typing pause; `Enter` starts immediately. The search dialog (quick search) shows what Viyu
already knows first, under **Your library**: your subscribed and cached channels and the videos in your history and likes,
instantly and without a request. Results from the server follow. Cards show whether a video is watched, liked or can be
resumed. A platform filter narrows mixed results. Channel results have their own **Subscribe** button.

Pasting a video link (YouTube, a Twitch VOD, a PeerTube video of a listed instance) in the search field opens it after you
confirm. Disabled sources are never searched.

## Subscriptions

**Subscriptions** shows the newest videos of the channels you follow. Channel avatars toggle a filter; selecting a channel
narrows the timeline and the search field filters titles and creators. The sidebar counts the profile's subscriptions.

- Expired channel listings are refreshed in the background, a few at a time; cached videos show immediately.
- Exact publication dates come from the platform or, for YouTube, the channel's public RSS feed. Missing dates are never
  invented, and videos with unknown dates sort last.
- **Same creator on several platforms**: when a creator's YouTube and Twitch channels share a name, Viyu links the two
  subscriptions on its own. The filter then shows one creator with both platform badges. **Separate** splits them for good;
  **Group** links such a pair again. Unsubscribing from one platform leaves the other alone.
- **Subscriptions → Series** lists the series of your subscribed channels.

## Series

Viyu infers series from the public catalogs of the YouTube and Twitch channels you follow. Explicit delimiters and episode
markers in titles are recognised (`Part 3`, `[09]`, `S05E01`); two matching videos are required, and unrelated recurring
words do not create a series. A title ending in `(first look)`, `(preview)`, `(demo)` and similar qualifiers is treated as the
pilot of the series.

A Twitch collection (a list of VODs a streamer put together and named) is a series of its own, called like the collection and
in its order, whatever the titles say; its videos no longer take part in the inference. Twitch cannot list a collection of
more than 100 videos, so those are left to the titles. A video that Twitch let expire leaves its series, which disappears
once fewer than two videos remain.

When a numbering starts over (a demo, then the released game), Viyu may split the series into seasons; titles that name
seasons show one collapsible block per season with a **Play season** button, otherwise a single **Play series** button.

Episode numbers come from titles first. For videos without one, the server can read the number from thumbnails with OCR if
the administrator's server was built with it and the switch is on. Conflicting or failed results never invent a number.
Opening a channel starts a background job that fetches its whole catalog, not only the latest videos; the Series tab of a
channel page fills in as the analysis finishes.

## History and likes

**History** lists what you watched, most recent first, with a progress bar from your resume position. The card menu has
**Remove from history** and **Mark as watched/unwatched**, and **Clear all** empties the profile's history (and its resume
positions) after confirmation. **Likes** lists the videos you liked. Likes, dislikes and **Not interested** stay in Viyu and
are not sent to the platforms; with a connected YouTube source, only the _watched_ mark is.

## Notifications

The bell shows videos that subscribed creators released since you last looked, one line per creator (both platforms merged),
each opening the channel. A dot marks new releases until you press **Mark all as seen**. Newly subscribed channels do not
flood you with their back catalog, and live streams do not count.

## Statistics

**Stats** shows what the active profile watched: total watch time and distinct videos, the most watched tags, categories and
channels, a month-by-month chart and the split between platforms, for all time, 30 days, 90 days or a year. Time is taken
from your Viyu history; platforms you turned off are left out.
