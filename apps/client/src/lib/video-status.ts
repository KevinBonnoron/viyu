import type { VideoReaction, VideoReactionRecord, WatchHistoryRecord } from '@/types';

/** What the active profile's history and reactions say about one video. */
export interface VideoStatus {
  watched: boolean;
  /** The fraction of an unfinished playback, when it can be resumed. */
  progress?: number;
  reaction?: VideoReaction;
}

/** The statuses of the videos the given history rows and reactions mention, keyed by `videos` record id. */
export function buildVideoStatuses(
  histories: readonly Pick<WatchHistoryRecord, 'video' | 'video_duration' | 'watched' | 'position'>[],
  reactions: readonly Pick<VideoReactionRecord, 'video' | 'reaction'>[],
): Map<string, VideoStatus> {
  const statuses = new Map<string, VideoStatus>();
  for (const entry of histories) {
    const duration = entry.video_duration;
    const watched = entry.watched;
    const progress = !watched && duration > 0 && Number.isFinite(entry.position) && entry.position > 0 ? Math.min(1, entry.position / duration) : undefined;
    statuses.set(entry.video, { watched, progress });
  }
  for (const entry of reactions) {
    statuses.set(entry.video, { ...(statuses.get(entry.video) ?? { watched: false }), reaction: entry.reaction });
  }
  return statuses;
}
