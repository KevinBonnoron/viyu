import { useState } from 'react';
import { api } from '@/lib/api';
import { sleep } from '@/lib/sleep';
import type { SeriesSummary, SeriesSync } from '@/types/series.type';
import { useAbortableEffect } from './useAbortableEffect';

export function useChannelSeries(channelId: string, enabled: boolean, channelSyncing: boolean) {
  const [series, setSeries] = useState<SeriesSummary[]>([]);
  const [sync, setSync] = useState<SeriesSync | null>(null);
  const [error, setError] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useAbortableEffect(
    async (signal) => {
      if (!enabled) {
        return;
      }
      do {
        try {
          const result = await api.channels.series(channelId, { signal });
          if (signal.aborted) {
            return;
          }
          setSeries(result.series);
          setSync(result.sync);
          setError(false);
          if (!channelSyncing && result.sync.status === 'complete') {
            return;
          }
        } catch {
          if (!signal.aborted) {
            setError(true);
          }
          return;
        }
        await sleep(2000, signal);
      } while (!signal.aborted);
    },
    [channelId, enabled, channelSyncing, attempt],
  );

  return { series, sync, error, retry: () => setAttempt((value) => value + 1) };
}
