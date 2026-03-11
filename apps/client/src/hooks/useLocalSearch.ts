import { and, createLiveQueryCollection, ilike, inArray, like, or } from '@tanstack/react-db';
import { useMemo } from 'react';
import { videoCollection } from '@/collections/video.collection';
import { TRANSLATION_LANGUAGE } from '@/features/video/utils/titles';
import { asRowSource, useChunkedRows } from '@/hooks/useChunkedRows';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';
import { type LocalVideoMatch, searchLocalChannels, searchLocalVideos } from '@/lib/local-search';
import { useSubscriptions } from '@/providers/subscriptions-provider';
import type { ChannelRecord, VideoRecord } from '@/types';

export interface LocalSearchResults {
  channels: ChannelRecord[];
  videos: LocalVideoMatch[];
}

const LOCAL_SEARCH_DEBOUNCE_MS = 200;
const MIN_TERM_LENGTH = 2;
/** How many candidate videos each chunk of followed channels loads before the fuzzy ranking keeps the best few. */
const CANDIDATES = 24;
const NO_IDS: readonly string[] = [];

/** The LIKE pattern of a term: PocketBase and TanStack DB both read `%` and `_` as wildcards, so the term's own are removed. */
function likePattern(term: string): string {
  return `%${term.replace(/[%_\\]/g, '')}%`;
}

/**
 * The library part of a search: the channels the active profile follows whose name matches, and the matching videos of
 * those channels. Nothing else Viyu happens to have cached (results of earlier searches, other people's channels) is
 * part of it. Channels come from the subscriptions already loaded; videos are looked up per chunk of followed channels
 * with a bounded query, then ranked locally. A query shorter than two characters looks nothing up.
 */
export function useLocalSearch(query: string, platform?: string): LocalSearchResults {
  const { data: subscriptions } = useSubscriptions();
  const term = useDebouncedValue(query.trim(), LOCAL_SEARCH_DEBOUNCE_MS);
  const pattern = likePattern(term);
  const active = term.length >= MIN_TERM_LENGTH && pattern.length > 2;
  // PocketBase folds case for ASCII only: a pattern with other characters is matched case-sensitively there and here.
  const ascii = /^[\p{ASCII}]*$/u.test(term);

  const followed = useMemo(
    () =>
      subscriptions.flatMap((subscription) =>
        subscription.channelInfo && (!platform || subscription.channelInfo.platform === platform) ? [subscription.channelInfo] : [],
      ),
    [subscriptions, platform],
  );
  const channelIds = useMemo(() => (active ? followed.map((channel) => channel.id) : NO_IDS), [active, followed]);
  const videos = useChunkedRows<VideoRecord>(channelIds, `${pattern}|${ascii}`, (chunk) =>
    asRowSource(
      createLiveQueryCollection({
        query: (q) =>
          q
            .from({ videos: videoCollection })
            .where(({ videos }) =>
              and(
                inArray(videos.channel, chunk),
                ascii
                  ? or(ilike(videos.titles?.original, pattern), ilike(videos.titles?.[TRANSLATION_LANGUAGE], pattern))
                  : or(like(videos.titles?.original, pattern), like(videos.titles?.[TRANSLATION_LANGUAGE], pattern)),
              ),
            )
            .orderBy(({ videos }) => videos.published, 'desc')
            .limit(CANDIDATES),
        startSync: true,
      }),
    ),
  );

  return useMemo(() => {
    if (!active) {
      return { channels: [], videos: [] };
    }
    const byId = new Map(followed.map((channel) => [channel.id, channel]));
    return {
      channels: searchLocalChannels(followed, term, platform),
      videos: searchLocalVideos([...videos.rows], term, platform).map((record) => ({ record, channel: byId.get(record.channel) })),
    };
  }, [active, followed, videos.rows, term, platform]);
}
