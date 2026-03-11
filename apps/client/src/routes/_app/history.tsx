import { eq, useLiveInfiniteQuery } from '@tanstack/react-db';
import { createFileRoute, Link, redirect, useLocation } from '@tanstack/react-router';
import { Clock, Trash2 } from 'lucide-react';
import { useMemo, useState } from 'react';
import { watchHistoryCollection } from '@/collections/watch-history.collection';
import { ConfirmDialog } from '@/components/confirm-dialog';
import { EmptyState } from '@/components/empty-state';
import { PageTitle } from '@/components/page-title';
import { ProfilesError } from '@/components/profiles-error';
import { type SavedVideoEntry, SavedVideoGrid } from '@/components/saved-video-grid';
import { SourceFeedView, SourceModes } from '@/components/source-feed';
import { Button } from '@/components/ui/button';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { clearProfilePlaybackPositions } from '@/features/video/utils/playback-position';
import { api } from '@/lib/api';
import { removeVideoHistory } from '@/lib/watch-history';
import { useAuth } from '@/providers/auth-provider';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useActiveSource, useSources } from '@/providers/sources-provider';
import { useUndo } from '@/providers/undo-provider';

export const Route = createFileRoute('/_app/history')({
  validateSearch: (): Record<string, never> => ({}),
  beforeLoad: ({ location }) => {
    const params = new URLSearchParams(location.searchStr);
    if (params.has('source')) {
      throw redirect({ to: '/history', state: { viyuSource: params.get('source') || undefined }, replace: true });
    }
  },
  component: HistoryPage,
});

const HISTORY_PAGE_SIZE = 36;

function HistoryPage() {
  const { t } = useI18n();
  const source = useActiveSource(useLocation({ select: (location) => location.state.viyuSource }));
  const navigate = Route.useNavigate();
  return (
    <div className="space-y-6">
      <PageTitle description={t('ui.library.historyDescription')}>{t('nav.history')}</PageTitle>
      <SourceModes
        source={source}
        capability="history"
        localLabel={t('history.localTab')}
        onChange={(selected) => void navigate({ state: { viyuSource: selected } })}
      />
      {source ? <SourceFeedView platform={source} kind="history" /> : <LocalHistory />}
    </div>
  );
}

function LocalHistory() {
  const { activeProfile, isError, retry } = useProfiles();
  if (isError) {
    return <ProfilesError onRetry={retry} />;
  }
  if (!activeProfile) {
    return <VideoGridSkeleton />;
  }
  return <HistoryPages key={activeProfile.id} profileId={activeProfile.id} />;
}

/** The profile's history, newest first, one page at a time: each page is loaded from PocketBase when the end of the list scrolls into view. */
function HistoryPages({ profileId }: { profileId: string }) {
  const { t } = useI18n();
  const { user } = useAuth();
  const { isEnabled } = useSources();
  const offerUndo = useUndo();
  const [confirmClear, setConfirmClear] = useState(false);
  const { data, isReady, isError, hasNextPage, isFetchingNextPage, fetchNextPage } = useLiveInfiniteQuery(
    (q) =>
      q
        .from({ history: watchHistoryCollection })
        .where(({ history }) => eq(history.profile, profileId))
        .orderBy(({ history }) => history.watched_at, 'desc'),
    { pageSize: HISTORY_PAGE_SIZE },
  );
  const entries = useMemo<SavedVideoEntry[]>(
    () =>
      data
        // A video of a platform that was turned off stays stored but is not shown; a row whose video could not be expanded is kept.
        .filter((row) => !row.expand?.video || isEnabled(row.expand.video.platform))
        .map((row) => ({
          id: row.id,
          video: row.video,
          record: row.expand?.video,
          watched_at: row.watched_at,
          position: row.position,
          video_duration: row.video_duration,
          series: row.series || undefined,
        })),
    [data, isEnabled],
  );
  const removeHistory = async (entry: SavedVideoEntry) => {
    if (!user) {
      throw new Error('An active account is required');
    }
    const undo = await removeVideoHistory(user.id, profileId, entry.video);
    offerUndo(t('video.removedFromHistory'), undo);
  };
  const clearHistory = async () => {
    if (!user) {
      throw new Error('An active account is required');
    }
    await api.history.clear(profileId);
    // Server entries are gone; the per-video local resume positions of this profile go with them.
    clearProfilePlaybackPositions(user.id, profileId);
  };
  if (isError) {
    return (
      <p role="alert" className="rounded-xl border border-destructive/25 bg-card p-5 text-destructive">
        {t('ui.library.loadError')}
      </p>
    );
  }
  if (!isReady) {
    return <VideoGridSkeleton />;
  }
  return (
    <>
      {data.length > 0 && (
        <div className="-mt-2 mb-4 flex justify-end">
          <Button variant="outline" size="sm" onClick={() => setConfirmClear(true)}>
            <Trash2 aria-hidden="true" className="size-4" />
            {t('history.clearAll')}
          </Button>
        </div>
      )}
      {data.length === 0 && (
        <EmptyState icon={Clock} title={t('history.emptyTitle')} description={t('history.emptyDescription')}>
          <Button asChild variant="outline">
            <Link to="/search">{t('search.videos')}</Link>
          </Button>
        </EmptyState>
      )}
      <SavedVideoGrid
        entries={entries}
        onRemove={removeHistory}
        showProgress
        onEndReached={hasNextPage && !isFetchingNextPage ? () => void fetchNextPage() : undefined}
      />
      {isFetchingNextPage && <VideoGridSkeleton />}
      {confirmClear && (
        <ConfirmDialog
          title={t('history.clearAll')}
          description={t('history.clearAllConfirm')}
          confirmLabel={t('common.delete')}
          pendingLabel={t('history.removing')}
          cancelLabel={t('common.cancel')}
          errorMessage={t('history.clearAllError')}
          onConfirm={clearHistory}
          onClose={() => setConfirmClear(false)}
        />
      )}
    </>
  );
}
