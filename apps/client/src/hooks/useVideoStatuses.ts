import { and, createLiveQueryCollection, eq, inArray, useLiveQuery } from '@tanstack/react-db';
import { useMemo } from 'react';
import { videoReactionCollection } from '@/collections/video-reaction.collection';
import { watchHistoryCollection } from '@/collections/watch-history.collection';
import { asRowSource, useChunkedRows } from '@/hooks/useChunkedRows';
import { buildVideoStatuses, type VideoStatus } from '@/lib/video-status';
import { useProfiles } from '@/providers/profiles-provider';
import type { VideoReaction, VideoReactionRecord, WatchHistoryRecord } from '@/types';

const NO_IDS: readonly string[] = [];

/**
 * What the active profile's history and reactions say about the videos of a list, keyed by `videos` record id. Only the
 * rows of these videos are loaded (in chunks, never one query over the whole history), and the answer is live: a video
 * watched or liked while the list is open changes at once. A video the profile never met has no entry.
 */
export function useVideoStatuses(videoIds: readonly string[]): { statuses: ReadonlyMap<string, VideoStatus>; isReady: boolean; isError: boolean } {
  const { activeProfile, isReady: profileReady, isError: profileError } = useProfiles();
  const profileId = activeProfile?.id ?? '';
  const ids = profileId ? videoIds : NO_IDS;
  const histories = useChunkedRows<WatchHistoryRecord>(ids, profileId, (chunk) =>
    asRowSource(
      createLiveQueryCollection({
        query: (q) => q.from({ history: watchHistoryCollection }).where(({ history }) => and(eq(history.profile, profileId), inArray(history.video, chunk))),
        startSync: true,
      }),
    ),
  );
  const reactions = useChunkedRows<VideoReactionRecord>(ids, profileId, (chunk) =>
    asRowSource(
      createLiveQueryCollection({
        query: (q) =>
          q.from({ reactions: videoReactionCollection }).where(({ reactions }) => and(eq(reactions.profile, profileId), inArray(reactions.video, chunk))),
        startSync: true,
      }),
    ),
  );
  const statuses = useMemo(() => buildVideoStatuses(histories.rows, reactions.rows), [histories.rows, reactions.rows]);
  return {
    statuses,
    isReady: profileReady && !!activeProfile && histories.isReady && reactions.isReady,
    isError: profileError || histories.isError || reactions.isError,
  };
}

/** The reaction of the active profile to one video, live. */
export function useVideoReaction(videoId: string): { reaction: VideoReaction | undefined; isReady: boolean; isError: boolean } {
  const { activeProfile, isReady: profileReady, isError: profileError } = useProfiles();
  const profileId = activeProfile?.id;
  const query = useLiveQuery({
    query: (q) =>
      profileId
        ? q.from({ reactions: videoReactionCollection }).where(({ reactions }) => and(eq(reactions.profile, profileId), eq(reactions.video, videoId)))
        : undefined,
  });
  return { reaction: query.data?.[0]?.reaction, isReady: profileReady && !!profileId && query.isReady, isError: profileError || query.isError };
}

/** The history row of the active profile for one video, live. */
export function useVideoHistory(videoId: string): { row: WatchHistoryRecord | undefined; isReady: boolean; isError: boolean } {
  const { activeProfile, isReady: profileReady, isError: profileError } = useProfiles();
  const profileId = activeProfile?.id;
  const query = useLiveQuery({
    query: (q) =>
      profileId
        ? q.from({ history: watchHistoryCollection }).where(({ history }) => and(eq(history.profile, profileId), eq(history.video, videoId)))
        : undefined,
  });
  return { row: query.data?.[0], isReady: profileReady && !!profileId && query.isReady, isError: profileError || query.isError };
}

/** The profile's most recent like, live: its id changes whenever a like is added or the latest one is taken back, which is all a "did the likes change" check needs. */
export function useLatestLike(): { latest: string; isReady: boolean; isError: boolean } {
  const { activeProfile, isReady: profileReady, isError: profileError } = useProfiles();
  const profileId = activeProfile?.id;
  const query = useLiveQuery({
    query: (q) =>
      profileId
        ? q
            .from({ reactions: videoReactionCollection })
            .where(({ reactions }) => and(eq(reactions.profile, profileId), eq(reactions.reaction, 'like')))
            .orderBy(({ reactions }) => reactions.created, 'desc')
            .limit(1)
        : undefined,
  });
  return { latest: query.data?.[0]?.id ?? '', isReady: profileReady && !!profileId && query.isReady, isError: profileError || query.isError };
}
