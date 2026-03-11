import { eq, ilike, inArray, like, or, useLiveInfiniteQuery } from '@tanstack/react-db';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { videoCollection } from '@/collections/video.collection';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { VideoGrid } from '@/features/video/components/video-grid';
import { TRANSLATION_LANGUAGE } from '@/features/video/utils/titles';
import { videoRecordToResult } from '@/features/video/utils/video-utils';
import { foldSearch } from '@/lib/text';
import { mergeTimeline } from '@/lib/timeline-merge';
import { useI18n } from '@/providers/i18n-provider';
import type { ChannelRecord, VideoRecord, VideoResult } from '@/types';

const TIMELINE_PAGE_SIZE = 36;
/** A feed filters on at most this many channel ids, so the filter PocketBase receives stays short whatever the number of subscriptions. */
const CHANNELS_PER_FEED = 40;

interface FeedState {
  rows: VideoRecord[];
  ready: boolean;
  error: boolean;
  hasNextPage: boolean;
  fetching: boolean;
  fetchNextPage: () => Promise<void>;
}

/** The pages of one slice of the subscribed channels. It renders nothing: it reports its state to the timeline, which merges the slices. */
function ChannelFeed({
  index,
  channelIds,
  term,
  matchingChannelIds,
  hideSubscriberOnly,
  report,
}: {
  index: number;
  channelIds: string[];
  term: string;
  matchingChannelIds: string[];
  hideSubscriberOnly: boolean;
  report: (index: number, state: FeedState) => void;
}) {
  // PocketBase folds case for ASCII only: a term with other characters is matched case-sensitively there and here.
  const ascii = /^[\p{ASCII}]*$/u.test(term);
  const pattern = `%${term.replace(/[%_\\]/g, '')}%`;
  const feed = useLiveInfiniteQuery(
    (q) => {
      const channelVideos = q.from({ videos: videoCollection }).where(({ videos }) => inArray(videos.channel, channelIds));
      const searched = term
        ? channelVideos.where(({ videos }) => {
            const byTitle = ascii
              ? or(ilike(videos.titles?.original, pattern), ilike(videos.titles?.[TRANSLATION_LANGUAGE], pattern))
              : or(like(videos.titles?.original, pattern), like(videos.titles?.[TRANSLATION_LANGUAGE], pattern));
            return matchingChannelIds.length > 0 ? or(byTitle, inArray(videos.channel, matchingChannelIds)) : byTitle;
          })
        : channelVideos;
      const visible = hideSubscriberOnly ? searched.where(({ videos }) => eq(videos.subscriber_only, false)) : searched;
      return visible.orderBy(({ videos }) => videos.published, 'desc').orderBy(({ videos }) => videos.id);
    },
    { pageSize: TIMELINE_PAGE_SIZE },
  );
  const { data, isReady, isError, hasNextPage, isFetchingNextPage, fetchNextPage } = feed;
  const state = useMemo<FeedState>(
    () => ({ rows: data, ready: isReady, error: isError, hasNextPage, fetching: isFetchingNextPage, fetchNextPage }),
    [data, isReady, isError, hasNextPage, isFetchingNextPage, fetchNextPage],
  );
  useEffect(() => report(index, state), [report, index, state]);
  return null;
}

/**
 * Newest-first timeline of the subscribed channels, read page by page from the `videos` collection (no filter is ever
 * built over more than `CHANNELS_PER_FEED` channels: the channels are cut into slices, each slice pages on its own,
 * and the timeline merges them by publication, showing only what no unloaded page of any slice could precede). The
 * next pages load when the end of the grid scrolls into view. Streams in progress (`liveVideos`) come first.
 */
export function SubscriptionVideoPages({
  channelIds,
  channels,
  query,
  hideSubscriberOnly,
  liveVideos,
  visible,
  refreshing,
  refreshFailed,
  onRetryRefresh,
  onCountChange,
}: {
  channelIds: string[];
  /** The subscribed channel records by id: the cards' channel names, and the channels a search by name selects. */
  channels: ReadonlyMap<string, ChannelRecord>;
  query: string;
  hideSubscriberOnly: boolean;
  liveVideos: VideoResult[];
  visible: boolean;
  refreshing: boolean;
  refreshFailed: boolean;
  onRetryRefresh: () => void;
  onCountChange: (count: number | null) => void;
}) {
  const { t } = useI18n();
  const [scope] = useState(channelIds);
  const [feeds, setFeeds] = useState<Record<number, FeedState>>({});
  const [retry, setRetry] = useState(0);
  const slices = useMemo(() => {
    const result: string[][] = [];
    for (let offset = 0; offset < scope.length; offset += CHANNELS_PER_FEED) {
      result.push(scope.slice(offset, offset + CHANNELS_PER_FEED));
    }
    return result;
  }, [scope]);
  const matching = useMemo(() => {
    const needle = foldSearch(query);
    return needle ? scope.filter((id) => foldSearch(channels.get(id)?.name ?? '').includes(needle)) : [];
  }, [scope, channels, query]);
  const report = useCallback(
    (index: number, state: FeedState) => setFeeds((previous) => (previous[index] === state ? previous : { ...previous, [index]: state })),
    [],
  );

  const { videos, frontier, complete, ready, failed, fetching } = useMemo(() => {
    const states = slices.map((_, index) => feeds[index]);
    const merged = mergeTimeline(states.map((state) => state && { rows: state.rows, hasNextPage: state.hasNextPage }));
    return {
      videos: merged.rows.map((row) => videoRecordToResult({ ...row, expand: { channel: channels.get(row.channel) } })),
      frontier: states[merged.pending],
      complete: merged.pending < 0,
      ready: states.every((state) => state?.ready),
      failed: states.some((state) => state?.error),
      fetching: states.some((state) => state?.fetching),
    };
  }, [feeds, slices, channels]);
  const count = scope.length === 0 ? 0 : failed ? null : ready && complete ? videos.length : null;

  useEffect(() => onCountChange(count), [count, onCountChange]);

  const feedsOfSlices = slices.map((slice, index) => (
    <ChannelFeed
      key={`${slice.join(',')}:${retry}`}
      index={index}
      channelIds={slice}
      term={query}
      matchingChannelIds={matching}
      hideSubscriberOnly={hideSubscriberOnly}
      report={report}
    />
  ));
  // The feeds keep loading while another tab is shown, so coming back finds the timeline ready.
  if (!visible) {
    return <>{feedsOfSlices}</>;
  }
  const loading = !ready || fetching;
  const settled = ready && !fetching && !refreshing && !failed;
  // A stream already stored as a video is shown once, as its live card.
  const liveIds = new Set(liveVideos.map((video) => `${video.platform}:${video.id}`));
  const shown = [...liveVideos, ...videos.filter((video) => !liveIds.has(`${video.platform}:${video.id}`))];
  return (
    <div className="space-y-4">
      {feedsOfSlices}
      {shown.length > 0 && (
        <VideoGrid
          videos={shown}
          sortByPublication={false}
          historyActions
          filterPlatforms={false}
          onEndReached={frontier && !fetching && !failed ? () => void frontier.fetchNextPage() : undefined}
        />
      )}
      {(loading || (refreshing && shown.length === 0)) && (
        <div aria-live="polite" aria-busy="true">
          <span className="sr-only">{t(shown.length === 0 ? 'subscriptions.loading' : 'search.moreLoading')}</span>
          <VideoGridSkeleton />
        </div>
      )}
      {failed && (
        <ErrorSnackbar
          message={t('errors.requestFailed')}
          action={t('common.retry')}
          onAction={() => {
            setFeeds({});
            setRetry((value) => value + 1);
          }}
        />
      )}
      {settled && shown.length === 0 && refreshFailed && (
        <ErrorSnackbar message={t('errors.sourceRequestFailed')} action={t('common.retry')} onAction={onRetryRefresh} />
      )}
      {settled && shown.length === 0 && complete && (
        <p className="py-8 text-center text-sm text-muted-foreground">{t(query ? 'subscriptions.noSearchResults' : 'subscriptions.filter.empty')}</p>
      )}
    </div>
  );
}
