import { useMemo, useState } from 'react';
import { ChannelFilters } from '@/components/channel-filters';
import { VideoGrid } from '@/features/video/components/video-grid';
import { channelIdentity } from '@/lib/channel-groups';
import { useI18n } from '@/providers/i18n-provider';
import type { ChannelResult, VideoResult } from '@/types';

const noChannels: ChannelResult[] = [];

export function SubscriptionVideos({
  videos,
  channels: provided = noChannels,
  onEndReached,
}: {
  videos: VideoResult[];
  channels?: ChannelResult[];
  onEndReached?: () => void;
}) {
  const { t, language } = useI18n();
  const [selected, setSelected] = useState<string>();
  const channels = useMemo(() => {
    const found = new Map<string, ChannelResult>();
    for (const channel of provided) {
      found.set(channelIdentity(channel), { ...channel });
    }
    for (const video of videos) {
      if (!video.channelId) {
        continue;
      }
      const channel = { id: video.channelId, platform: video.platform, source: video.source, name: video.channelName, thumbnail: video.channelThumbnail };
      const key = channelIdentity(channel);
      const known = found.get(key);
      if (!known) {
        found.set(key, channel);
      } else if (!known.thumbnail && channel.thumbnail) {
        known.thumbnail = channel.thumbnail;
      }
    }
    return [...found.values()].sort(
      (a, b) => a.name.localeCompare(b.name, language, { sensitivity: 'base', numeric: true }) || channelIdentity(a).localeCompare(channelIdentity(b)),
    );
  }, [provided, videos, language]);
  const active = channels.find((channel) => channelIdentity(channel) === selected);
  const visible = active
    ? videos.filter((video) => video.channelId === active.id && video.platform === active.platform && video.source === active.source)
    : videos;
  return (
    <div className="min-w-0 space-y-5">
      {channels.length > 0 && <ChannelFilters channels={channels} selected={active ? channelIdentity(active) : undefined} onSelect={setSelected} />}
      {visible.length > 0 ? (
        <VideoGrid videos={visible} historyActions onEndReached={onEndReached} />
      ) : (
        <p className="py-8 text-center text-sm text-muted-foreground">{t('subscriptions.filter.empty')}</p>
      )}
    </div>
  );
}
