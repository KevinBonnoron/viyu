import { eq, queryOnce } from '@tanstack/react-db';
import { useCallback, useState } from 'react';
import { videoCollection } from '@/collections/video.collection';
import { channelCollection } from '@/features/video/collections/channel.collection';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import type { VideoResult } from '@/types';

/**
 * What a page needs to know about a `videos` record besides its id. `nativeId` is the platform's own id of the video:
 * it only serves the third parties the browser talks to directly (SponsorBlock, Return YouTube Dislike) and the
 * Twitch login of a stream's chat; every request to the API is made with the record id.
 */
export interface VideoRef {
  id: string;
  platform: string;
  nativeId: string;
  channelId: string;
}

/** The same for a `channels` record; `nativeId` is the platform's own id of the channel, only used to tell whether a YouTube channel has been resolved yet. */
export interface ChannelRef {
  id: string;
  platform: string;
  nativeId: string;
  name: string;
  thumbnail: string;
}

export type Lookup<T> = { status: 'loading' } | { status: 'ready'; value: T } | { status: 'missing' } | { status: 'error' };

// A record id never changes meaning, so what was resolved once is reused. A redirect that already holds the video's
// details (a pasted link resolved through the API) can also hand them over to the player that opens next.
const videoRefs = new Map<string, VideoRef>();
const channelRefs = new Map<string, ChannelRef>();
const metaSeeds = new Map<string, { video: VideoResult; at: number }>();
const SEED_LIFETIME_MS = 15_000;

/** Keeps the details the API just returned for the player that opens next, so it does not request them again. */
export function seedVideoMeta(video: VideoResult): void {
  metaSeeds.set(video.id, { video, at: Date.now() });
}

export function seededVideoMeta(id: string): VideoResult | undefined {
  const seed = metaSeeds.get(id);
  if (!seed) {
    return undefined;
  }
  if (Date.now() - seed.at > SEED_LIFETIME_MS) {
    metaSeeds.delete(id);
    return undefined;
  }
  return seed.video;
}

/** A lookup found no record: it does not exist, or the viewer may not see it. */
class MissingRecordError extends Error {}

/** Whether a failed lookup means the record does not exist (as opposed to the server being unreachable). */
export function isMissingRecord(failure: unknown): boolean {
  return failure instanceof MissingRecordError;
}

function useLookup<T>(key: string, cache: Map<string, T>, load: (signal: AbortSignal) => Promise<T>): { lookup: Lookup<T>; retry: () => void } {
  const [state, setState] = useState<{ key: string; attempt: number; lookup: Lookup<T> } | null>(null);
  const [attempt, setAttempt] = useState(0);
  const cached = cache.get(key);
  // `load` is deliberately not a dependency: it only closes over `key`.
  useAbortableEffect(
    async (signal) => {
      if (cache.has(key)) {
        return;
      }
      try {
        const value = await load(signal);
        cache.set(key, value);
        if (!signal.aborted) {
          setState({ key, attempt, lookup: { status: 'ready', value } });
        }
      } catch (failure) {
        if (!signal.aborted) {
          setState({ key, attempt, lookup: { status: isMissingRecord(failure) ? 'missing' : 'error' } });
        }
      }
    },
    [key, attempt, cache],
  );
  const retry = useCallback(() => setAttempt((value) => value + 1), []);
  if (cached) {
    return { lookup: { status: 'ready', value: cached }, retry };
  }
  return { lookup: state && state.key === key && state.attempt === attempt ? state.lookup : { status: 'loading' }, retry };
}

/** Resolves a `videos` record id to what the player needs besides it. */
export function useVideoRecord(recordId: string) {
  return useLookup(recordId, videoRefs, async () => {
    // A point lookup through the collection: one record by id, never the whole `videos` table.
    const [record] = await queryOnce((q) => q.from({ videos: videoCollection }).where(({ videos }) => eq(videos.id, recordId)));
    if (!record) {
      throw new MissingRecordError();
    }
    return { id: record.id, platform: record.platform, nativeId: record.platform_id, channelId: record.channel };
  });
}

/** Resolves a `channels` record id to what a channel page needs besides it. */
export function useChannelRecord(recordId: string) {
  return useLookup(recordId, channelRefs, async () => {
    const [record] = await queryOnce((q) => q.from({ channels: channelCollection }).where(({ channels }) => eq(channels.id, recordId)));
    if (!record) {
      throw new MissingRecordError();
    }
    return { id: record.id, platform: record.platform, nativeId: record.platform_id, name: record.name, thumbnail: record.thumbnail };
  });
}
