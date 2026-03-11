import { useEffect, useMemo, useRef, useState } from 'react';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { useDebouncedValue } from '@/hooks/useDebouncedValue';
import { api } from '@/lib/api';
import { ApiError, requestError } from '@/lib/api-error';
import { getSearchHandoff, handoffSearch } from '@/lib/search-handoff';
import { isTypedSearchQuery, SEARCH_DEBOUNCE_MS } from '@/lib/search-query';
import { mergeResults } from '@/lib/search-results';
import type { TranslationKey } from '@/providers/i18n-provider';
import type { SearchPage, SearchResult } from '@/types';

interface SearchSpec {
  query: string;
  platform: string;
  source?: string;
}

interface SearchState {
  key: string;
  results: SearchResult[];
  hasMore: boolean;
  /** The first page as the server sent it: what a handoff to the search page carries. */
  firstPage?: SearchPage;
  loading: boolean;
  error: TranslationKey | null;
}

const NO_RESULTS: SearchResult[] = [];
const withSource = (results: SearchResult[], source?: string): SearchResult[] => results.map((result) => ({ ...result, source }));

const NO_FAILURES: { platform: string; error: TranslationKey }[] = [];
function platformFailures(page?: SearchPage): { platform: string; error: TranslationKey }[] {
  if (!page?.failures?.length) {
    return NO_FAILURES;
  }
  return page.failures.map((failure) => ({ platform: failure.platform, error: requestError(new ApiError(failure.code, 0, failure.message)) }));
}

/**
 * Calls `onSettle` with the trimmed input once typing alone may launch it: long enough, not a pasted video link (that
 * needs explicit validation) and `SEARCH_DEBOUNCE_MS` without a keystroke.
 */
export function useTypedSearchSettle(input: string, onSettle: (query: string) => void): void {
  const settled = useDebouncedValue(input.trim(), SEARCH_DEBOUNCE_MS);
  const latest = useRef(onSettle);
  latest.current = onSettle;
  useEffect(() => {
    if (isTypedSearchQuery(settled)) {
      latest.current(settled);
    }
  }, [settled]);
}

/**
 * One search against the server, page by page: a request per page that is cancelled when the search changes or the
 * component unmounts, pages accumulated without duplicates, and the failure kept for the retry.
 *
 * - `typing`: the query comes from an input. It is launched after a pause and only when `isTypedSearchQuery` allows;
 *   results of an older query are never shown for a newer one. Otherwise the query is a committed one, launched at once.
 * - `reuseHandoff`: start from the page the quick-search dialog handed over for this very search, if any.
 * - `enabled`: nothing is searched (and nothing is shown) while false.
 */
export function useSearch({
  query,
  platform,
  source,
  typing = false,
  reuseHandoff = false,
  enabled = true,
}: {
  query: string;
  platform: string;
  source?: string;
  typing?: boolean;
  reuseHandoff?: boolean;
  enabled?: boolean;
}) {
  const trimmed = query.trim();
  const key =
    enabled && (typing ? isTypedSearchQuery(trimmed) : trimmed.length > 0)
      ? JSON.stringify({ query: trimmed, platform, source } satisfies SearchSpec)
      : undefined;
  const pausedKey = useDebouncedValue(key, SEARCH_DEBOUNCE_MS);
  // A typed query is launched after the pause; going idle (the dialog closed, the input cleared) takes effect at once.
  const launched = typing ? (key ? pausedKey : undefined) : key;
  const spec = useMemo(() => (launched ? (JSON.parse(launched) as SearchSpec) : undefined), [launched]);
  const [state, setState] = useState<SearchState | null>(() => {
    const page = reuseHandoff && key ? getSearchHandoff(trimmed, platform, source) : undefined;
    return key && page ? { key, results: withSource(page.results, source), hasMore: page.hasMore, firstPage: page, loading: false, error: null } : null;
  });
  const handedOff = useRef(state?.key);
  const [paging, setPaging] = useState({ key: launched, page: 1, attempt: 0 });
  const { page, attempt } = paging.key === launched ? paging : { page: 1, attempt: 0 };

  useAbortableEffect(
    async (signal) => {
      if (!launched || !spec || (page === 1 && attempt === 0 && handedOff.current === launched)) {
        return;
      }
      const base = (previous: SearchState | null): SearchState =>
        previous?.key === launched ? previous : { key: launched, results: [], hasMore: false, loading: true, error: null };
      setState((previous) => ({ ...base(previous), loading: true, error: null }));
      try {
        const data = await api.search(spec.query, { source: spec.source, platform: spec.platform, page, signal });
        if (signal.aborted) {
          return;
        }
        setState((previous) => {
          const current = base(previous);
          return {
            ...current,
            results: mergeResults(current.results, withSource(data.results, spec.source)),
            hasMore: data.hasMore,
            firstPage: page === 1 ? data : current.firstPage,
            loading: false,
            error: null,
          };
        });
      } catch (failure: unknown) {
        if (!signal.aborted) {
          setState((previous) => ({ ...base(previous), loading: false, error: requestError(failure) }));
        }
      }
    },
    [launched, page, attempt],
  );

  const current = key && state?.key === key ? state : null;
  return {
    results: current?.results ?? NO_RESULTS,
    hasMore: current?.hasMore ?? false,
    // Launched but not answered yet, including the instant before its request leaves.
    loading: !!key && key === launched && (!current || current.loading),
    requestError: current?.error ?? null,
    /** Platforms the combined search could not query (the others answered), with why. */
    platformFailures: platformFailures(current?.firstPage),
    /** Reads the next page; ignored while a page is loading, after a failure and when there is none. */
    loadMore: () => {
      if (!launched || !current || current.loading || current.error || !current.hasMore) {
        return;
      }
      setState((previous) => (previous ? { ...previous, loading: true } : previous));
      setPaging({ key: launched, page: page + 1, attempt: 0 });
    },
    /** Asks again for the page that failed. */
    retry: () => {
      if (launched) {
        setPaging({ key: launched, page, attempt: attempt + 1 });
      }
    },
    /** Leaves the first page for the search page to start from, when it is the page of this very search. */
    handOff: () => {
      if (current?.firstPage && spec && current.key === launched) {
        handoffSearch(spec.query, spec.platform, spec.source, current.firstPage);
      }
    },
  };
}
