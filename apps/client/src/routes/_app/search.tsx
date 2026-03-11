import { createFileRoute, redirect, useLocation } from '@tanstack/react-router';
import { Search } from 'lucide-react';
import { useEffect, useMemo, useRef, useState } from 'react';
import { EmptyState } from '@/components/empty-state';
import { ErrorSnackbar } from '@/components/error-snackbar';
import { PageTitle } from '@/components/page-title';
import { PlatformFilters } from '@/components/platform-filters';
import { SearchGrid } from '@/components/search-grid';
import { SourceModes } from '@/components/source-feed';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { VideoGridSkeleton } from '@/components/video-grid-skeleton';
import { youtubeVideoIdFromUrl } from '@/features/video/utils/youtube-url';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { useLocalSearch } from '@/hooks/useLocalSearch';
import { useSearch, useTypedSearchSettle } from '@/hooks/useSearch';
import { useVisibleVideos } from '@/hooks/useVisibleVideos';
import { requestError } from '@/lib/api-error';
import { useOpenLink } from '@/lib/links';
import { localVideoResult } from '@/lib/local-search';
import { platformName } from '@/lib/platforms';
import { searchPlatformOrAll } from '@/lib/source-activation';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useActiveSource, useSources } from '@/providers/sources-provider';
import { isChannelResult, type SearchResult } from '@/types';

export const Route = createFileRoute('/_app/search')({
  validateSearch: (search: Record<string, unknown>): { q?: string } => ({
    q: typeof search.q === 'string' ? search.q : undefined,
  }),
  beforeLoad: ({ location, search }) => {
    const params = new URLSearchParams(location.searchStr);
    if (params.has('source')) {
      throw redirect({ to: '/search', search: { q: search.q }, state: { viyuSource: params.get('source') || undefined }, replace: true });
    }
  },
  component: SearchPage,
});

function SearchPage() {
  const { t } = useI18n();
  const { q } = Route.useSearch();
  const source = useActiveSource(useLocation({ select: (location) => location.state.viyuSource }));
  const { isEnabled } = useSources();
  // A platform chosen earlier that has been disabled since is not searched: the search goes back to every enabled platform.
  const platform = searchPlatformOrAll(useLocation({ select: (location) => location.state.viyuSearchPlatform }) ?? source ?? 'all', isEnabled);
  const youtubeEnabled = isEnabled('youtube');
  // A pasted YouTube link is opened directly only while YouTube is enabled; otherwise it is an ordinary query.
  const navigate = Route.useNavigate();
  const openLink = useOpenLink();
  const [linkError, setLinkError] = useState<TranslationKey | null>(null);
  const linkSource = source === 'youtube' ? source : undefined;
  const searchInput = useRef<HTMLInputElement>(null);
  const [query, setQuery] = useState(q ?? '');
  const [retry, setRetry] = useState(0);
  useEffect(() => {
    searchInput.current?.focus({ preventScroll: true });
  }, []);
  // The URL query follows the input, except when it is the echo of a search this page launched:
  // overwriting the input then would discard characters typed while that search was starting.
  const own = useRef<string>(undefined);
  useEffect(() => {
    if (q === own.current) {
      return;
    }
    own.current = undefined;
    setQuery(q ?? '');
  }, [q]);
  useAbortableEffect(
    async (signal) => {
      if (!q || !youtubeEnabled || !youtubeVideoIdFromUrl(q)) {
        return;
      }
      setLinkError(null);
      try {
        await openLink(q, { source: linkSource, signal });
      } catch (failure: unknown) {
        if (!signal.aborted) {
          setLinkError(requestError(failure));
        }
      }
    },
    [q, linkSource, openLink, youtubeEnabled],
  );
  // Typing alone searches after a pause. It replaces the history entry so keystrokes do not stack up
  // (only Enter pushes one) and never opens a pasted video link; that needs explicit validation.
  useTypedSearchSettle(query, (value) => {
    if (value !== q) {
      own.current = value;
      void navigate({ search: { q: value }, state: { viyuSource: source, viyuSearchPlatform: platform }, replace: true });
    }
  });
  return (
    <div className="space-y-6">
      <PageTitle description={t('ui.library.searchDescription')}>{t('nav.search')}</PageTitle>
      <SourceModes
        source={source}
        capability="search"
        localLabel={t('search.allTab')}
        onChange={(selected, selectedPlatform) => void navigate({ search: { q }, state: { viyuSource: selected, viyuSearchPlatform: selectedPlatform } })}
      />
      <form
        onSubmit={(event) => {
          event.preventDefault();
          const value = query.trim();
          if (!value) {
            return;
          }
          if (youtubeEnabled && youtubeVideoIdFromUrl(value)) {
            setLinkError(null);
            openLink(value, { source: linkSource }).catch((failure: unknown) => setLinkError(requestError(failure)));
          } else if (value === q) {
            setRetry((previous) => previous + 1);
          } else {
            own.current = value;
            void navigate({ search: { q: value }, state: { viyuSource: source, viyuSearchPlatform: platform } });
          }
        }}
        className="flex flex-col gap-3 rounded-2xl border bg-card p-3 shadow-xs sm:flex-row"
      >
        <div className="relative flex-1">
          <Search aria-hidden="true" className="absolute left-3 top-1/2 -translate-y-1/2 size-4 text-muted-foreground" />
          <Input
            ref={searchInput}
            aria-label={t('search.videos')}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder={t('search.placeholder')}
            className="border-transparent bg-transparent pl-10 shadow-none focus-visible:border-primary"
          />
        </div>
        <Button type="submit">{t('nav.search')}</Button>
      </form>
      {q?.trim() && !(youtubeEnabled && youtubeVideoIdFromUrl(q)) ? (
        <SearchResults key={`${platform}:${source ?? 'public'}:${q}:${retry}`} query={q} source={source} searchPlatform={platform} reuse={retry === 0} />
      ) : (
        !q && <EmptyState icon={Search} title={t('ui.library.searchEmptyTitle')} description={t('ui.library.searchEmptyDescription')} />
      )}
      {linkError && <ErrorSnackbar message={t(linkError)} action={t('common.dismiss')} onAction={() => setLinkError(null)} />}
    </div>
  );
}

const videoOf = (result: SearchResult) => (isChannelResult(result) ? undefined : result);

function SearchResults({ query, source, searchPlatform, reuse }: { query: string; source?: string; searchPlatform: string; reuse: boolean }) {
  const { t } = useI18n();
  const {
    results,
    hasMore,
    loading,
    requestError: error,
    platformFailures,
    loadMore,
    retry,
  } = useSearch({ query, platform: searchPlatform, source, reuseHandoff: reuse });
  // Local matches come first, instantly from records Viyu already holds; the server answers are
  // de-duplicated against them and appended as they arrive. They respect the search's platform
  // intent, not the platform list derived from whatever has loaded.
  const local = useLocalSearch(query, searchPlatform === 'all' ? undefined : searchPlatform);
  const localResults = useMemo<SearchResult[]>(
    () => [
      ...local.channels.map((channel) => ({ id: channel.id, platform: channel.platform, name: channel.name, thumbnail: channel.thumbnail }) as SearchResult),
      ...local.videos.map((match) => localVideoResult(match) as SearchResult),
    ],
    [local],
  );
  const localIds = useMemo(() => new Set(localResults.map((result) => `${result.platform}:${result.id}`)), [localResults]);
  const combinedResults = useMemo(
    () => [...localResults, ...results.filter((result) => !localIds.has(`${result.platform}:${result.id}`))],
    [localResults, results, localIds],
  );
  const platforms = useMemo(() => [...new Set(combinedResults.map((result) => result.platform))].sort(), [combinedResults]);
  const [selectedPlatform, setSelectedPlatform] = useState<string>();
  const platform = selectedPlatform && platforms.includes(selectedPlatform) ? selectedPlatform : undefined;
  const filteredResults = useMemo(
    () => (platform ? combinedResults.filter((result) => result.platform === platform) : combinedResults),
    [combinedResults, platform],
  );
  // The library (followed channels and their videos) and the search results are two sections, never one mixed grid.
  const libraryResults = useMemo(() => filteredResults.filter((result) => localIds.has(`${result.platform}:${result.id}`)), [filteredResults, localIds]);
  const searchResults = useMemo(() => filteredResults.filter((result) => !localIds.has(`${result.platform}:${result.id}`)), [filteredResults, localIds]);
  const { visible } = useVisibleVideos(filteredResults, videoOf, { discovery: true, hideDismissed: false });

  return (
    <div className="space-y-4">
      <PlatformFilters platforms={platforms} selected={platform} onChange={setSelectedPlatform} />
      {platformFailures.length > 0 && (
        <ul role="status" className="space-y-1 rounded-xl border border-destructive/30 bg-destructive/5 px-4 py-3 text-sm">
          {platformFailures.map((failure) => (
            <li key={failure.platform}>
              <span className="font-medium">{t('search.platformFailed', { platform: platformName(failure.platform) })}</span>{' '}
              <span className="text-muted-foreground">{t(failure.error)}</span>
            </li>
          ))}
        </ul>
      )}
      {combinedResults.length > 0 && <p className="text-xs text-muted-foreground">{t('ui.library.resultCount', { count: visible.length })}</p>}
      {libraryResults.length > 0 && (
        <section aria-label={t('search.localSection')} className="space-y-3">
          <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t('search.localSection')}</h2>
          <SearchGrid results={libraryResults} />
        </section>
      )}
      {searchResults.length > 0 && (
        <section aria-label={t('search.resultsSection')} className="space-y-3">
          {libraryResults.length > 0 && <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t('search.resultsSection')}</h2>}
          <SearchGrid results={searchResults} onEndReached={hasMore && !loading && !error ? loadMore : undefined} />
        </section>
      )}
      {loading && (
        <div aria-live="polite" aria-busy="true">
          <span className="sr-only">{t(results.length === 0 ? 'search.loading' : 'search.moreLoading')}</span>
          <VideoGridSkeleton />
        </div>
      )}
      {error && <ErrorSnackbar message={t(error)} action={t('search.retry')} onAction={retry} />}
      {!loading && !error && combinedResults.length === 0 && (
        <EmptyState icon={Search} title={t('search.empty')} description={t('ui.library.searchNoResultsDescription')} />
      )}
    </div>
  );
}
