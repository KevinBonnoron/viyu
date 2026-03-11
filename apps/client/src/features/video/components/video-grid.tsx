import { useMemo, useState } from 'react';
import { MediaGrid } from '@/components/media-grid';
import { PlatformFilters } from '@/components/platform-filters';
import { comparePublicationsNewestFirst } from '@/features/video/utils/publication';
import { useVisibleVideos } from '@/hooks/useVisibleVideos';
import { useI18n } from '@/providers/i18n-provider';
import type { VideoResult } from '@/types';
import { VideoCard } from './video-card';
import { VideoCardMenu } from './video-card-menu';

const asVideo = (video: VideoResult) => video;
const identityOf = (video: VideoResult) => `${video.platform}:${video.source ?? 'public'}:${video.id}`;

export function VideoGrid({
  videos,
  sortByPublication = true,
  historyActions = false,
  showWatched = false,
  filterPlatforms = true,
  onEndReached,
}: {
  videos: VideoResult[];
  sortByPublication?: boolean;
  historyActions?: boolean;
  showWatched?: boolean;
  filterPlatforms?: boolean;
  /** Called when the end of the list comes into view; pass it only while more can be loaded. */
  onEndReached?: () => void;
}) {
  const { t } = useI18n();
  const platforms = useMemo(() => [...new Set(videos.map((video) => video.platform))].sort(), [videos]);
  const [selectedPlatform, setSelectedPlatform] = useState<string>();
  const platform = filterPlatforms && selectedPlatform && platforms.includes(selectedPlatform) ? selectedPlatform : undefined;
  const candidates = useMemo(() => (platform ? videos.filter((video) => video.platform === platform) : videos), [videos, platform]);
  // History is a personal list: what was opened stays visible, so it is only filtered in discovery lists.
  const { visible, statuses, isReady, isError } = useVisibleVideos(candidates, asVideo, { discovery: !showWatched, hideDismissed: !showWatched });
  // A stream in progress comes before everything else; the rest is newest first.
  const ordered = useMemo(
    () =>
      sortByPublication
        ? [...visible].sort((a, b) => Number(!!b.live) - Number(!!a.live) || comparePublicationsNewestFirst(a.published, b.published))
        : visible,
    [visible, sortByPublication],
  );
  return (
    <div className="space-y-4">
      {filterPlatforms && <PlatformFilters platforms={platforms} selected={platform} onChange={setSelectedPlatform} />}
      {videos.length > 0 && ordered.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('video.allHidden')}</p>
      ) : (
        <MediaGrid
          items={ordered}
          identity={identityOf}
          onEndReached={onEndReached}
          renderItem={(video) => {
            const status = statuses.get(video.id);
            const isWatched = !video.live && !!status?.watched;
            return (
              <VideoCard
                video={video}
                watched={isWatched}
                progress={video.live ? undefined : status?.progress}
                actions={
                  historyActions ? (
                    <VideoCardMenu
                      video={{ video: video.id, title: video.title }}
                      watched={isWatched}
                      canMarkWatched
                      canDismiss={!video.live}
                      disabled={!isReady || isError}
                    />
                  ) : undefined
                }
              />
            );
          }}
        />
      )}
    </div>
  );
}
