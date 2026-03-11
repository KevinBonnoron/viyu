import { Link } from '@tanstack/react-router';
import { ListVideo } from 'lucide-react';
import { useEffect, useMemo } from 'react';
import { EmptyState } from '@/components/empty-state';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { Button } from '@/components/ui/button';
import { Skeleton } from '@/components/ui/skeleton';
import { VirtualGrid } from '@/components/virtual-grid';
import { useSeriesSummaries } from '@/hooks/useSeriesRecords';
import { ChannelLink } from '@/lib/links';
import { filterEnabledPlatforms } from '@/lib/source-activation';
import { foldSearch } from '@/lib/text';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useSources } from '@/providers/sources-provider';
import type { ChannelResult } from '@/types';

export function SeriesPage({
  selectedChannels,
  onCountChange,
  active,
  query,
}: {
  selectedChannels?: ChannelResult[];
  onCountChange: (count: number | null) => void;
  active: boolean;
  query: string;
}) {
  const { activeProfile, isError, retry } = useProfiles();
  const { t } = useI18n();
  if (isError) {
    return (
      <Button variant="outline" onClick={retry}>
        {t('profiles.loadError')} {t('common.retry')}
      </Button>
    );
  }
  if (!active) {
    return activeProfile ? (
      <ProfileSeries
        key={activeProfile.id}
        profileId={activeProfile.id}
        selectedChannels={selectedChannels}
        onCountChange={onCountChange}
        active={false}
        query={query}
      />
    ) : null;
  }
  if (!activeProfile) {
    return <SeriesGridSkeleton />;
  }
  return (
    <ProfileSeries key={activeProfile.id} profileId={activeProfile.id} selectedChannels={selectedChannels} onCountChange={onCountChange} active query={query} />
  );
}

function ProfileSeries({
  profileId,
  selectedChannels,
  onCountChange,
  active,
  query,
}: {
  profileId: string;
  selectedChannels?: ChannelResult[];
  onCountChange: (count: number | null) => void;
  active: boolean;
  query: string;
}) {
  const { t, language } = useI18n();
  const { isEnabled } = useSources();
  const { result, error, retry } = useSeriesSummaries(profileId);
  const synchronizing = result?.sync.status === 'pending' || result?.sync.status === 'running';
  const enabledSeries = useMemo(
    () => (result ? filterEnabledPlatforms(result.series, (series) => series.platform, isEnabled) : undefined),
    [result, isEnabled],
  );
  const visibleSeries =
    enabledSeries?.filter(
      (series) =>
        (!selectedChannels || selectedChannels.some((channel) => series.platform === channel.platform && series.channelRecordId === channel.id)) &&
        (!query || foldSearch(series.title).includes(query) || foldSearch(series.channelName).includes(query)),
    ) ?? [];
  useEffect(() => {
    onCountChange(result && !error ? visibleSeries.length : null);
  }, [result, error, visibleSeries.length, onCountChange]);
  if (!active) {
    return null;
  }
  return (
    <div className="space-y-6">
      {error && <ErrorSnackbar message={t(error)} action={t('common.retry')} onAction={retry} />}
      {!result && !error && <SeriesGridSkeleton />}
      {visibleSeries.length > 0 && (
        <VirtualGrid
          items={visibleSeries}
          getKey={(series) => series.id}
          className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3"
          renderItem={(series) => {
            const published = Date.parse(series.latestPublishedAt);
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
                  <ChannelLink channel={{ id: series.channelRecordId, platform: series.platform }} className="hover:underline">
                    {series.channelName}
                  </ChannelLink>
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
      )}
      {result && visibleSeries.length === 0 && query && <output className="block text-sm text-muted-foreground">{t('series.noSearchResults')}</output>}
      {selectedChannels && result && visibleSeries.length === 0 && !query && <p className="text-sm text-muted-foreground">{t('series.noChannelSeries')}</p>}
      {enabledSeries?.length === 0 && !query && !selectedChannels && !synchronizing && !error && result?.sync.status === 'complete' && (
        <EmptyState icon={ListVideo} title={t('series.emptyTitle')} description={t('series.emptyDescription')}>
          <Button asChild variant="outline">
            <Link to="/subscriptions" search={{ tab: 'videos' }}>
              {t('nav.subscriptions')}
            </Link>
          </Button>
          <Button asChild variant="outline">
            <Link to="/search">{t('nav.search')}</Link>
          </Button>
        </EmptyState>
      )}
    </div>
  );
}

function SeriesGridSkeleton() {
  const { t } = useI18n();
  return (
    <div aria-busy="true" className="grid gap-5 sm:grid-cols-2 xl:grid-cols-3">
      <span className="sr-only">{t('common.loading')}</span>
      {['first', 'second', 'third', 'fourth', 'fifth', 'sixth'].map((position) => (
        <div key={position} aria-hidden="true" className="overflow-hidden rounded-2xl border bg-card">
          <Skeleton className="aspect-video w-full rounded-none" />
          <div className="space-y-2 p-4">
            <Skeleton className="h-6 w-4/5" />
            <Skeleton className="h-5 w-1/2" />
            <Skeleton className="h-5 w-1/3" />
            <Skeleton className="h-5 w-2/3" />
          </div>
        </div>
      ))}
    </div>
  );
}
