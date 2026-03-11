import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { isPlatformEnabled } from '@/lib/source-activation';
import type { Source } from '@/types';
import { useAuth } from './auth-provider';
import type { TranslationKey } from './i18n-provider';

interface SourcesContextValue {
  sources: Source[];
  loading: boolean;
  error: TranslationKey | null;
  revision: number;
  refresh: () => void;
  update: (source: Source) => void;
  disconnected: (platform: string) => void;
  /** Whether content of this platform may be shown; true for every platform while the sources are loading or failed to load. */
  isEnabled: (platform: string) => boolean;
  /** Turns the platform on or off for the account. Rejects when the server refuses; the state is then unchanged. */
  setEnabled: (platform: string, enabled: boolean) => Promise<void>;
}

const SourcesContext = createContext<SourcesContextValue | null>(null);

export function SourcesProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const [sources, setSources] = useState<Source[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<TranslationKey | null>(null);
  const [revision, setRevision] = useState(0);
  const [reload, setReload] = useState(0);
  const refresh = useCallback(() => setReload((value) => value + 1), []);

  useEffect(() => {
    const controller = new AbortController();
    setSources([]);
    setLoading(true);
    setError(null);
    if (!user) {
      setLoading(false);
      return;
    }
    void api.sources
      .list({ signal: controller.signal })
      .then((data) => {
        if (!controller.signal.aborted) {
          setSources(data);
          setRevision((value) => value + 1);
        }
      })
      .catch((failure: unknown) => {
        if (!controller.signal.aborted) {
          setError(requestError(failure));
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });
    return () => controller.abort();
  }, [user, reload]);

  const update = useCallback((source: Source) => {
    setSources((previous) =>
      previous.some((item) => item.platform === source.platform)
        ? previous.map((item) => (item.platform === source.platform ? source : item))
        : [...previous, source],
    );
    setRevision((value) => value + 1);
  }, []);
  const disconnected = useCallback((platform: string) => {
    setSources((previous) =>
      previous.map((source) => (source.platform === platform ? { ...source, connected: false, connectionId: undefined, updatedAt: undefined } : source)),
    );
    setRevision((value) => value + 1);
  }, []);
  const setEnabled = useCallback(
    async (platform: string, enabled: boolean) => {
      update(await api.sources.setEnabled(platform, enabled));
    },
    [update],
  );
  const known = !loading && !error;
  const isEnabled = useCallback((platform: string) => isPlatformEnabled(sources, platform, known), [sources, known]);
  const value = useMemo(
    () => ({ sources, loading, error, revision, refresh, update, disconnected, isEnabled, setEnabled }),
    [sources, loading, error, revision, refresh, update, disconnected, isEnabled, setEnabled],
  );

  return <SourcesContext.Provider value={value}>{children}</SourcesContext.Provider>;
}

export function useSources() {
  const context = useContext(SourcesContext);
  if (!context) {
    throw new Error('useSources must be used within SourcesProvider');
  }
  return context;
}

/** The connected source a page was opened on, or none when it was since disabled (the page then shows the Viyu view). */
export function useActiveSource(source?: string): string | undefined {
  const { isEnabled } = useSources();
  return source && isEnabled(source) ? source : undefined;
}
