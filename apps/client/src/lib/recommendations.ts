import type { VideoStatus } from '@/lib/video-status';
import type { VideoResult } from '@/types';

/** How many recommended videos the end screen offers. */
export const END_SCREEN_SUGGESTIONS = 4;

/** What a playback end shows: the next episode of the series, or a replay with recommendations; nothing for a stream. */
export type EndScreenKind = 'next' | 'suggestions';

export function endScreenKind({ live, hasNextEpisode }: { live: boolean; hasNextEpisode: boolean }): EndScreenKind | null {
  if (live) {
    return null;
  }
  return hasNextEpisode ? 'next' : 'suggestions';
}

/** The key a video is excluded by: `platform:id`. */
export function videoKey(video: Pick<VideoResult, 'platform' | 'id'>): string {
  return `${video.platform}:${video.id}`;
}

/** The recommendations the viewer still wants: not one of `exclude` (the series' episodes), not marked "not interested". */
export function visibleRecommendations(
  videos: readonly VideoResult[],
  statuses: ReadonlyMap<string, Pick<VideoStatus, 'reaction'>>,
  exclude?: ReadonlySet<string>,
): VideoResult[] {
  return videos.filter((video) => !exclude?.has(videoKey(video)) && statuses.get(video.id)?.reaction !== 'not_interested');
}

/** The videos offered when playback ends: the first visible recommendations, never the video that just played. */
export function endScreenSuggestions(
  videos: readonly VideoResult[],
  statuses: ReadonlyMap<string, Pick<VideoStatus, 'reaction'>>,
  { exclude, currentId }: { exclude?: ReadonlySet<string>; currentId?: string } = {},
): VideoResult[] {
  return visibleRecommendations(videos, statuses, exclude)
    .filter((video) => video.id !== currentId)
    .slice(0, END_SCREEN_SUGGESTIONS);
}
