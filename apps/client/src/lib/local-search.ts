import Fuse, { type IFuseOptions } from 'fuse.js';
import { publishedFromRecord } from '@/features/video/utils/publication';
import { recordTitles } from '@/features/video/utils/titles';
import type { ChannelRecord, VideoRecord, VideoResult } from '@/types';

/** How many local matches each kind contributes to the quick surfaces. */
export const LOCAL_CHANNEL_LIMIT = 3;
export const LOCAL_VIDEO_LIMIT = 5;

/** A query shorter than this has too many fuzzy matches to be useful. */
const MIN_QUERY_LENGTH = 2;

const channelFuseOptions: IFuseOptions<ChannelRecord> = { keys: ['name'], ignoreLocation: true, threshold: 0.3, minMatchCharLength: MIN_QUERY_LENGTH };
const videoFuseOptions: IFuseOptions<VideoRecord> = {
  keys: [
    { name: 'title', weight: 2, getFn: (record) => recordTitles(record.titles).title },
    { name: 'originalTitle', weight: 1, getFn: (record) => recordTitles(record.titles).originalTitle },
  ],
  ignoreLocation: true,
  threshold: 0.35,
  minMatchCharLength: MIN_QUERY_LENGTH,
};

/**
 * Instant local search over records Viyu already holds, the way melody-manager searches the library:
 * purely client-side, so the answer is there in the same keystroke. Channels are the known channels
 * (subscriptions and everything cached); videos are the ones the profile actually knows (its history
 * and likes) — the videos table holds every video ever seen, far too much to index for a palette.
 */
export function searchLocalChannels(channels: ChannelRecord[], query: string, platform?: string): ChannelRecord[] {
  const pool = platform ? channels.filter((channel) => channel.platform === platform) : channels;
  const trimmed = query.trim();
  if (trimmed.length < MIN_QUERY_LENGTH || pool.length === 0) {
    return [];
  }
  return new Fuse(pool, channelFuseOptions)
    .search(trimmed)
    .slice(0, LOCAL_CHANNEL_LIMIT)
    .map((result) => result.item);
}

export function searchLocalVideos(videos: VideoRecord[], query: string, platform?: string): VideoRecord[] {
  const pool = platform ? videos.filter((video) => video.platform === platform) : videos;
  const trimmed = query.trim();
  if (trimmed.length < MIN_QUERY_LENGTH || pool.length === 0) {
    return [];
  }
  return new Fuse(pool, videoFuseOptions)
    .search(trimmed)
    .slice(0, LOCAL_VIDEO_LIMIT)
    .map((result) => result.item);
}

/** A local video match with the channel record it belongs to, when it is known. */
export interface LocalVideoMatch {
  record: VideoRecord;
  channel?: ChannelRecord;
}

/** Presents a local video record like a search answer, so cards, links and the player treat it the same. */
export function localVideoResult({ record, channel }: LocalVideoMatch): VideoResult {
  const { title, originalTitle } = recordTitles(record.titles);
  return {
    id: record.id,
    platform: record.platform,
    title,
    originalTitle,
    originalTitleVerified: record.metadata?.original_title_verified ?? false,
    originalLanguage: record.metadata?.original_language ?? '',
    channelId: channel?.id ?? (typeof record.channel === 'string' ? record.channel : ''),
    channelName: channel?.name ?? '',
    channelThumbnail: channel?.thumbnail ?? '',
    thumbnail: record.thumbnails?.[0] ?? '',
    thumbnails: record.thumbnails ?? [],
    duration: record.duration ?? 0,
    description: record.description ?? '',
    tags: record.tags ?? [],
    published: publishedFromRecord(record.published),
    viewCount: record.view_count ?? 0,
    likeCount: record.metadata?.like_count ?? null,
    dislikeCount: record.metadata?.dislike_count ?? null,
    subscriberOnly: record.subscriber_only,
    category: record.category,
  };
}
