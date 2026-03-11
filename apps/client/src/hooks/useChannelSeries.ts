import { eq, useLiveQuery } from '@tanstack/react-db';
import { useMemo } from 'react';
import { seriesCollection } from '@/collections/series.collection';
import type { VideoResult } from '@/types';

/** A channel's series as the tab's cards need it, derived from the record and the catalog videos. */
export interface ChannelSeriesSummary {
  id: string;
  title: string;
  thumbnail: string;
  videoCount: number;
  /** Day of the newest member the catalog knows; empty when no member's publication is known. */
  latestPublishedAt: string;
}

/**
 * The series of one channel, live from the `series` collection. Records hold only the ordered member
 * ids, so the card fields are derived from the channel's catalog videos: the first artwork found and
 * the newest publication day. Sorted like the server used to: newest publication first, then name.
 */
export function useChannelSeries(channelId: string, videos: VideoResult[]): ChannelSeriesSummary[] {
  const query = useLiveQuery({
    query: (q) => (channelId ? q.from({ series: seriesCollection }).where(({ series }) => eq(series.channel, channelId)) : undefined),
  });
  return useMemo(() => {
    const byId = new Map(videos.map((video) => [video.id, video]));
    const summaries = (query.data ?? []).map((record): ChannelSeriesSummary => {
      let thumbnail = '';
      let latestPublishedAt = '';
      for (const id of record.videos ?? []) {
        const video = byId.get(id);
        if (!video) {
          continue;
        }
        if (!thumbnail && video.thumbnail) {
          thumbnail = video.thumbnail;
        }
        const day = video.published.slice(0, 10);
        if (day > latestPublishedAt) {
          latestPublishedAt = day;
        }
      }
      return { id: record.id, title: record.name, thumbnail, videoCount: (record.videos ?? []).length, latestPublishedAt };
    });
    summaries.sort((a, b) => b.latestPublishedAt.localeCompare(a.latestPublishedAt) || a.title.localeCompare(b.title) || a.id.localeCompare(b.id));
    return summaries;
  }, [query.data, videos]);
}
