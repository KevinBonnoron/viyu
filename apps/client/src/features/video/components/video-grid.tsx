import { useEffect, useMemo, useRef, useState } from 'react';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { MediaGrid } from '@/components/media-grid';
import { PlatformFilters } from '@/components/platform-filters';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { comparePublicationsNewestFirst } from '@/features/video/utils/publication';
import { useUserSettings } from '@/hooks/useUserSettings';
import { useVisibleVideos } from '@/hooks/useVisibleVideos';
import type { VideoStatus } from '@/lib/video-status';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
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
  loadWhenEmpty = false,
}: {
  videos: VideoResult[];
  sortByPublication?: boolean;
  historyActions?: boolean;
  showWatched?: boolean;
  filterPlatforms?: boolean;
  /** Called when the end of the list comes into view; pass it only while more can be loaded. */
  onEndReached?: () => void;
  /** The local Feed opts in so shelf exclusion cannot strand source pagination. Other lists keep scroll-only paging. */
  loadWhenEmpty?: boolean;
}) {
  const { t } = useI18n();
  const platforms = useMemo(() => [...new Set(videos.map((video) => video.platform))].sort(), [videos]);
  const [selectedPlatform, setSelectedPlatform] = useState<string>();
  const platform = filterPlatforms && selectedPlatform && platforms.includes(selectedPlatform) ? selectedPlatform : undefined;
  const candidates = useMemo(() => (platform ? videos.filter((video) => video.platform === platform) : videos), [videos, platform]);
  // History is a personal list: what was opened stays visible, so it is only filtered in discovery lists.
  const {
    visible,
    statuses,
    isReady: statusesReady,
    isError: statusesError,
    retry: retryStatuses,
  } = useVisibleVideos(candidates, asVideo, {
    discovery: !showWatched,
    hideDismissed: !showWatched,
  });
  const { activeProfile } = useProfiles();
  const { isReady: settingsReady, isError: settingsError } = useUserSettings();
  const isReady = statusesReady && settingsReady;
  const isError = statusesError || settingsError;
  // Appending a page must not withdraw the checked cards or publish unchecked ones.
  const published = useRef<{ profileId: string | undefined; statuses: ReadonlyMap<string, VideoStatus>; ids: ReadonlySet<string> }>({
    profileId: undefined,
    statuses: new Map(),
    ids: new Set(),
  });
  const checked = isReady ? visible : visible.filter((video) => published.current.profileId === activeProfile?.id && published.current.ids.has(video.id));
  useEffect(() => {
    if (isReady) {
      published.current = { profileId: activeProfile?.id, statuses, ids: new Set(visible.map((video) => video.id)) };
    }
  }, [isReady, activeProfile?.id, statuses, visible]);
  // A stream in progress comes before everything else; the rest is newest first.
  const ordered = useMemo(
    () =>
      sortByPublication
        ? [...checked].sort((a, b) => Number(!!b.live) - Number(!!a.live) || comparePublicationsNewestFirst(a.published, b.published))
        : checked,
    [checked, sortByPublication],
  );
  // No observer is mounted when known filters remove every card, even if an unrelated status query failed.
  useEffect(() => {
    if (loadWhenEmpty && visible.length === 0 && settingsReady && (statusesReady || statusesError)) {
      onEndReached?.();
    }
  }, [loadWhenEmpty, visible.length, settingsReady, statusesReady, statusesError, onEndReached]);
  return (
    <div className="space-y-4">
      {filterPlatforms && <PlatformFilters platforms={platforms} selected={platform} onChange={setSelectedPlatform} />}
      {isReady && videos.length > 0 && ordered.length === 0 ? (
        <p className="text-sm text-muted-foreground">{t('video.allHidden')}</p>
      ) : (
        <MediaGrid
          items={ordered}
          identity={identityOf}
          onEndReached={isReady ? onEndReached : undefined}
          renderItem={(video) => {
            const status = statuses.get(video.id) ?? (isReady ? undefined : published.current.statuses.get(video.id));
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
      {!isReady && !isError && (
        <div aria-live="polite" aria-busy="true">
          <span className="sr-only">{t('search.moreLoading')}</span>
          <VideoGridSkeleton />
        </div>
      )}
      {isError && (
        <ErrorSnackbar
          message={t('errors.requestFailed')}
          action={t('common.retry')}
          onAction={settingsError ? () => window.location.reload() : retryStatuses}
        />
      )}
    </div>
  );
}
