import { Link } from '@tanstack/react-router';
import { ListVideo, UserCheck, UserPlus } from 'lucide-react';
import { useMemo, useState } from 'react';
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
import { useChannelSubscription } from '@/hooks/useSubscriptions';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { ChannelLink, VideoLink } from '@/lib/links';
import { sleep } from '@/lib/sleep';
import { isSourceActive } from '@/lib/source-activation';
import { foldSearch } from '@/lib/text';
import { formatCompactNumber } from '@/lib/utils';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useSources } from '@/providers/sources-provider';
import type { ChannelInfo, ChannelSync, VideoResult } from '@/types';

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
  const subscription = useChannelSubscription(channel ? { id: channelId, platform: channel.platform, name: channel.name, thumbnail: channel.thumbnail } : null);
  const live = useChannelLive(channelId);
  const target = { id: channelId, platform, source };

  // The channel is read again every couple of seconds while the server is still crawling it.
  useAbortableEffect(
    async (signal) => {
      setLoading(true);
      setError(null);
      let retrySync = retry > 0;
      let crawling = true;
      while (crawling && !signal.aborted) {
        try {
          const data = await api.channels.get(channelId, { source, signal, sync: platform === 'youtube', retry: retrySync });
          if (signal.aborted) {
            return;
          }
          setChannel((current) => data.channel ?? current);
          setVideos((current) => {
            const catalog = new Map(current.map((video) => [`${video.platform}:${video.id}`, video]));
            for (const video of data.videos) {
              catalog.set(`${video.platform}:${video.id}`, video);
            }
            return Array.from(catalog.values());
          });
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
        if (crawling) {
          await sleep(2000, signal);
        }
      }
    },
    [channelId, source, platform, retry],
  );

  const syncing = sync?.status === 'pending' || sync?.status === 'running';
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
  // The whole catalog is already loaded (and keeps growing while it syncs), so searching it needs no request.
  const visibleVideos = useMemo(() => {
    const needle = foldSearch(search.trim());
    return needle ? channelVideos.filter((video) => foldSearch(`${video.title} ${video.originalTitle}`).includes(needle)) : channelVideos;
  }, [channelVideos, search]);
  // An unresolved YouTube channel is named after its own ID. A Twitch channel's name is routinely its login, so equality says nothing there.
  const named = !!channel?.name && !(channel.platform === 'youtube' && channel.name === nativeId);
  const waitingForChannel = !error && (loading || syncing) && !named;

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
              <PageTitle>
                <Skeleton aria-hidden="true" className="h-7 w-48 max-w-full" />
              </PageTitle>
              <ChannelHeaderSkeleton />
            </section>
          ) : (
            <div className="flex flex-wrap items-center gap-5 rounded-2xl border bg-card p-6">
              <ChannelAvatar src={channel?.thumbnail} />
              <div className="min-w-0 flex-1">
                <PageTitle>{named && channel ? channel.name : t('player.unknownChannel')}</PageTitle>
                {live && (
                  <p className="mt-1 inline-flex items-center gap-1.5 rounded-md bg-red-600 px-2 py-0.5 text-[11px] font-bold uppercase tracking-wide text-white">
                    <span aria-hidden="true" className="size-1.5 animate-pulse rounded-full bg-white" />
                    {t('channel.live')}
                  </p>
                )}
                {channel && channel.subscriberCount > 0 && (
                  <p className="text-sm text-muted-foreground" title={t('channel.subscribers', { count: channel.subscriberCount.toLocaleString(language) })}>
                    {t('channel.subscribers', { count: formatCompactNumber(channel.subscriberCount, language) })}
                  </p>
                )}
                {channel?.description && <p className="text-sm text-muted-foreground mt-1 line-clamp-2">{channel.description}</p>}
              </div>
              {live?.recordId && (
                <Button asChild className="rounded-full bg-red-600 text-white hover:bg-red-600/90">
                  <VideoLink video={{ id: live.recordId, channelId, live: true, source }} title={live.title}>
                    <span aria-hidden="true" className="size-2 animate-pulse rounded-full bg-white" />
                    {t('channel.watchLive')}
                  </VideoLink>
                </Button>
              )}
              <Button
                variant={subscription.subscribed ? 'secondary' : 'default'}
                className="rounded-full"
                disabled={subscription.disabled}
                aria-pressed={subscription.subscribed}
                onClick={() => void subscription.toggle()}
              >
                {subscription.subscribed ? <UserCheck aria-hidden="true" className="size-4" /> : <UserPlus aria-hidden="true" className="size-4" />}
                {t(subscription.subscribed ? 'player.subscribed' : 'player.subscribe')}
              </Button>
            </div>
          )}
          <div className="space-y-4">
            <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b">
              <nav aria-label={t('channel.tabs')} className="flex gap-2">
                <ChannelLink
                  channel={target}
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
                    tab="series"
                    aria-current={series ? 'page' : undefined}
                    className={`flex items-center gap-2 border-b-2 px-3 py-2 text-sm font-medium ${series ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
                  >
                    {t('nav.series')}
                    <SeriesCount channelId={channelId} videos={videos} />
                  </ChannelLink>
                )}
              </nav>
              {(series || videos.length > 0) && (
                <Input
                  type="search"
                  aria-label={t('channel.search')}
                  placeholder={t('channel.search')}
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  className="mb-1 w-full sm:w-72"
                />
              )}
            </div>
            {series ? (
              <ChannelSeries channelId={channelId} videos={videos} sync={sync} query={search} active />
            ) : videos.length > 0 ? (
              visibleVideos.length > 0 ? (
                <VideoGrid videos={visibleVideos} historyActions />
              ) : (
                <p className="py-8 text-center text-sm text-muted-foreground">{t('subscriptions.noSearchResults')}</p>
              )
            ) : (syncing || loading) && !error ? (
              <VideoGridSkeleton />
            ) : !error && sync?.status !== 'error' ? (
              <p className="text-muted-foreground">{t('channel.empty')}</p>
            ) : null}
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

/** The channel's header card as it will look: avatar, a few lines of text and the subscribe button. The title lives in the topbar. */
function ChannelHeaderSkeleton() {
  return (
    <div aria-hidden="true" className="flex flex-wrap items-center gap-5 rounded-2xl border bg-card p-6">
      <Skeleton className="size-20 rounded-full" />
      <div className="min-w-0 flex-1 space-y-2">
        <Skeleton className="h-4 w-2/5" />
        <Skeleton className="h-4 w-full max-w-md" />
      </div>
      <Skeleton className="h-10 w-32 rounded-full" />
    </div>
  );
}

/** The whole page before any data: topbar title, header card, tabs with their search box, then the videos. */
export function ChannelSkeleton() {
  return (
    <>
      <PageTitle>
        <Skeleton aria-hidden="true" className="h-7 w-48 max-w-full" />
      </PageTitle>
      <ChannelHeaderSkeleton />
      <div aria-hidden="true" className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b">
          <div className="flex gap-2">
            <div className="px-3 py-2">
              <Skeleton className="h-5 w-20" />
            </div>
            <div className="px-3 py-2">
              <Skeleton className="h-5 w-16" />
            </div>
          </div>
          <Skeleton className="mb-1 h-10 w-full rounded-lg sm:w-72" />
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
function SeriesCount({ channelId, videos }: { channelId: string; videos: VideoResult[] }) {
  const { t, language } = useI18n();
  const { isError } = useProfiles();
  const series = useChannelSeries(channelId, videos);
  if (isError || series.length === 0) {
    return null;
  }
  return (
    <span
      title={t('series.count', { count: series.length.toLocaleString(language) })}
      className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium tabular-nums text-muted-foreground"
    >
      <span aria-hidden="true">{series.length.toLocaleString(language)}</span>
      <span className="sr-only">{t('series.count', { count: series.length.toLocaleString(language) })}</span>
    </span>
  );
}

function ChannelSeries({
  channelId,
  videos,
  sync,
  query,
  active,
}: {
  channelId: string;
  videos: VideoResult[];
  sync: ChannelSync | null;
  query: string;
  active: boolean;
}) {
  const { t } = useI18n();
  const { isError, retry } = useProfiles();
  if (isError) {
    return (
      <Button variant="outline" onClick={retry}>
        {t('profiles.loadError')} {t('common.retry')}
      </Button>
    );
  }
  if (!active) {
    return null;
  }
  return <ChannelSeriesList channelId={channelId} videos={videos} sync={sync} query={query} />;
}

function ChannelSeriesList({ channelId, videos, sync, query }: { channelId: string; videos: VideoResult[]; sync: ChannelSync | null; query: string }) {
  const { t, language } = useI18n();
  // Series arrive live from the collection as the background analysis stores them; the channel's own
  // crawl status drives the synchronizing and empty states.
  const channelSeries = useChannelSeries(channelId, videos);
  const synchronizing = sync?.status === 'pending' || sync?.status === 'running';
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
      ) : sync?.status === 'complete' ? (
        <p className="text-muted-foreground">{t('series.noChannelSeries')}</p>
      ) : null}
    </div>
  );
}
