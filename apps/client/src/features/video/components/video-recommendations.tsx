import { ErrorSnackbar } from '@/components/error-snackbar';
import { Skeleton } from '@/components/ui/skeleton';
import { VideoCard } from '@/features/video/components/video-card';
import { VideoCardMenu } from '@/features/video/components/video-card-menu';
import { visibleRecommendations } from '@/lib/recommendations';
import type { VideoStatus } from '@/lib/video-status';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import type { VideoResult } from '@/types';

/** The recommendations of the video playing, as one grid. A series' own episodes are in the series tab, not here. */
export function VideoRecommendations({
  videos,
  loading,
  error,
  onRetry,
  statuses,
  statusesReady,
  exclude,
}: {
  videos: readonly VideoResult[];
  loading: boolean;
  error?: TranslationKey;
  onRetry: () => void;
  /** What the profile's history and reactions say about these videos. */
  statuses: ReadonlyMap<string, VideoStatus>;
  statusesReady: boolean;
  exclude?: ReadonlySet<string>;
}) {
  const { t } = useI18n();

  const recommended = visibleRecommendations(videos, statuses, exclude);
  // Same menu as the feed: mark as watched/unwatched, not interested (hidden here at once), copy link.
  const card = (video: VideoResult) => {
    const watched = !video.live && !!statuses.get(video.id)?.watched;
    return (
      <VideoCard
        key={`${video.platform}:${video.source ?? 'public'}:${video.id}`}
        video={video}
        watched={watched}
        actions={
          <VideoCardMenu video={{ video: video.id, title: video.title }} watched={watched} canMarkWatched canDismiss={!video.live} disabled={!statusesReady} />
        }
      />
    );
  };

  return (
    <div className="space-y-4 p-4">
      {recommended.length > 0 && <div className="grid grid-cols-1 gap-4 stacked:grid-cols-2 min-[1400px]:stacked:grid-cols-4">{recommended.map(card)}</div>}
      {loading && recommended.length === 0 && (
        <div
          role="status"
          aria-busy="true"
          aria-label={t('player.recommendations.loading')}
          className="grid grid-cols-1 gap-4 stacked:grid-cols-2 min-[1400px]:stacked:grid-cols-4"
        >
          {['first', 'second', 'third', 'fourth', 'fifth', 'sixth', 'seventh', 'eighth'].map((position) => (
            <div key={position} aria-hidden="true" className="space-y-2">
              <Skeleton className="aspect-video w-full rounded-xl" />
              <Skeleton className="h-4 w-4/5" />
              <Skeleton className="h-3 w-3/5" />
            </div>
          ))}
        </div>
      )}
      {error && <ErrorSnackbar message={`${t('player.recommendations.error')} ${t(error)}`} action={t('common.retry')} onAction={onRetry} />}
      {!loading && !error && recommended.length === 0 && <p className="text-sm text-muted-foreground">{t('player.recommendations.empty')}</p>}
    </div>
  );
}
