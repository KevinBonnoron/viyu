import { and, eq, gt, useLiveInfiniteQuery } from '@tanstack/react-db';
import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { watchHistoryCollection } from '@/collections/watch-history.collection';
import { videoRecordToResult } from '@/features/video/utils/video-utils';
import { useUserSettings } from '@/hooks/useUserSettings';
import { useVideoReactions } from '@/hooks/useVideoStatuses';
import { pickResumable, type ResumableRow } from '@/lib/continue-watching';
import { useProfiles } from '@/providers/profiles-provider';
import { useSources } from '@/providers/sources-provider';
import type { VideoResult } from '@/types';

/** Read unfinished history incrementally until the scoped, visible selection is full or history is exhausted. */
const PAGE_SIZE = 36;
const SHOWN = 12;

export interface Resumable {
  row: ResumableRow;
  video: VideoResult;
}

/**
 * The videos the active profile started and did not finish, the last one played first, at most `SHOWN`. `channelIds`
 * (`channels` record ids) restricts them to those channels. Disabled shelves have no candidates. A video leaves the list
 * when it is finished, marked watched or dismissed.
 * `ids` are the `videos` record ids of the list, for the page to leave them out of the list it shows below.
 */
export function useContinueWatching(
  channelIds?: readonly string[],
  enabled = true,
): { items: Resumable[]; ids: ReadonlySet<string>; isReady: boolean; isError: boolean; retry: () => void } {
  const { activeProfile, isReady: profileReady, isError: profileError, retry: retryProfile } = useProfiles();
  const { isEnabled } = useSources();
  const { settings, isReady: settingsReady, isError: settingsError } = useUserSettings();
  const settingsSettled = settingsReady || settingsError;
  const hideSubscriberOnly = settingsReady && !settingsError && settings?.hide_subscriber_only === true;
  const profileId = enabled ? (activeProfile?.id ?? '') : '';
  const [attempt, setAttempt] = useState(0);
  const { data, isReady, isError, hasNextPage, isFetchingNextPage, fetchNextPage } = useLiveInfiniteQuery(
    (q) =>
      q
        .from({ history: watchHistoryCollection })
        .where(({ history }) => and(eq(history.profile, profileId), eq(history.watched, false), gt(history.position, 0)))
        .orderBy(({ history }) => history.watched_at, 'desc')
        .orderBy(({ history }) => history.id),
    { pageSize: PAGE_SIZE, queryKey: ['continue-watching', profileId, attempt] },
  );
  const candidates = useMemo<Resumable[]>(
    () =>
      pickResumable(settingsSettled && profileId ? data : [], { channelIds, isEnabled, hideSubscriberOnly }).map((row) => ({
        row,
        video: videoRecordToResult(row.expand.video),
      })),
    [settingsSettled, profileId, data, channelIds, isEnabled, hideSubscriberOnly],
  );
  const videoIds = useMemo(() => candidates.map((item) => item.video.id), [candidates]);
  const { rows: reactions, isReady: visibilityReady, isError: visibilityError, retry: retryVisibility } = useVideoReactions(videoIds);
  const visible = useMemo(() => {
    const dismissed = new Set(reactions.filter((row) => row.reaction === 'not_interested').map((row) => row.video));
    return candidates.filter((item) => !dismissed.has(item.video.id));
  }, [candidates, reactions]);
  const needsMore = settingsSettled && !!profileId && (channelIds === undefined || channelIds.length > 0) && visible.length < SHOWN && hasNextPage;
  useEffect(() => {
    if (needsMore && isReady && visibilityReady && !isError && !visibilityError && !isFetchingNextPage) {
      void fetchNextPage();
    }
  }, [needsMore, isReady, visibilityReady, isError, visibilityError, isFetchingNextPage, fetchNextPage]);
  // New candidates must not appear before their reactions are known. While they load, retain only previously checked
  // cards that still belong to the current profile, channel scope and enabled platforms.
  const published = useRef<{ profileId: string; ids: ReadonlySet<string> }>({ profileId: '', ids: new Set() });
  const items = useMemo(() => {
    if (!settingsSettled) {
      return [];
    }
    if (visibilityReady) {
      return visible.slice(0, SHOWN);
    }
    if (published.current.profileId !== profileId) {
      return [];
    }
    // IDs carry approval only; current rows provide progress, metadata and most-recently-played order.
    return visible.filter((item) => published.current.ids.has(item.video.id));
  }, [settingsSettled, visible, visibilityReady, profileId]);
  const retry = useCallback(() => {
    if (profileError) {
      retryProfile();
      return;
    }
    if (visibilityError) {
      retryVisibility();
      return;
    }
    if (isFetchingNextPage) {
      return;
    }
    if (hasNextPage) {
      // The infinite-query controller retries the failed window without discarding the committed pages.
      void fetchNextPage();
    } else {
      // A failed first page has no next-page window yet; recreate that query explicitly.
      setAttempt((value) => value + 1);
    }
  }, [profileError, retryProfile, visibilityError, retryVisibility, isFetchingNextPage, hasNextPage, fetchNextPage]);
  const ids = useMemo<ReadonlySet<string>>(() => new Set(items.map((item) => item.video.id)), [items]);
  useEffect(() => {
    // Remember removals learned from a ready chunk while other reactions are still pending or failed.
    published.current = { profileId, ids };
  }, [profileId, ids]);
  return {
    items,
    ids,
    isReady: !enabled || (profileReady && settingsSettled && isReady && visibilityReady && !needsMore && !isFetchingNextPage),
    isError: enabled && (profileError || isError || visibilityError),
    retry,
  };
}
