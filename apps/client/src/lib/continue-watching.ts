import type { VideoRecord, WatchHistoryRecord } from '@/types';

/** A history row whose video the server expanded: what a "continue watching" card is drawn from. */
export type ResumableRow = WatchHistoryRecord & { expand: { video: VideoRecord } };

/**
 * The rows, among unfinished history rows (already newest first), that may be offered for resuming: the video must be
 * known (a row the server could not expand has nothing to draw), on a platform that is turned on, not finished, started
 * (a position to resume from), allowed by the subscriber-only preference, and within `channelIds` when given. Order is kept.
 */
export function pickResumable(
  rows: readonly WatchHistoryRecord[],
  {
    channelIds,
    isEnabled,
    hideSubscriberOnly = false,
  }: { channelIds?: readonly string[]; isEnabled: (platform: string) => boolean; hideSubscriberOnly?: boolean },
): ResumableRow[] {
  const channels = channelIds && new Set(channelIds);
  return rows.filter(
    (row): row is ResumableRow =>
      !!row.expand?.video &&
      !row.watched &&
      row.position > 0 &&
      isEnabled(row.expand.video.platform) &&
      !(hideSubscriberOnly && row.expand.video.subscriber_only) &&
      (!channels || channels.has(row.expand.video.channel)),
  );
}
