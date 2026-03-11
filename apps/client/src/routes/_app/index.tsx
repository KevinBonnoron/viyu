import { createFileRoute, Link, redirect, useLocation } from '@tanstack/react-router';
import { Play, Search } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { EmptyState } from '@/components/empty-state';
import { FilterSkeleton } from '@/components/filter-skeleton';
import { PageTitle } from '@/components/page-title';
import { SourceFeedView, SourceModes } from '@/components/source-feed';
import { Button } from '@/components/ui/button';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { VideoGrid } from '@/features/video';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { useLatestLike } from '@/hooks/useVideoStatuses';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { isSourceActive } from '@/lib/source-activation';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useActiveSource, useSources } from '@/providers/sources-provider';
import { useSubscriptions } from '@/providers/subscriptions-provider';
import type { Source, VideoResult } from '@/types';

export const Route = createFileRoute('/_app/')({
  validateSearch: (): Record<string, never> => ({}),
  beforeLoad: ({ location }) => {
    const params = new URLSearchParams(location.searchStr);
    if (params.has('source')) {
      throw redirect({ to: '/', state: { viyuSource: params.get('source') || undefined }, replace: true });
    }
  },
  component: FeedPage,
});

function FeedPage() {
  const { t } = useI18n();
  const source = useActiveSource(useLocation({ select: (location) => location.state.viyuSource }));
  const navigate = Route.useNavigate();
  const { sources, revision } = useSources();
  const { activeProfile } = useProfiles();
  const homeSources = sources.filter((item) => isSourceActive(item) && item.capabilities.includes('home'));
  return (
    <div className="space-y-6">
      <PageTitle description={t('ui.library.feedDescription')}>
        {t('nav.feed')}
        {source && <span className="font-normal text-muted-foreground"> · {sources.find((item) => item.platform === source)?.name ?? source}</span>}
      </PageTitle>
      <SourceModes
        source={source}
        capability="home"
        localLabel={t('feed.all')}
        pills
        onChange={(selected) => void navigate({ state: { viyuSource: selected } })}
      />
      {source ? (
        <SourceFeedView platform={source} kind="home" />
      ) : (
        <LocalFeed key={`${activeProfile?.id ?? 'none'}:${revision}`} profileId={activeProfile?.id} sources={homeSources} />
      )}
    </div>
  );
}

type RemoteFeed = { source: Source; videos: VideoResult[]; page: number; hasMore: boolean; loading: boolean; error: TranslationKey | null };

function LocalFeed({ profileId, sources }: { profileId?: string; sources: Source[] }) {
  const { t } = useI18n();
  const { latest: latestLike, isReady: likesReady, isError: likesError } = useLatestLike();
  const { data: subscriptions, isReady: subscriptionsReady, isError: subscriptionsError } = useSubscriptions();
  const [videos, setVideos] = useState<VideoResult[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<TranslationKey | null>(null);
  const [retry, setRetry] = useState(0);
  const [initialSources] = useState(() => sources);
  const [remoteFeeds, setRemoteFeeds] = useState<RemoteFeed[]>(() =>
    sources.map((source) => ({ source, videos: [], page: 0, hasMore: false, loading: true, error: null })),
  );
  const remoteAbort = useRef<AbortController | null>(null);

  const loadSource = useCallback(async (platform: string, page: number, signal: AbortSignal) => {
    setRemoteFeeds((feeds) => feeds.map((feed) => (feed.source.platform === platform ? { ...feed, loading: true, error: null } : feed)));
    try {
      const result = await api.sources.feed(platform, 'home', page, { signal });
      if (!signal.aborted) {
        setRemoteFeeds((feeds) =>
          feeds.map((feed) =>
            feed.source.platform === platform
              ? {
                  ...feed,
                  videos: [...(page === 1 ? [] : feed.videos), ...result.videos.map((video) => ({ ...video, source: platform }))],
                  page,
                  hasMore: result.hasMore,
                  loading: false,
                }
              : feed,
          ),
        );
      }
    } catch (failure) {
      if (!signal.aborted) {
        setRemoteFeeds((feeds) => feeds.map((feed) => (feed.source.platform === platform ? { ...feed, loading: false, error: requestError(failure) } : feed)));
      }
    }
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    remoteAbort.current = controller;
    for (const source of initialSources) {
      void loadSource(source.platform, 1, controller.signal);
    }
    return () => {
      remoteAbort.current = null;
      controller.abort();
    };
  }, [initialSources, loadSource]);
  const hasActivity = latestLike !== '' || subscriptions.length > 0;
  const activityKey = `${latestLike}|${subscriptions
    .map((subscription) => subscription.id)
    .sort()
    .join(',')}`;

  useAbortableEffect(
    (signal) => {
      if (!profileId || !likesReady || !subscriptionsReady || !hasActivity) {
        return;
      }
      setLoading(true);
      setError(null);
      return api
        .feed(profileId, { signal })
        .then((data) => {
          if (!signal.aborted) {
            setVideos(data);
          }
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
    [profileId, likesReady, subscriptionsReady, hasActivity, activityKey, retry],
  );

  const merged = new Map<string, VideoResult>();
  for (const feed of remoteFeeds) {
    for (const video of feed.videos) {
      const key = `${video.platform}:${video.id}`;
      if (!merged.has(key)) {
        merged.set(key, video);
      }
    }
  }
  if (hasActivity) {
    for (const video of videos) {
      const key = `${video.platform}:${video.id}`;
      if (!merged.has(key)) {
        merged.set(key, video);
      }
    }
  }
  const combined = [...merged.values()];
  const pending = loading || (!likesReady && !likesError) || (!subscriptionsReady && !subscriptionsError) || remoteFeeds.some((feed) => feed.loading);
  const hasErrors = !!error || likesError || subscriptionsError || remoteFeeds.some((feed) => feed.error);
  const more = remoteFeeds.filter((feed) => feed.hasMore && !feed.loading && !feed.error);
  const loadMore = useCallback(() => {
    const signal = remoteAbort.current?.signal;
    if (!signal || pending) {
      return;
    }
    for (const feed of remoteFeeds) {
      if (feed.hasMore && !feed.loading && !feed.error) {
        void loadSource(feed.source.platform, feed.page + 1, signal);
      }
    }
  }, [remoteFeeds, pending, loadSource]);

  if (!pending && !hasErrors && !hasActivity && remoteFeeds.length === 0) {
    return (
      <EmptyState icon={Play} title={t('feed.welcomeTitle')} description={t('feed.welcomeDescription')} className="min-h-[360px]">
        <Button asChild variant="outline">
          <Link to="/search">
            <Search aria-hidden="true" />
            {t('search.videos')}
          </Link>
        </Button>
      </EmptyState>
    );
  }
  return (
    <div className="space-y-4">
      {(likesError || subscriptionsError) && (
        <p role="alert" className="text-destructive">
          {t('ui.library.loadError')}
        </p>
      )}
      {error && (
        <div role="alert">
          <p>{t(error)}</p>
          <Button variant="outline" onClick={() => setRetry((value) => value + 1)}>
            {t('common.retry')}
          </Button>
        </div>
      )}
      {pending && combined.length === 0 && (
        <div className="space-y-4">
          <FilterSkeleton />
          <VideoGridSkeleton />
        </div>
      )}
      {combined.length > 0 && <VideoGrid videos={combined} historyActions onEndReached={more.length > 0 && !pending ? loadMore : undefined} />}
      {pending && (
        <div aria-live="polite" aria-busy="true" className="space-y-4">
          <span className="sr-only">{t('feed.loading')}</span>
          <VideoGridSkeleton />
        </div>
      )}
      {!pending && !hasErrors && combined.length === 0 && <p className="text-muted-foreground">{remoteFeeds.length ? t('feed.allEmpty') : t('feed.empty')}</p>}
    </div>
  );
}
