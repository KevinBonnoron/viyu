import { useMemo } from 'react';
import { useHideSubscriberOnly, useHideWatched } from '@/hooks/useUserSettings';
import { useVideoStatuses } from '@/hooks/useVideoStatuses';
import type { VideoStatus } from '@/lib/video-status';
import type { VideoResult } from '@/types';

/**
 * Which items of a list the active profile gets to see, and what its history says about the videos among them.
 * `videoOf` maps an item to its video (a list may mix in other things, like channels, which are always shown).
 * With `discovery` the settings apply (subscriber-only and watched videos may be hidden); `hideDismissed` also drops
 * the videos marked "not interested". A stream in progress is never "watched" or dismissed: it is on air now.
 * Nothing is hidden until the settings and the statuses are known.
 */
export function useVisibleVideos<T>(
  items: readonly T[],
  videoOf: (item: T) => VideoResult | undefined,
  { discovery, hideDismissed }: { discovery: boolean; hideDismissed: boolean },
): { visible: T[]; statuses: ReadonlyMap<string, VideoStatus>; isReady: boolean; isError: boolean } {
  const ids = useMemo(() => items.flatMap((item) => videoOf(item)?.id ?? []), [items, videoOf]);
  const { statuses, isReady, isError } = useVideoStatuses(ids);
  const hideSubscriberOnly = useHideSubscriberOnly() && discovery;
  const hideWatched = useHideWatched() && discovery && isReady && !isError;
  const visible = useMemo(
    () =>
      items.filter((item) => {
        const video = videoOf(item);
        if (!video) {
          return true;
        }
        const status = statuses.get(video.id);
        return (
          !(hideSubscriberOnly && video.subscriberOnly) &&
          !(hideWatched && !video.live && status?.watched) &&
          !(hideDismissed && !video.live && status?.reaction === 'not_interested')
        );
      }),
    [items, videoOf, statuses, hideSubscriberOnly, hideWatched, hideDismissed],
  );
  return { visible, statuses, isReady, isError };
}
