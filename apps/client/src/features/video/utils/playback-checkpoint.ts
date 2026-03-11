/**
 * The seconds a playback sample earns as watch time. Seeking never earns any: a jump of the media clock larger than the wall
 * clock allows (at the playback rate, plus a small tolerance) is rejected, even if a browser reports no `seeking` state.
 */
export function sampledSeconds(mediaDelta: number, wallSeconds: number, playbackRate: number): number {
  return mediaDelta > 0 && mediaDelta <= Math.max(0, wallSeconds) * playbackRate + 0.5 ? mediaDelta : 0;
}

/**
 * The `duration_watched` a checkpoint writes. `stored` is what the history row holds, `savedPlayed` how much of this
 * session's `played` seconds an earlier checkpoint already added to it: only the not-yet-saved delta is added, so a
 * re-watch (or a repeated checkpoint) never counts the same seconds twice. The total never exceeds the video's duration
 * (when it is known): watching a video twice over is still one full view.
 */
export function nextDurationWatched(stored: number, savedPlayed: number, played: number, duration: number): number {
  const base = Number.isFinite(stored) && stored > 0 ? stored : 0;
  const delta = Number.isFinite(played) ? Math.max(0, played - savedPlayed) : 0;
  const total = base + delta;
  const capped = Number.isFinite(duration) && duration > 0 ? Math.min(duration, total) : total;
  // Millisecond precision: the sum of many float samples carries no meaning beyond it.
  return Math.round(capped * 1000) / 1000;
}

/** The share of a video's length at which a checkpoint marks it watched. This is the only place that threshold exists. */
const WATCHED_FRACTION = 0.9;

/**
 * Whether a checkpoint marks the video watched: its media position or the watch time accumulated on the row reached 90 % of
 * the video's duration. An unknown duration never does. The flag is only ever raised by a checkpoint, never lowered.
 */
export function reachedWatched(mediaTime: number, durationWatched: number, duration: number): boolean {
  if (!Number.isFinite(duration) || duration <= 0) {
    return false;
  }
  const threshold = duration * WATCHED_FRACTION;
  return (Number.isFinite(mediaTime) && mediaTime >= threshold) || (Number.isFinite(durationWatched) && durationWatched >= threshold);
}
