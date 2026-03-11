import { useState } from 'react';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import type { SubscriptionLive } from '@/types';

/** The server answers from a one-minute cache, so asking more often would only repeat its answer. */
const LIVE_REFRESH_MS = 2 * 60_000;

/**
 * The profile's subscribed channels that are live now. `refreshKey` changes when the subscriptions do,
 * which asks again at once; otherwise the list is renewed every couple of minutes while the page is
 * visible. A failed lookup keeps the previous list: live status is a convenience, never an error.
 */
export function useSubscriptionsLive(profileId: string | undefined, refreshKey: string): SubscriptionLive[] {
  const [live, setLive] = useState<SubscriptionLive[]>([]);
  useAbortableEffect(
    (signal) => {
      if (!profileId) {
        setLive([]);
        return;
      }
      const load = () => {
        if (document.hidden) {
          return;
        }
        api.subscriptions
          .live(profileId, { signal })
          .then((result) => {
            if (!signal.aborted) {
              setLive(result);
            }
          })
          .catch(() => undefined);
      };
      load();
      const interval = window.setInterval(load, LIVE_REFRESH_MS);
      return () => window.clearInterval(interval);
    },
    [profileId, refreshKey],
  );
  return live;
}
