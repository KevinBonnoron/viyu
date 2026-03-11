import { eq, useLiveInfiniteQuery } from '@tanstack/react-db';
import { createFileRoute, Link } from '@tanstack/react-router';
import { Heart } from 'lucide-react';
import { useMemo } from 'react';
import { videoReactionCollection } from '@/collections/video-reaction.collection';
import { EmptyState } from '@/components/empty-state';
import { PageTitle } from '@/components/page-title';
import { ProfilesError } from '@/components/profiles-error';
import { type SavedVideoEntry, SavedVideoGrid } from '@/components/saved-video-grid';
import { Button } from '@/components/ui/button';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useSources } from '@/providers/sources-provider';

export const Route = createFileRoute('/_app/likes')({
  validateSearch: (): Record<string, never> => ({}),
  component: LikesPage,
});

const LIKES_PAGE_SIZE = 36;

function LikesPage() {
  const { t } = useI18n();
  const { activeProfile, isError, retry } = useProfiles();
  return (
    <div className="space-y-6">
      <PageTitle description={t('likes.emptyDescription')}>{t('likes.title')}</PageTitle>
      {isError ? (
        <ProfilesError onRetry={retry} />
      ) : activeProfile ? (
        <LikedVideos key={activeProfile.id} profileId={activeProfile.id} />
      ) : (
        <VideoGridSkeleton />
      )}
    </div>
  );
}

/** The videos the profile liked, newest like first, one page at a time. */
function LikedVideos({ profileId }: { profileId: string }) {
  const { t } = useI18n();
  const { isEnabled } = useSources();
  const { data, isReady, isError, hasNextPage, isFetchingNextPage, fetchNextPage } = useLiveInfiniteQuery(
    (q) =>
      q
        .from({ reactions: videoReactionCollection })
        .where(({ reactions }) => eq(reactions.profile, profileId))
        .where(({ reactions }) => eq(reactions.reaction, 'like'))
        .orderBy(({ reactions }) => reactions.created, 'desc'),
    { pageSize: LIKES_PAGE_SIZE },
  );
  const entries = useMemo<SavedVideoEntry[]>(
    () =>
      data
        .filter((reaction) => !reaction.expand?.video || isEnabled(reaction.expand.video.platform))
        .map((reaction) => ({ id: reaction.id, video: reaction.video, record: reaction.expand?.video, watched_at: reaction.created })),
    [data, isEnabled],
  );
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
  return data.length > 0 ? (
    <>
      <SavedVideoGrid entries={entries} onEndReached={hasNextPage && !isFetchingNextPage ? () => void fetchNextPage() : undefined} />
      {isFetchingNextPage && <VideoGridSkeleton />}
    </>
  ) : (
    <EmptyState icon={Heart} title={t('likes.emptyTitle')} description={t('ui.library.likesEmptyDescription')}>
      <Button asChild variant="outline">
        <Link to="/search">{t('search.videos')}</Link>
      </Button>
    </EmptyState>
  );
}
