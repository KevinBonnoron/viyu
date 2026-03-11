import { createFileRoute } from '@tanstack/react-router';
import { Check, Play } from 'lucide-react';
import { useMemo, useState } from 'react';
import { CollapsibleCard } from '@/components/collapsible-card';
import { PageTitle, PageTitleSkeleton } from '@/components/page-title';
import { ProfilesError } from '@/components/profiles-error';
import { RecordNotFound } from '@/components/record-not-found';
import { SeriesItemLabel, SeriesSyncStatus } from '@/components/series';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { useSeriesDetails } from '@/hooks/useSeriesRecords';
import { useVideoStatuses } from '@/hooks/useVideoStatuses';
import { ChannelLink, VideoLink } from '@/lib/links';
import { groupBySeason } from '@/lib/series-seasons';
import { cn } from '@/lib/utils';
import type { VideoStatus } from '@/lib/video-status';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import type { SeriesItem } from '@/types/series.type';

const ROW_SKELETONS = ['first', 'second', 'third', 'fourth'];

export const Route = createFileRoute('/_app/series/$seriesId')({ component: SeriesPage });

/** `seriesId` is a `series` record id. */
function SeriesPage() {
  const { seriesId } = Route.useParams();
  const { activeProfile, isError, retry } = useProfiles();
  if (isError) {
    return <ProfilesError onRetry={retry} />;
  }
  if (!activeProfile) {
    return <SeriesSkeleton />;
  }
  return <SeriesDetail key={`${activeProfile.id}:${seriesId}`} profileId={activeProfile.id} seriesId={seriesId} />;
}

/** The rows the episodes will take, in the same card: shown while the series loads and, below the episodes, while more are still being found. */
function SeriesRowsSkeleton({ count }: { count: number }) {
  return (
    <div aria-hidden="true" className="overflow-hidden rounded-2xl border bg-card">
      {ROW_SKELETONS.slice(0, count).map((position) => (
        <div key={position} className="flex items-center gap-4 border-b p-4 last:border-b-0">
          <Skeleton className="h-4 w-8" />
          <Skeleton className="aspect-video w-32 rounded-lg" />
          <div className="min-w-0 flex-1 space-y-2">
            <Skeleton className="h-5 w-3/5" />
            <Skeleton className="h-4 w-1/4" />
          </div>
        </div>
      ))}
    </div>
  );
}

/** The page before the series is known. The title lives in the topbar, so its placeholder does too; the episodes follow. */
function SeriesSkeleton() {
  const { t } = useI18n();
  return (
    <main aria-busy="true" className="space-y-6">
      <PageTitleSkeleton />
      <output className="block" aria-label={t('common.loading')}>
        <SeriesRowsSkeleton count={4} />
      </output>
    </main>
  );
}

function SeriesDetail({ profileId, seriesId }: { profileId: string; seriesId: string }) {
  const { t } = useI18n();
  const { series, sync, episodes, dates, error, loading, retry } = useSeriesDetails(profileId, seriesId);
  const videoIds = useMemo(() => series?.items.map((item) => item.video.id) ?? [], [series]);
  const { statuses } = useVideoStatuses(videoIds);
  const blocks = series ? groupBySeason(series.items) : [];
  if (error === 'series.notFound' && !series) {
    return <RecordNotFound kind="series" />;
  }
  if (loading && !series) {
    return <SeriesSkeleton />;
  }
  // Placeholders only for what is really still coming: more episodes while the catalog or the analysis runs,
  // a date while that date's lookup is queued. A series whose episodes are all known shows no extra rows.
  const working = (status: string | null) => !error && (status === 'pending' || status === 'running');
  const moreEpisodes = working(episodes);
  const analysing = working(dates);
  return (
    <main className="space-y-6">
      {series && (
        <PageTitle
          description={
            <ChannelLink channel={{ id: series.channelRecordId, platform: series.platform }} tab="series" className="hover:underline">
              {series.channelName}
            </ChannelLink>
          }
        >
          {series.title}
        </PageTitle>
      )}
      <SeriesSyncStatus sync={sync} error={error} onRetry={retry} />
      {series && series.items.length > 0 && blocks.length === 1 && blocks[0].season === null && (
        <div className="space-y-4">
          <Button asChild variant="outline" size="sm">
            <VideoLink video={series.items[0].video} series={series.id}>
              <Play aria-hidden="true" className="size-4 fill-current" />
              {t('series.play')}
            </VideoLink>
          </Button>
          <section aria-label={series.title} className="overflow-hidden rounded-2xl border bg-card">
            {blocks[0].items.map(({ item, position }) => (
              <SeriesEpisodeRow
                key={`${item.video.platform}:${item.video.id}`}
                item={item}
                number={position}
                seriesId={series.id}
                status={statuses.get(item.video.id)}
                pendingDate={analysing}
              />
            ))}
          </section>
        </div>
      )}
      {series && series.items.length > 0 && (blocks.length > 1 || blocks[0]?.season !== null) && (
        <div className="space-y-4">
          {blocks.map((block) => (
            <CollapsibleCard
              key={block.season ?? 'other'}
              id={`series:${series.id}:${block.season === null ? 'other' : `season-${block.season}`}`}
              title={block.season === null ? t('series.otherVideos') : t('series.season', { season: block.season })}
              badge={
                <span className="rounded-full bg-muted px-2 py-0.5 text-xs font-medium tabular-nums text-muted-foreground">
                  {t('series.videoCount', { count: block.items.length })}
                </span>
              }
              actions={
                <Button asChild variant="outline" size="sm" className="w-48 justify-center whitespace-nowrap">
                  <VideoLink video={block.items[0].item.video} series={series.id}>
                    <Play aria-hidden="true" className="size-4 fill-current" />
                    {t(block.season === null ? 'series.playBlock' : 'series.playSeason')}
                  </VideoLink>
                </Button>
              }
            >
              {block.items.map(({ item, position }, index) => (
                <SeriesEpisodeRow
                  key={`${item.video.platform}:${item.video.id}`}
                  item={item}
                  number={block.season === null ? position : (item.episode ?? index + 1)}
                  inSeason={block.season !== null}
                  seriesId={series.id}
                  status={statuses.get(item.video.id)}
                  pendingDate={analysing}
                />
              ))}
            </CollapsibleCard>
          ))}
        </div>
      )}
      {moreEpisodes && <SeriesRowsSkeleton count={series && series.items.length > 0 ? 2 : 4} />}
    </main>
  );
}

function SeriesEpisodeRow({
  item,
  number,
  inSeason = false,
  seriesId,
  pendingDate = false,
  status,
}: {
  item: SeriesItem;
  number: number;
  inSeason?: boolean;
  seriesId: string;
  pendingDate?: boolean;
  status?: VideoStatus;
}) {
  const watched = status?.watched ?? false;
  const progress = status?.progress;
  return (
    <VideoLink video={item.video} series={seriesId} className="flex items-center gap-4 border-b p-4 last:border-b-0 hover:bg-muted/50">
      <span className="w-8 shrink-0 text-right text-sm text-muted-foreground">{number}</span>
      <EpisodeThumbnail src={item.video.thumbnail} watched={watched} progress={progress} />
      <span className="min-w-0 flex-1">
        <span className={`block font-medium ${watched ? 'text-muted-foreground' : ''}`}>{item.video.title}</span>
        <span className="mt-1 block text-sm text-muted-foreground">
          <SeriesItemLabel item={item} inSeason={inSeason} pendingDate={pendingDate} />
        </span>
      </span>
    </VideoLink>
  );
}

/** The thumbnail's place is always held, so the row does not change shape: a pulse while the image loads, a plain block when there is none. */
function EpisodeThumbnail({ src, watched = false, progress }: { src: string; watched?: boolean; progress?: number }) {
  const { t } = useI18n();
  const [state, setState] = useState<'loading' | 'loaded' | 'failed'>(src ? 'loading' : 'failed');
  return (
    <span aria-hidden="true" className={cn('relative aspect-video w-32 shrink-0 overflow-hidden rounded-lg bg-muted', state === 'loading' && 'animate-pulse')}>
      {src && state !== 'failed' && (
        <img src={src} alt="" loading="lazy" onLoad={() => setState('loaded')} onError={() => setState('failed')} className="size-full object-cover" />
      )}
      {watched && <span className="absolute inset-0 bg-black/45" />}
      {watched && (
        <span className="absolute left-1.5 top-1.5 inline-flex items-center gap-1 rounded-md border border-white/30 bg-black px-1.5 py-0.5 text-[10px] font-semibold text-white">
          <Check aria-hidden="true" className="size-2.5" />
          {t('video.watched')}
        </span>
      )}
      {progress !== undefined && (
        <span className="absolute inset-x-0 bottom-0 h-1 bg-black/50">
          <span className="block h-full bg-primary" style={{ width: `${progress * 100}%` }} />
        </span>
      )}
    </span>
  );
}
