import type { VideoRecord, VideoResult } from '@/types';
import { publishedFromRecord } from './publication';
import { recordTitles } from './titles';

export function videoRecordToResult(record: VideoRecord): VideoResult {
  const channel = record.expand?.channel;
  const { title, originalTitle } = recordTitles(record.titles);
  return {
    id: record.id,
    platform: record.platform,
    title,
    originalTitle,
    originalTitleVerified: record.metadata?.original_title_verified ?? false,
    originalLanguage: record.metadata?.original_language || '',
    channelId: record.channel,
    channelName: channel?.name || '',
    channelThumbnail: channel?.thumbnail || '',
    thumbnails: record.thumbnails || [],
    thumbnail: record.thumbnails?.find((thumbnail) => thumbnail.trim()) || '',
    creators: record.creators || [],
    description: record.description || '',
    duration: record.duration,
    viewCount: record.view_count || 0,
    likeCount: record.metadata?.like_count ?? null,
    dislikeCount: record.metadata?.dislike_count ?? null,
    tags: record.tags || [],
    category: record.category || undefined,
    published: publishedFromRecord(record.published),
    subscriberOnly: record.subscriber_only ?? false,
  };
}
