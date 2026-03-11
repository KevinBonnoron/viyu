import { useEffect, useMemo, useRef, useState } from 'react';

/** The part of a live query collection the hook below reads. */
export interface RowSource<T> {
  readonly toArray: readonly T[];
  readonly status: string;
  subscribeChanges: (callback: () => void) => { unsubscribe: () => void };
  on: (event: 'status:change', callback: () => void) => () => void;
  cleanup: () => unknown;
}

/** Types a live query collection as the rows source of `useChunkedRows`. */
export function asRowSource<T>(collection: unknown): RowSource<T> {
  return collection as RowSource<T>;
}

const CHUNK_SIZE = 40;
const EMPTY: readonly never[] = [];

interface ChunkState<T> {
  key: string;
  sources: readonly RowSource<T>[];
  version: number;
}

/**
 * Live rows for a list of ids of any length, loaded through on-demand collections without ever building a filter over
 * more than `CHUNK_SIZE` ids: the ids are cut into chunks and `build` makes one live query per chunk. The rows of all the
 * chunks are returned together. `scope` names whatever else the queries depend on (the profile), so a change of it rebuilds them.
 */
export function useChunkedRows<T>(
  ids: readonly string[],
  scope: string,
  build: (chunk: string[]) => RowSource<T>,
): { rows: readonly T[]; isReady: boolean; isError: boolean } {
  const unique = useMemo(() => [...new Set(ids)].sort(), [ids]);
  const key = `${scope}|${unique.join(',')}`;
  const latest = useRef({ unique, build });
  latest.current = { unique, build };
  const [state, setState] = useState<ChunkState<T>>({ key: '', sources: EMPTY, version: 0 });
  useEffect(() => {
    const { unique: wanted, build: make } = latest.current;
    let active = true;
    const sources: RowSource<T>[] = [];
    for (let offset = 0; offset < wanted.length; offset += CHUNK_SIZE) {
      sources.push(make(wanted.slice(offset, offset + CHUNK_SIZE)));
    }
    const notify = () => {
      if (active) {
        setState((previous) => (previous.key === key ? { ...previous, version: previous.version + 1 } : previous));
      }
    };
    const statusListeners = sources.map((source) => source.on('status:change', notify));
    const subscriptions = sources.map((source) => source.subscribeChanges(notify));
    setState({ key, sources, version: 0 });
    return () => {
      active = false;
      for (const stop of statusListeners) {
        stop();
      }
      for (const subscription of subscriptions) {
        subscription.unsubscribe();
      }
      for (const source of sources) {
        void source.cleanup();
      }
    };
  }, [key]);
  const current = state.key === key ? state : null;
  // biome-ignore lint/correctness/useExhaustiveDependencies: `version` marks changes inside the sources, which the memo must follow.
  const rows = useMemo(() => (current ? current.sources.flatMap((source) => source.toArray) : (EMPTY as readonly T[])), [current?.sources, current?.version]);
  const isError = !!current?.sources.some((source) => source.status === 'error');
  const isReady = unique.length === 0 || (!!current && current.sources.every((source) => source.status === 'ready'));
  return { rows, isReady, isError };
}
