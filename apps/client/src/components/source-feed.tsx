import { Video } from 'lucide-react';
import { useState } from 'react';
import { EmptyState } from '@/components/empty-state';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { PlatformBadge } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { VideoGrid } from '@/features/video';
import { SubscriptionVideos } from '@/features/video/components/subscription-videos';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { isSourceActive } from '@/lib/source-activation';
import { cn } from '@/lib/utils';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useSources } from '@/providers/sources-provider';
import type { SourceFeedKind, VideoResult } from '@/types';

export function SourceModes({
  source,
  capability,
  localLabel = 'Viyu',
  onChange,
  className,
  pills = false,
}: {
  source?: string;
  capability: string;
  localLabel?: string;
  onChange: (source?: string, platform?: string) => void;
  className?: string;
  pills?: boolean;
}) {
  const { sources, loading, error, refresh } = useSources();
  const { t } = useI18n();
  const modes = sources.filter((item) => isSourceActive(item) && item.capabilities.includes(capability));
  // Platforms are filtered in the results, not chosen here: a search only offers a choice when a connected source adds one.
  if (modes.length === 0 && !source && !loading && !error) {
    return null;
  }
  return (
    <div className={cn('space-y-2', className)}>
      <div className={cn('flex flex-wrap items-center justify-between gap-3', pills ? 'pb-1' : 'border-b pb-4')}>
        <section className={cn('flex flex-wrap', pills ? 'gap-2' : 'gap-1 rounded-lg bg-muted p-1')} aria-label={t('sourceFeed.selection')}>
          <Button
            variant="ghost"
            className={cn(
              'h-8 px-3 text-xs text-muted-foreground',
              pills
                ? 'rounded-full border border-transparent aria-pressed:border-border aria-pressed:bg-accent aria-pressed:text-foreground'
                : 'rounded-md aria-pressed:bg-card aria-pressed:text-foreground aria-pressed:shadow-xs',
            )}
            aria-pressed={!source}
            onClick={() => onChange(undefined, capability === 'search' ? 'all' : undefined)}
          >
            {capability === 'search' && <PlatformBadge platform="all" decorative className="size-5 border-0 bg-transparent shadow-none" />}
            {localLabel}
          </Button>
          {modes.map((item) => (
            <Button
              key={item.platform}
              variant="ghost"
              className={cn(
                'h-8 px-3 text-xs text-muted-foreground',
                pills
                  ? 'rounded-full border border-transparent aria-pressed:border-border aria-pressed:bg-accent aria-pressed:text-foreground'
                  : 'rounded-md aria-pressed:bg-card aria-pressed:text-foreground aria-pressed:shadow-xs',
              )}
              aria-pressed={source === item.platform}
              onClick={() => onChange(item.platform, item.platform)}
            >
              <PlatformBadge platform={item.platform} decorative className="size-5 border-0 bg-transparent shadow-none" />
              {item.name}
            </Button>
          ))}
          {source && !modes.some((item) => item.platform === source) && (
            <Button
              aria-pressed
              variant="ghost"
              className={cn('h-8 px-3 text-xs', pills ? 'rounded-full border border-border bg-accent' : 'rounded-md bg-card shadow-xs')}
              onClick={() => onChange(source, source)}
            >
              <PlatformBadge platform={source} decorative className="size-5 border-0 bg-transparent shadow-none" />
              {sources.find((item) => item.platform === source)?.name ?? source}
            </Button>
          )}
        </section>
      </div>
      {loading && <output className="text-xs text-muted-foreground">{t('sourceFeed.loadingConnections')}</output>}
      {error && <ErrorSnackbar message={`${t('sourceFeed.listError')} ${t(error)}`} action={t('common.retry')} onAction={refresh} />}
    </div>
  );
}

export function SourceFeedView({ platform, kind }: { platform: string; kind: SourceFeedKind }) {
  const { revision } = useSources();
  return <ConnectedFeed key={`${platform}:${kind}:${revision}`} platform={platform} kind={kind} />;
}

function ConnectedFeed({ platform, kind }: { platform: string; kind: SourceFeedKind }) {
  const { sources } = useSources();
  const { t } = useI18n();
  const kindLabel = kind === 'home' ? t('nav.home') : kind === 'history' ? t('nav.history') : t('nav.subscriptions');
  const name = sources.find((source) => source.platform === platform)?.name ?? platform;
  const [videos, setVideos] = useState<VideoResult[]>([]);
  const [page, setPage] = useState(1);
  const [hasMore, setHasMore] = useState(false);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<TranslationKey | null>(null);
  const [retry, setRetry] = useState(0);

  useAbortableEffect(
    (signal) => {
      setLoading(true);
      setError(null);
      return api.sources
        .feed(platform, kind, page, { signal })
        .then((data) => {
          if (signal.aborted) {
            return;
          }
          const entries = data.videos.map((video) => ({ ...video, source: platform }));
          setVideos((previous) => (page === 1 ? entries : [...previous, ...entries]));
          setHasMore(data.hasMore);
        })
        .catch((failure: unknown) => {
          if (!signal.aborted) {
            setError(requestError(failure));
          }
        })
        .finally(() => {
          if (!signal.aborted) {
            setLoading(false);
          }
        });
    },
    [platform, kind, page, retry],
  );

  const loadMore = hasMore && !loading && !error ? () => setPage((value) => value + 1) : undefined;

  return (
    <section className="space-y-4" aria-label={`${name} ${kindLabel}`}>
      {kind !== 'home' && (
        <p className="max-w-3xl text-xs leading-relaxed text-muted-foreground">
          {kind === 'history' ? t('sourceFeed.historyDescription', { name }) : t('sourceFeed.subscriptionsDescription', { name })}
        </p>
      )}
      {videos.length > 0 &&
        (kind === 'subscriptions' ? (
          <SubscriptionVideos videos={videos} onEndReached={loadMore} />
        ) : (
          <VideoGrid
            videos={videos}
            sortByPublication={kind !== 'history'}
            historyActions={kind === 'home'}
            showWatched={kind === 'history'}
            onEndReached={loadMore}
          />
        ))}
      {loading && (
        <div aria-live="polite" aria-busy="true" className="space-y-4">
          <span className={kind === 'home' ? 'sr-only' : 'text-xs text-muted-foreground'}>{t('sourceFeed.loading', { name, kind: kindLabel })}</span>
          <VideoGridSkeleton />
        </div>
      )}
      {error && (
        <ErrorSnackbar
          message={t(error)}
          action={page > 1 ? t('sourceFeed.retryPage') : t('sourceFeed.retry')}
          onAction={() => setRetry((value) => value + 1)}
        />
      )}
      {!loading && !error && videos.length === 0 && <EmptyState icon={Video} title={t('sourceFeed.empty', { name })} />}
    </section>
  );
}
