import { createFileRoute, Link, redirect } from '@tanstack/react-router';
import { Users } from 'lucide-react';
import { useEffect, useMemo, useState } from 'react';
import { ChannelFilters } from '@/components/channel-filters';
import { EmptyState } from '@/components/empty-state';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { GroupWithDialog } from '@/components/group-with-dialog';
import { PageTitle } from '@/components/page-title';
import { PlatformBadge } from '@/components/platform-badge';
import { PlatformFilters } from '@/components/platform-filters';
import { ProfilesError } from '@/components/profiles-error';
import { SeriesPage } from '@/components/series-page';
import { SubscriptionVideoPages } from '@/components/subscription-timeline';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Skeleton } from '@/components/ui/skeleton';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';
import { useGroupSubscriptions } from '@/hooks/useSubscriptions';
import { useSubscriptionsLive } from '@/hooks/useSubscriptionsLive';
import { useHideSubscriberOnly, useUserSettings } from '@/hooks/useUserSettings';
import { api } from '@/lib/api';
import { buildChannelFilters, filterChannels, filterIdentity, isChannelGroup, matchesChannels, ungroupedDuplicates } from '@/lib/channel-groups';
import { platformName } from '@/lib/platforms';
import { SEARCH_DEBOUNCE_MS } from '@/lib/search-query';
import { foldSearch } from '@/lib/text';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { type SubscriptionItem, useSubscriptions } from '@/providers/subscriptions-provider';
import type { ChannelRecord, ChannelResult, SubscriptionLive, VideoResult } from '@/types';

export const Route = createFileRoute('/_app/subscriptions')({
  validateSearch: (search: Record<string, unknown>): { tab?: 'videos' | 'series' } => ({
    tab: search.tab === 'series' ? 'series' : undefined,
  }),
  beforeLoad: ({ location, search }) => {
    const params = new URLSearchParams(location.searchStr);
    if (params.has('source')) {
      throw redirect({ to: '/subscriptions', search: { tab: search.tab }, state: { viyuSource: params.get('source') || undefined }, replace: true });
    }
  },
  component: SubscriptionsPage,
});

function SubscriptionsPage() {
  const { activeProfile } = useProfiles();
  const { tab } = Route.useSearch();
  return <ProfileSubscriptions key={activeProfile?.id ?? ''} series={tab === 'series'} />;
}

function ProfileSubscriptions({ series }: { series: boolean }) {
  const { t, language } = useI18n();
  const { data: subscriptions, byChannel, isReady } = useSubscriptions();
  const { activeProfile, isError, retry } = useProfiles();
  const [selected, setSelected] = useState<string>();
  const [groupWithOpen, setGroupWithOpen] = useState(false);
  const [search, setSearch] = useState('');
  const query = foldSearch(search.trim());
  const [videoCount, setVideoCount] = useState<number | null>(null);
  const [seriesCount, setSeriesCount] = useState<number | null>(null);
  const filters = useMemo(
    () =>
      buildChannelFilters(subscriptions).sort(
        (a, b) => a.name.localeCompare(b.name, language, { sensitivity: 'base', numeric: true }) || filterIdentity(a).localeCompare(filterIdentity(b)),
      ),
    [subscriptions, language],
  );
  const selectedFilter = filters.find((filter) => filterIdentity(filter) === selected);
  const selectedChannels = useMemo(() => (selectedFilter ? filterChannels(selectedFilter) : undefined), [selectedFilter]);
  const duplicates = useMemo(
    () => ungroupedDuplicates(subscriptions).sort((a, b) => a.name.localeCompare(b.name, language, { sensitivity: 'base', numeric: true })),
    [subscriptions, language],
  );
  const grouping = useGroupSubscriptions();
  // A single channel selected on the bar: the subscription a manual grouping starts from.
  const singleSubscription = selectedFilter && !isChannelGroup(selectedFilter) ? byChannel.get(selectedFilter.id) : undefined;
  const groupWith = async (target: SubscriptionItem) => {
    if (!singleSubscription) {
      return;
    }
    const members = await grouping.group(`with:${target.id}`, [singleSubscription.id, target.id]);
    setGroupWithOpen(false);
    const creator = members?.find((member) => member.creator)?.creator;
    if (creator) {
      setSelected(`group:${creator}`);
    }
  };
  const subscriptionKey = useMemo(
    () =>
      subscriptions
        .map((subscription) => subscription.id)
        .sort()
        .join(','),
    [subscriptions],
  );
  const live = useSubscriptionsLive(isReady && subscriptionKey ? activeProfile?.id : undefined, subscriptionKey);
  // A bubble is live when any channel it stands for is.
  const liveFilters = useMemo(() => {
    const channels = new Set(live.map((item) => `${item.platform}:${item.channelRecordId}`));
    return new Set(filters.filter((filter) => filterChannels(filter).some((channel) => channels.has(`${channel.platform}:${channel.id}`))).map(filterIdentity));
  }, [live, filters]);
  if (isError) {
    return <ProfilesError onRetry={retry} />;
  }
  return (
    <div className="space-y-6">
      <PageTitle description={t('ui.library.subscriptionsDescription')}>{t('nav.subscriptions')}</PageTitle>
      <div className="space-y-2">
        <div className="min-w-0">
          {!isReady ? (
            <div aria-hidden="true" className="flex max-w-full gap-2 overflow-hidden py-1">
              {['first', 'second', 'third', 'fourth', 'fifth', 'sixth', 'seventh', 'eighth'].map((position) => (
                <div key={position} className="flex w-16 shrink-0 flex-col items-center gap-1.5 py-1">
                  <Skeleton className="size-12 rounded-full" />
                  <Skeleton className="h-4 w-12" />
                </div>
              ))}
            </div>
          ) : filters.length > 0 ? (
            <ChannelFilters
              channels={filters}
              selected={selectedFilter ? filterIdentity(selectedFilter) : undefined}
              live={liveFilters}
              onSelect={setSelected}
            />
          ) : null}
        </div>
        {isReady && duplicates.length > 0 && (
          <ul className="space-y-2">
            {duplicates.map((duplicate) => {
              const platforms = duplicate.platforms.map(platformName).join(' / ');
              return (
                <li key={duplicate.key} className="flex flex-wrap items-center gap-3 rounded-lg border bg-card px-3 py-2 text-sm">
                  <span className="flex shrink-0 -space-x-1">
                    {duplicate.platforms.map((platform) => (
                      <PlatformBadge key={platform} platform={platform} className="size-6 rounded-full" />
                    ))}
                  </span>
                  <span className="min-w-0 flex-1 text-muted-foreground">{t('subscriptions.group.duplicate', { name: duplicate.name, platforms })}</span>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    aria-label={t('subscriptions.group.label', { name: duplicate.name, platforms })}
                    disabled={!!grouping.pendingKey}
                    onClick={() =>
                      void grouping.group(
                        duplicate.key,
                        duplicate.subscriptions.map((subscription) => subscription.id),
                      )
                    }
                  >
                    {t('subscriptions.group.action')}
                  </Button>
                </li>
              );
            })}
          </ul>
        )}
        {isReady && selectedFilter && isChannelGroup(selectedFilter) && (
          <div className="flex flex-wrap items-center gap-3 rounded-lg border bg-card px-3 py-2 text-sm">
            <span className="flex shrink-0 -space-x-1">
              {selectedFilter.channels.map((channel) => (
                <PlatformBadge key={channel.platform} platform={channel.platform} className="size-6 rounded-full" />
              ))}
            </span>
            <span className="min-w-0 flex-1 text-muted-foreground">
              {t('subscriptions.group.joined', {
                name: selectedFilter.name,
                platforms: selectedFilter.channels.map((channel) => platformName(channel.platform)).join(' / '),
              })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-label={t('subscriptions.group.separateLabel', {
                name: selectedFilter.name,
                platforms: selectedFilter.channels.map((channel) => platformName(channel.platform)).join(' / '),
              })}
              disabled={!!grouping.pendingKey}
              onClick={() => void grouping.ungroup(selectedFilter.id)}
            >
              {t('subscriptions.group.separate')}
            </Button>
          </div>
        )}
        {isReady && selectedFilter && !isChannelGroup(selectedFilter) && singleSubscription && (
          <div className="flex flex-wrap items-center gap-3 rounded-lg border bg-card px-3 py-2 text-sm">
            <PlatformBadge platform={selectedFilter.platform} className="size-6 rounded-full" />
            <span className="min-w-0 flex-1 text-muted-foreground">
              {t('subscriptions.groupWith.hint', { name: selectedFilter.name, platform: platformName(selectedFilter.platform) })}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              aria-haspopup="dialog"
              aria-label={t('subscriptions.groupWith.actionLabel', { name: selectedFilter.name })}
              disabled={!!grouping.pendingKey}
              onClick={() => setGroupWithOpen(true)}
            >
              {t('subscriptions.groupWith.action')}
            </Button>
          </div>
        )}
        {groupWithOpen && selectedFilter && !isChannelGroup(selectedFilter) && singleSubscription && (
          <GroupWithDialog
            subscriptions={subscriptions}
            channel={selectedFilter}
            pending={!!grouping.pendingKey}
            onChoose={(target) => void groupWith(target)}
            onClose={() => setGroupWithOpen(false)}
          />
        )}
        {grouping.error && <ErrorSnackbar message={t('subscriptions.group.failed')} action={t('common.dismiss')} onAction={grouping.dismissError} />}
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2 border-b">
          <nav aria-label={t('nav.subscriptions')} className="flex gap-2">
            <Link
              to="/subscriptions"
              search={{ tab: 'videos' }}
              aria-current={!series ? 'page' : undefined}
              className={`border-b-2 px-3 py-2 text-sm font-medium ${!series ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
            >
              {t('subscriptions.videos')}
              {videoCount !== null && (
                <span className="ml-2 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">{videoCount.toLocaleString(language)}</span>
              )}
            </Link>
            <Link
              to="/subscriptions"
              search={{ tab: 'series' }}
              aria-current={series ? 'page' : undefined}
              className={`border-b-2 px-3 py-2 text-sm font-medium ${series ? 'border-primary text-foreground' : 'border-transparent text-muted-foreground hover:text-foreground'}`}
            >
              {t('nav.series')}
              {seriesCount !== null && (
                <span className="ml-2 rounded-full bg-muted px-2 py-0.5 text-xs text-muted-foreground">{seriesCount.toLocaleString(language)}</span>
              )}
            </Link>
          </nav>
          <Input
            type="search"
            aria-label={t('subscriptions.search')}
            placeholder={t('subscriptions.search')}
            value={search}
            onChange={(event) => setSearch(event.target.value)}
            className="mb-1 w-full sm:w-72"
          />
        </div>
      </div>
      <LocalSubscriptions selectedChannels={selectedChannels} live={live} onCountChange={setVideoCount} active={!series} query={search.trim()} />
      <SeriesPage selectedChannels={selectedChannels} onCountChange={setSeriesCount} active={series} query={query} />
    </div>
  );
}
function LocalSubscriptions({
  selectedChannels,
  live,
  onCountChange,
  active,
  query,
}: {
  selectedChannels?: ChannelResult[];
  live: SubscriptionLive[];
  onCountChange: (count: number | null) => void;
  active: boolean;
  query: string;
}) {
  const { t } = useI18n();
  const { activeProfile } = useProfiles();
  const { data: subscriptions = [], isReady } = useSubscriptions();
  const hideSubscriberOnly = useHideSubscriberOnly();
  const { isReady: settingsReady } = useUserSettings();
  const profileId = activeProfile?.id;
  const searched = useDebouncedValue(query, SEARCH_DEBOUNCE_MS);
  const [attempt, setAttempt] = useState(0);
  // New videos a refresh stores reach the timeline through the collection's realtime updates: the timeline is not rebuilt.
  const [refresh, setRefresh] = useState({ pending: true, failed: false });
  const channels = useMemo(
    () =>
      new Map(
        subscriptions.flatMap((subscription): [string, ChannelRecord][] =>
          subscription.channelInfo ? [[subscription.channel, subscription.channelInfo]] : [],
        ),
      ),
    [subscriptions],
  );
  const membership = useMemo(
    () =>
      subscriptions
        .map((subscription) => subscription.id)
        .sort()
        .join(','),
    [subscriptions],
  );
  const [platform, setPlatform] = useState<string>();
  const scoped = useMemo(() => {
    const records = new Map(
      subscriptions.flatMap((subscription) =>
        subscription.channelInfo
          ? [
              [
                `${subscription.channelInfo.platform}:${subscription.channelInfo.id}`,
                { id: subscription.channel, platform: subscription.channelInfo.platform },
              ] as const,
            ]
          : [],
      ),
    );
    return selectedChannels ? selectedChannels.flatMap((channel) => records.get(`${channel.platform}:${channel.id}`) ?? []) : [...records.values()];
  }, [subscriptions, selectedChannels]);
  // Offered from the selected channels, not from loaded videos, so it is available before every page is loaded.
  const platforms = useMemo(() => [...new Set(scoped.map((item) => item.platform))].sort(), [scoped]);
  const activePlatform = platform && platforms.includes(platform) ? platform : undefined;
  const channelIds = useMemo(
    () => [...new Set(scoped.filter((item) => !activePlatform || item.platform === activePlatform).map((item) => item.id))].sort(),
    [scoped, activePlatform],
  );
  const empty = isReady && subscriptions.length === 0;
  // Streams in progress come first, whatever the page of videos loaded below: they honour the selected channels, the platform and the search.
  const liveVideos = useMemo<VideoResult[]>(() => {
    const needle = foldSearch(searched);
    return live.flatMap((item) => {
      const channel = channels.get(item.channelRecordId);
      if (
        !channel ||
        !item.recordId ||
        (activePlatform && item.platform !== activePlatform) ||
        !matchesChannels(selectedChannels, { platform: item.platform, channelId: item.channelRecordId })
      ) {
        return [];
      }
      const title = item.title || channel.name;
      if (needle && !foldSearch(`${title} ${channel.name}`).includes(needle)) {
        return [];
      }
      return [
        {
          id: item.recordId,
          platform: item.platform,
          title,
          originalTitle: title,
          originalTitleVerified: true,
          originalLanguage: '',
          channelId: channel.id,
          channelName: channel.name,
          channelThumbnail: channel.thumbnail,
          thumbnail: item.thumbnail,
          duration: 0,
          description: '',
          tags: [],
          published: '',
          viewCount: item.viewers ?? 0,
          likeCount: null,
          dislikeCount: null,
          live: true,
          category: item.category,
        },
      ];
    });
  }, [live, channels, selectedChannels, activePlatform, searched]);

  // Expired channel listings are refreshed once per profile and subscription set, not per filter change.
  useAbortableEffect(
    (signal) => {
      if (!profileId || !isReady || membership === '') {
        return;
      }
      setRefresh((current) => ({ ...current, pending: true, failed: false }));
      return api.subscriptions
        .refresh(profileId)
        .then((result) => {
          if (!signal.aborted) {
            setRefresh({ pending: false, failed: result.failed > 0 });
          }
        })
        .catch(() => {
          if (!signal.aborted) {
            setRefresh((current) => ({ ...current, pending: false, failed: true }));
          }
        });
    },
    [profileId, isReady, membership, attempt],
  );

  useEffect(() => {
    if (!isReady) {
      onCountChange(null);
    } else if (empty) {
      onCountChange(0);
    }
  }, [isReady, empty, onCountChange]);

  if (!active && (empty || !isReady || !profileId)) {
    return null;
  }
  if (empty) {
    return (
      <EmptyState icon={Users} title={t('subscriptions.emptyTitle')} description={t('subscriptions.emptyDescription')}>
        <Button asChild variant="outline">
          <Link to="/search">{t('search.videos')}</Link>
        </Button>
      </EmptyState>
    );
  }
  if (!isReady || !settingsReady || !profileId) {
    return (
      <div aria-live="polite" aria-busy="true">
        <span className="sr-only">{t('subscriptions.loading')}</span>
        <VideoGridSkeleton />
      </div>
    );
  }
  return (
    <>
      {active && <PlatformFilters platforms={platforms} selected={activePlatform} onChange={setPlatform} />}
      <SubscriptionVideoPages
        key={`${profileId}|${channelIds.join(',')}|${searched}|${hideSubscriberOnly}`}
        hideSubscriberOnly={hideSubscriberOnly}
        channelIds={channelIds}
        channels={channels}
        query={searched}
        liveVideos={liveVideos}
        visible={active}
        refreshing={refresh.pending}
        refreshFailed={refresh.failed}
        onRetryRefresh={() => setAttempt((value) => value + 1)}
        onCountChange={onCountChange}
      />
    </>
  );
}
