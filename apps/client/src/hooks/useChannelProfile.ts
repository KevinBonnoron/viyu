import { eq, useLiveQuery } from '@tanstack/react-db';
import { channelCollection } from '@/features/video/collections/channel.collection';

/**
 * The cached record of a public channel, kept current by PocketBase's realtime updates: a point lookup by id, never
 * the whole channels collection. The server fills avatars and subscriber counts in the background, so nothing is requested from here.
 */
export function useChannelProfile(channelId: string | null) {
  const { data } = useLiveQuery({
    query: (q) => (channelId ? q.from({ channels: channelCollection }).where(({ channels }) => eq(channels.id, channelId)) : undefined),
  });
  return data?.[0];
}
