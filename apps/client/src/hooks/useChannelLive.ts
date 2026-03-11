import { useState } from 'react';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import type { ChannelLive } from '@/types';

/**
 * The stream a channel is broadcasting now, if any. A failed lookup shows nothing: the badge is a convenience, never an
 * error. An empty `channelId` looks nothing up. With `refreshMs` the lookup is repeated, keeping the last answer meanwhile
 * (the server caches a channel's status for a minute, so asking more often than that gains nothing).
 */
export function useChannelLive(channelId: string, refreshMs?: number): ChannelLive | null {
  const [live, setLive] = useState<ChannelLive | null>(null);
  useAbortableEffect(
    (signal) => {
      setLive(null);
      if (!channelId) {
        return;
      }
      const lookup = () =>
        api.channels
          .live(channelId, { signal })
          .then((result) => {
            if (!signal.aborted && result.live && result.recordId) {
              setLive(result);
            }
          })
          .catch(() => undefined);
      void lookup();
      const repeat = refreshMs ? window.setInterval(() => void lookup(), refreshMs) : undefined;
      return () => {
        if (repeat !== undefined) {
          window.clearInterval(repeat);
        }
      };
    },
    [channelId, refreshMs],
  );
  return live;
}
