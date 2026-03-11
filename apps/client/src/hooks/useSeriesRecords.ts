import { createLiveQueryCollection, eq, inArray, useLiveQuery } from '@tanstack/react-db';
import { useEffect, useMemo, useRef, useState } from 'react';
import { seriesCollection } from '@/collections/series.collection';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { asRowSource, useChunkedRows } from '@/hooks/useChunkedRows';
import { ApiError, requestError } from '@/lib/api-error';
import { seriesApi } from '@/lib/series-api';
import { seriesChangeKey } from '@/lib/series-key';
import type { TranslationKey } from '@/providers/i18n-provider';
import { useSubscriptions } from '@/providers/subscriptions-provider';
import type { SeriesDetailResponse, SeriesRecord, SeriesResponse } from '@/types/series.type';

/** How long a synchronising series is left alone before it is read again. */
const POLL_MS = 2000;

/**
 * The live `series` records of some channels, loaded in bounded chunks (never one filter over every subscription) and
 * summed into `changeKey`: what the server-computed answers below are re-read on.
 */
function useSeriesRecords(channelIds: readonly string[]): { changeKey: string; isReady: boolean; isError: boolean } {
  const { rows, isReady, isError } = useChunkedRows<SeriesRecord>(channelIds, '', (chunk) =>
    asRowSource(
      createLiveQueryCollection({
        query: (q) => q.from({ series: seriesCollection }).where(({ series }) => inArray(series.channel, chunk)),
        startSync: true,
      }),
    ),
  );
  const changeKey = useMemo(() => seriesChangeKey(rows), [rows]);
  return { changeKey, isReady, isError };
}

/** The live `series` record with this id, reduced to its change key (empty when it does not exist). */
function useSeriesRecord(seriesId: string | undefined): { changeKey: string; isReady: boolean; isError: boolean } {
  const query = useLiveQuery({
    query: (q) => (seriesId ? q.from({ series: seriesCollection }).where(({ series }) => eq(series.id, seriesId)) : undefined),
  });
  const changeKey = useMemo(() => seriesChangeKey(query.data ?? []), [query.data]);
  return { changeKey, isReady: query.isReady, isError: query.isError };
}

interface RequestState<T> {
  scope: string;
  value: T | null;
  error: TranslationKey | null;
}

/** Resolves when `wake` is called, `ms` passed (when given) or `signal` aborted. */
function idle(signal: AbortSignal, wake: { current: (() => void) | null }, ms?: number): Promise<void> {
  return new Promise((resolve) => {
    const timer = ms === undefined ? undefined : window.setTimeout(done, ms);
    function done() {
      window.clearTimeout(timer);
      signal.removeEventListener('abort', done);
      wake.current = null;
      resolve();
    }
    wake.current = done;
    signal.addEventListener('abort', done, { once: true });
  });
}

/**
 * One server-computed series answer (summaries, ordered episodes, sync state), kept current without subscribing to
 * anything by hand: it is read when `enabled` first holds, again whenever `refreshKey` (derived from the live records
 * it depends on) changes, and while `shouldPoll` says the server is still working. Reads never overlap, so a burst of
 * record changes costs one more read, never a cancelled one; a change of `scope` starts over.
 */
function useSeriesRequest<T>({
  scope,
  refreshKey,
  enabled,
  load,
  shouldPoll,
}: {
  scope: string;
  refreshKey: string;
  enabled: boolean;
  load: (signal: AbortSignal) => Promise<T>;
  shouldPoll?: (value: T) => boolean;
}) {
  const [state, setState] = useState<RequestState<T> | null>(null);
  const [revision, setRevision] = useState(0);
  const requestedRevision = useRef(0);
  const wake = useRef<(() => void) | null>(null);
  const latest = useRef({ refreshKey, load, shouldPoll });
  latest.current = { refreshKey, load, shouldPoll };
  useEffect(() => {
    wake.current?.();
  }, [refreshKey]);
  useAbortableEffect(
    async (signal) => {
      if (!enabled) {
        return;
      }
      // A retry after a failed request clears the error while the page is read again.
      if (requestedRevision.current !== revision) {
        requestedRevision.current = revision;
        setState((previous) => (previous?.scope === scope ? { ...previous, error: null } : previous));
      }
      while (!signal.aborted) {
        const handled = latest.current.refreshKey;
        let polling = false;
        try {
          const value = await latest.current.load(signal);
          if (signal.aborted) {
            return;
          }
          setState({ scope, value, error: null });
          polling = latest.current.shouldPoll?.(value) ?? false;
        } catch (failure) {
          if (signal.aborted) {
            return;
          }
          const error = failure instanceof ApiError && failure.status === 404 ? 'series.notFound' : requestError(failure);
          setState((previous) => ({ scope, value: previous?.scope === scope ? previous.value : null, error }));
        }
        // Nothing to do until the records change (or the poll is due), unless they already changed while reading.
        if (latest.current.refreshKey === handled) {
          await idle(signal, wake, polling ? POLL_MS : undefined);
        }
      }
    },
    [scope, enabled, revision],
  );
  const current = state?.scope === scope ? state : null;
  return {
    value: current?.value ?? null,
    error: current?.error ?? null,
    /** No answer for this scope yet. */
    loading: !current,
    retry: () => setRevision((value) => value + 1),
  };
}

/** The ids of the channels the active profile is subscribed to, and their key; `isSettled` once the subscriptions are known (or failed to load, which must not block the pages forever). */
function useSubscribedChannels() {
  const { data, isReady, isError } = useSubscriptions();
  const channelIds = useMemo(() => [...new Set(data.map((subscription) => subscription.channel))].sort(), [data]);
  return { channelIds, channelKey: channelIds.join(','), isSettled: isReady || isError };
}

/** The series of the active profile's subscriptions (summaries, sync and analysis state), re-read when a `series` record of those channels changes. */
export function useSeriesSummaries(profileId: string) {
  const { channelIds, channelKey, isSettled } = useSubscribedChannels();
  const records = useSeriesRecords(channelIds);
  // The subscription set is known before the request starts: loading earlier would start one that is replaced once it arrives.
  const request = useSeriesRequest<SeriesResponse>({
    scope: profileId,
    refreshKey: `${channelKey}|${records.changeKey}`,
    enabled: isSettled && (records.isReady || records.isError),
    load: (signal) => seriesApi.list(profileId, { signal }),
  });
  return { result: request.value, error: request.error, retry: request.retry };
}

/** The series with this `series` record id, re-read when its record changes and while its channel is still synchronising. */
export function useSeriesDetails(profileId: string, seriesId?: string) {
  const { channelKey, isSettled } = useSubscribedChannels();
  const record = useSeriesRecord(seriesId);
  const request = useSeriesRequest<SeriesDetailResponse>({
    scope: `${profileId}:${seriesId ?? ''}`,
    refreshKey: `${channelKey}|${record.changeKey}`,
    enabled: !!seriesId && isSettled && (record.isReady || record.isError),
    load: (signal) => seriesApi.detail(seriesId ?? '', profileId, { signal }),
    shouldPoll: ({ sync }) => sync.status === 'pending' || sync.status === 'running',
  });
  return {
    series: request.value?.series ?? null,
    sync: request.value?.sync ?? null,
    episodes: request.value?.episodes ?? null,
    dates: request.value?.dates ?? null,
    error: request.error,
    loading: !!seriesId && request.loading,
    retry: request.retry,
  };
}
