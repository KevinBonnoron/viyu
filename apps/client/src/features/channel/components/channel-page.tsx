import { Link } from '@tanstack/react-router';
import { ListVideo, UserCheck, UserPlus } from 'lucide-react';
import { useMemo, useRef, useState } from 'react';
import { ChannelAvatar } from '@/components/channel-avatar';
import { PageTitle } from '@/components/page-title';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { Snackbar } from '@/components/ui/snackbar';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { VirtualGrid } from '@/components/virtual-grid';
import { VideoGrid } from '@/features/video';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { useChannelLive } from '@/hooks/useChannelLive';
import { useChannelSeries } from '@/hooks/useChannelSeries';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';
import { useChannelSubscription } from '@/hooks/useSubscriptions';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { ChannelLink, VideoLink } from '@/lib/links';
import { sleep } from '@/lib/sleep';
import { isSourceActive } from '@/lib/source-activation';
import { foldSearch } from '@/lib/text';
import { formatCompactNumber } from '@/lib/utils';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useSources } from '@/providers/sources-provider';
import type { ChannelInfo, ChannelSync, VideoResult } from '@/types';
import type { SeriesSummary, SeriesSync } from '@/types/series.type';

/** A channel page for the channel of a `channels` record. `nativeId` is the platform's own id of the channel, only used to tell whether the channel has been resolved yet. */
export function ChannelScreen({
  channelId,
  platform,
  nativeId,
  source,
  series,
}: {
  channelId: string;
  platform: string;
  nativeId: string;
  source?: string;
  series: boolean;
}) {
  return (
    <Channel
      key={`${platform}:${source ?? 'public'}:${channelId}`}
      channelId={channelId}
      source={source}
      platform={platform}
      nativeId={nativeId}
      series={series && platform === 'youtube'}
    />
  );
}

function Channel({
  channelId,
  source,
  platform,
  nativeId,
  series,
}: {
  channelId: string;
  source?: string;
  platform: string;
  nativeId: string;
  series: boolean;
}) {
  const { t, language } = useI18n();
  const { sources } = useSources();
  const [channel, setChannel] = useState<ChannelInfo | null>(null);
  const [videos, setVideos] = useState<VideoResult[]>([]);
  const [loading, setLoading] = useState(true);
  const [hasResult, setHasResult] = useState(false);
  const [sync, setSync] = useState<ChannelSync | null>(null);
  const [error, setError] = useState<TranslationKey | null>(null);
  const [retry, setRetry] = useState(0);
  const [search, setSearch] = useState('');
  const [seriesSearch, setSeriesSearch] = useState('');
  const query = useDebouncedValue(search.trim(), 300);
  const [requested, setRequested] = useState<{ query: string; page: number }>({ query: '', page: 1 });
  const [resultKey, setResultKey] = useState<{ query: string } | null>(null);
  const [hasMore, setHasMore] = useState(false);
  const loaded = useRef<{ query: string; page: number; videos: VideoResult[] } | null>(null);
  const lastRetry = useRef(0);
  const page = requested.query === query ? requested.page : 1;
  const syncing = sync?.status === 'pending' || sync?.status === 'running';
  const channelSeries = useChannelSeries(channelId, platform === 'youtube', syncing);
  const subscription = useChannelSubscription(channel ? { id: channelId, platform: channel.platform, name: channel.name, thumbnail: channel.thumbnail } : null);
  const live = useChannelLive(channelId);
  const target = { id: channelId, platform, source };

  const videoResultReady = resultKey?.query === query;
  useAbortableEffect(
    async (signal) => {
      if (requested.query !== query) {
        setRequested({ query, page: 1 });
      }
      setLoading(true);
      setError(null);
      let retrySync = retry !== lastRetry.current;
      let firstPass = true;
      let crawling = true;
      while (crawling && !signal.aborted) {
        try {
          const previous = loaded.current;
          const append = firstPass && previous?.query === query && previous.page < page;
          const catalog = new Map<string, VideoResult>(append ? previous.videos.map((video) => [`${video.platform}:${video.id}`, video]) : []);
          const pages = [];
          for (let current = append ? previous.page + 1 : 1; current <= page; current++) {
            const result = await api.channels.get(channelId, {
              source,
              signal,
              sync: platform === 'youtube',
              retry: retrySync && current === (append ? previous.page + 1 : 1),
              page: current,
              limit: 30,
              q: query,
            });
            for (const video of result.videos) {
              catalog.set(`${video.platform}:${video.id}`, video);
            }
            pages.push(result);
            if (!result.hasMore) {
              break;
            }
          }
          const data = pages[pages.length - 1];
          if (signal.aborted) {
            return;
          }
          lastRetry.current = retry;
          setChannel((current) => data.channel ?? current);
          const nextVideos = Array.from(catalog.values());
          loaded.current = { query, page: data.page, videos: nextVideos };
          setVideos(nextVideos);
          setResultKey({ query });
          setHasMore(data.hasMore);
          setHasResult(true);
          setSync(data.sync ?? null);
          crawling = data.sync?.status === 'pending' || data.sync?.status === 'running';
        } catch (failure: unknown) {
          crawling = false;
          if (!signal.aborted) {
            setError(requestError(failure));
          }
        }
        if (signal.aborted) {
          return;
        }
        setLoading(false);
        retrySync = false;
        firstPass = false;
        if (crawling) {
          await sleep(2000, signal);
        }
      }
    },
    [channelId, source, platform, retry, query, page],
  );

  // A video flagged live is only that while the live lookup says so: a stream that ended keeps its flag until the server notices.
  const channelVideos = useMemo(
    () =>
      videos
        .filter((video) => !video.live || video.id === live?.recordId)
        .map((video) =>
          sources.some((item) => isSourceActive(item) && item.platform === video.platform && item.capabilities.includes('stream'))
            ? { ...video, source: video.platform }
            : video,
        ),
    [videos, sources, live?.recordId],
  );
  // An unresolved YouTube channel is named after its own ID. A Twitch channel's name is routinely its login, so equality says nothing there.
  const named = !!channel?.name && !(channel.platform === 'youtube' && channel.name === nativeId);
  const waitingForChannel = !error && (loading || syncing) && !named;
  const createdAt = channel?.createdAt ? new Date(channel.createdAt) : null;
  const createdDate =
    createdAt && !Number.isNaN(createdAt.getTime())
      ? createdAt.toLocaleDateString(language, { day: 'numeric', month: 'long', year: 'numeric', timeZone: 'UTC' })
      : null;

  return (
    <div className="space-y-6">
      {loading && !hasResult && (
        <section aria-live="polite" aria-busy="true" aria-label={t('channel.loading')} className="space-y-6">
          <ChannelSkeleton />
        </section>
      )}
      {hasResult && (
        <>
          {waitingForChannel ? (
            <section aria-live="polite" aria-busy="true" aria-label={t('channel.loading')}>
              <ChannelHeaderSkeleton />
            </section>
          ) : (
            <div className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-4 rounded-2xl border bg-card p-4 sm:gap-5 sm:p-6 lg:grid-cols-[auto_minmax(0,1fr)_auto]">
              <ChannelAvatar src={channel?.thumbnail} className="size-16 sm:size-20" />
              <div className="min-w-0">
                <PageTitle>{named && channel ? channel.name : t('player.unknownChannel')}</PageTitle>
                {live && (
                  <p className="mt-1 inline-flex items-center gap-1.5 whitespace-nowrap rounded-md bg-red-600 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide text-white">
                    <span aria-hidden="true" className="size-1.5 animate-pulse rounded-full bg-white" />
                    {t('channel.live')}
                  </p>
                )}
                {channel && channel.subscriberCount > 0 && (
                  <p className="text-sm text-muted-foreground" title={t('channel.subscribers', { count: channel.subscriberCount.toLocaleString(language) })}>
                    {t('channel.subscribers', { count: formatCompactNumber(channel.subscriberCount, language) })}
                  </p>
                )}
                {channel?.description && <p className="text-sm text-muted-foreground mt-1 line-clamp-2 [overflow-wrap:anywhere]">{channel.description}</p>}
              </div>
              <div className="col-span-2 flex flex-wrap items-center gap-2 lg:col-span-1 lg:justify-end">
                {live?.recordId && (
                  <Button asChild className="grow rounded-full bg-red-600 text-white hover:bg-red-600/90 lg:grow-0">
                    <VideoLink video={{ id: live.recordId, channelId, live: true, source }} title={live.title}>
                      <span aria-hidden="true" className="size-2 animate-pulse rounded-full bg-white" />
                      {t('channel.watchLive')}
                    </VideoLink>
                  </Button>
                )}
                <Button
                  variant={subscription.subscribed ? 'secondary' : 'default'}
                  className="grow rounded-full lg:grow-0"
                  disabled={subscription.disabled}
                  aria-pressed={subscription.subscribed}
                  onClick={() => void subscription.toggle()}
                >
                  {subscription.subscribed ? <UserCheck aria-hidden="true" className="size-4" /> : <UserPlus aria-hidden="true" className="size-4" />}
                  {t(subscription.subscribed ? 'player.subscribed' : 'player.subscribe')}
                </Button>
              </div>
              {channel && (createdDate || channel.country || channel.videoCount != null || channel.viewCount != null) && (
                <dl className="col-span-full grid grid-cols-2 gap-x-4 gap-y-3 border-t pt-4 text-sm sm:flex sm:flex-wrap sm:gap-x-8">
                  {createdDate && (
                    <div className="min-w-0">
                      <dt className="text-xs text-muted-foreground">{t('channel.createdAt')}</dt>
                      <dd className="mt-1 font-medium">
                        <time dateTime={channel.createdAt}>{createdDate}</time>
                      </dd>
                    </div>
                  )}
                  {channel.country && (
                    <div className="min-w-0">
                      <dt className="text-xs text-muted-foreground">{t('channel.country')}</dt>
                      <dd className="mt-1 break-words font-medium">{channel.country}</dd>
                    </div>
                  )}
                  {channel.videoCount != null && (
                    <div className="min-w-0">
                      <dt className="text-xs text-muted-foreground">{t('channel.videoCount')}</dt>
                      <dd className="mt-1 break-words font-medium tabular-nums">{channel.videoCount.toLocaleString(language)}</dd>
                    </div>
                  )}
                  {channel.viewCount != null && (
                    <div className="min-w-0">
                      <dt className="text-xs text-muted-foreground">{t('channel.viewCount')}</dt>
                      <dd className="mt-1 break-words font-medium tabular-nums">{channel.viewCount.toLocaleString(language)}</dd>
                    </div>
                  )}
                </dl>
              )}
            </div>
          )}
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b">
              <nav aria-label={t('channel.tabs')} className="flex gap-2">
                <ChannelLink
                  channel={target}
                  resetScroll={false}
                  tab="videos"
                  aria-current={!series ? 'page' : undefined}
                  className={`flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium ${!series ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
                >
                  {t('channel.videos')}
                  {sync && (
                    <span
                      title={t('channel.sync.loaded', { count: sync.loaded.toLocaleString(language) })}
                      className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium tabular-nums text-muted-foreground"
                    >
                      <span aria-hidden="true">{sync.loaded.toLocaleString(language)}</span>
                      <span className="sr-only">{t('channel.sync.loaded', { count: sync.loaded.toLocaleString(language) })}</span>
                    </span>
                  )}
                </ChannelLink>
                {platform === 'youtube' && (
                  <ChannelLink
                    channel={target}
                    resetScroll={false}
                    tab="series"
                    aria-current={series ? 'page' : undefined}
                    className={`flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium ${series ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
                  >
                    {t('nav.series')}
                    <SeriesCount count={channelSeries.series.length} />
                  </ChannelLink>
                )}
              </nav>
              <div className="mb-1 flex w-full flex-wrap items-center gap-2 sm:w-auto">
                <Input
                  type="search"
                  aria-label={t('channel.search')}
                  placeholder={t('channel.search')}
                  value={series ? seriesSearch : search}
                  onChange={(event) => (series ? setSeriesSearch(event.target.value) : setSearch(event.target.value))}
                  className="min-w-0 flex-1 sm:w-72 sm:flex-none"
                />
              </div>
            </div>
            <div hidden={series}>
              {videoResultReady && channelVideos.length > 0 ? (
                <VideoGrid
                  videos={channelVideos}
                  sortByPublication={false}
                  historyActions
                  onEndReached={!series && hasMore && !loading && !error ? () => setRequested({ query, page: page + 1 }) : undefined}
                />
              ) : (!videoResultReady || syncing || loading) && !error ? (
                <VideoGridSkeleton />
              ) : !error && query ? (
                <p className="py-8 text-center text-sm text-muted-foreground">{t('subscriptions.noSearchResults')}</p>
              ) : !error && sync?.status !== 'error' ? (
                <p className="text-muted-foreground">{t('channel.empty')}</p>
              ) : null}
              {loading && videoResultReady && channelVideos.length > 0 && <VideoGridSkeleton fillViewport={false} />}
            </div>
            {platform === 'youtube' && (
              <div hidden={!series}>
                <ChannelSeries
                  series={channelSeries.series}
                  seriesSync={channelSeries.sync}
                  error={channelSeries.error}
                  retry={channelSeries.retry}
                  sync={sync}
                  query={seriesSearch}
                />
              </div>
            )}
          </div>
        </>
      )}
      {(error || (!loading && sync?.status === 'error') || subscription.error) && (
        <div className="fixed bottom-4 right-4 z-50 flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2">
          {error && <Snackbar message={t(error)} action={t('channel.retry')} disabled={loading} onAction={() => setRetry((value) => value + 1)} />}
          {!error && !loading && sync?.status === 'error' && (
            <Snackbar message={t('channel.sync.error')} action={t('channel.sync.retry')} onAction={() => setRetry((value) => value + 1)} />
          )}
          {subscription.error && (
            <Snackbar
              message={t('channel.subscription.failed')}
              action={t('common.retry')}
              disabled={subscription.disabled}
              onAction={() => void subscription.toggle()}
            />
          )}
        </div>
      )}
    </div>
  );
}

function ChannelHeaderSkeleton() {
  return (
    <div
      aria-hidden="true"
      className="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-4 rounded-2xl border bg-card p-4 sm:gap-5 sm:p-6 lg:grid-cols-[auto_minmax(0,1fr)_auto]"
    >
      <Skeleton className="size-16 rounded-full sm:size-20" />
      <div className="min-w-0">
        <Skeleton className="h-6 w-48 max-w-full desktop:h-9 desktop:w-64 desktop:lg:h-10.5" />
        <Skeleton className="h-5 w-28 max-w-full" />
        <div className="mt-1 space-y-2">
          <Skeleton className="h-4 w-full max-w-md" />
          <Skeleton className="h-4 w-3/4 max-w-sm" />
        </div>
      </div>
      <div className="col-span-2 flex lg:col-span-1 lg:justify-end">
        <Skeleton className="h-10 grow rounded-full lg:w-32 lg:grow-0" />
      </div>
      <div className="col-span-full grid grid-cols-2 gap-x-4 gap-y-3 border-t pt-4 sm:flex sm:flex-wrap sm:gap-x-8">
        {[0, 1, 2, 3].map((item) => (
          <div key={item} className="space-y-1">
            <Skeleton className="h-4 w-16" />
            <Skeleton className="h-5 w-24" />
          </div>
        ))}
      </div>
    </div>
  );
}

export function ChannelSkeleton() {
  return (
    <>
      <ChannelHeaderSkeleton />
      <div aria-hidden="true" className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b">
          <div className="flex gap-2">
            <div className="flex items-center gap-2 border-b-2 border-transparent px-3 py-2">
              <Skeleton className="h-5 w-16" />
              <Skeleton className="h-5 w-10 rounded-full" />
            </div>
            <div className="flex items-center gap-2 border-b-2 border-transparent px-3 py-2">
              <Skeleton className="h-5 w-14" />
              <Skeleton className="h-5 w-8 rounded-full" />
            </div>
          </div>
          <div className="mb-1 flex w-full flex-wrap items-center gap-2 sm:w-auto">
            <Skeleton className="h-10 min-w-0 flex-1 rounded-lg sm:w-72 sm:flex-none" />
          </div>
        </div>
        <VideoGridSkeleton />
      </div>
    </>
  );
}

/** What the channel route shows while it resolves the record, identical to the page's own loading state. */
export function ChannelLoading() {
  const { t } = useI18n();
  return (
    <section aria-live="polite" aria-busy="true" aria-label={t('channel.loading')} className="space-y-6">
      <ChannelSkeleton />
    </section>
  );
}
function SeriesCount({ count }: { count: number }) {
  const { t, language } = useI18n();
  if (count === 0) {
    return null;
  }
  return (
    <span
      title={t('series.count', { count: count.toLocaleString(language) })}
      className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium tabular-nums text-muted-foreground"
    >
      <span aria-hidden="true">{count.toLocaleString(language)}</span>
      <span className="sr-only">{t('series.count', { count: count.toLocaleString(language) })}</span>
    </span>
  );
}

function ChannelSeries({
  series: channelSeries,
  seriesSync,
  error,
  retry,
  sync,
  query,
}: {
  series: SeriesSummary[];
  seriesSync: SeriesSync | null;
  error: boolean;
  retry: () => void;
  sync: ChannelSync | null;
  query: string;
}) {
  const { t, language } = useI18n();
  if (error) {
    return (
      <Button variant="outline" onClick={retry}>
        {t('errors.requestFailed')} {t('common.retry')}
      </Button>
    );
  }
  const synchronizing = sync?.status === 'pending' || sync?.status === 'running' || seriesSync?.status === 'pending' || seriesSync?.status === 'running';
  const needle = foldSearch(query.trim());
  const visibleSeries = needle ? channelSeries.filter((series) => foldSearch(series.title).includes(needle)) : channelSeries;
  return (
    <div className="space-y-4">
      {synchronizing && <p className="text-sm text-muted-foreground">{t('series.syncing', { count: sync?.loaded ?? 0 })}</p>}
      {visibleSeries.length > 0 ? (
        <VirtualGrid
          items={visibleSeries}
          getKey={(series) => series.id}
          className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3"
          renderItem={(series) => {
            const published = series.latestPublishedAt ? Date.parse(series.latestPublishedAt) : Number.NaN;
            const publicationDate = Number.isFinite(published)
              ? new Intl.DateTimeFormat(language, { dateStyle: 'medium', timeZone: 'UTC' }).format(published)
              : null;
            return (
              <article className="overflow-hidden rounded-2xl border bg-card">
                <Link to="/series/$seriesId" params={{ seriesId: series.id }} className="group block focus-visible:outline-2 focus-visible:outline-ring">
                  <div className="flex aspect-video items-center justify-center bg-muted">
                    {series.thumbnail ? (
                      <img src={series.thumbnail} alt="" loading="lazy" className="size-full object-cover" />
                    ) : (
                      <ListVideo aria-hidden="true" className="size-12 text-muted-foreground" />
                    )}
                  </div>
                  <h2 className="px-4 pt-4 font-semibold group-hover:underline">{series.title}</h2>
                </Link>
                <div className="space-y-2 p-4 pt-2 text-sm text-muted-foreground">
                  <p>{t('series.videoCount', { count: series.videoCount })}</p>
                  <p>
                    {publicationDate ? (
                      <time dateTime={series.latestPublishedAt}>{t('series.latestPublication', { date: publicationDate })}</time>
                    ) : (
                      t('series.unknownDate')
                    )}
                  </p>
                </div>
              </article>
            );
          }}
        />
      ) : channelSeries.length > 0 ? (
        <p className="py-8 text-center text-sm text-muted-foreground">{t('series.noSearchResults')}</p>
      ) : !synchronizing && seriesSync?.status === 'complete' ? (
        <p className="text-muted-foreground">{t('series.noChannelSeries')}</p>
      ) : null}
    </div>
  );
}
