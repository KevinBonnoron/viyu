import { createRouter, RouterProvider } from '@tanstack/react-router';
import { StrictMode, useEffect, useState } from 'react';
import ReactDOM from 'react-dom/client';
import './index.css';
import { TooltipLayer } from '@/components/tooltip-layer';
import { Button } from '@/components/ui/button';
import { bootstrapSession, isStandaloneClient, storedServerUrl } from '@/lib/server';
import { installSessionHandlers } from '@/lib/session';
import { I18nProvider, type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { routeTree } from './routeTree.gen';

// A native shell without a configured server goes straight to the setup route: there is nothing else to do.
const initialSession = bootstrapSession().then(
  async () => {
    installSessionHandlers();
    return isStandaloneClient && !(await storedServerUrl());
  },
  (): TranslationKey => 'startup.storageUnavailable',
);
const router = createRouter({ routeTree });

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router;
  }
}

function Startup() {
  const { t } = useI18n();
  const [state, setState] = useState<TranslationKey | 'ready' | undefined>();
  useEffect(() => {
    let active = true;
    void initialSession.then((result) => {
      if (!active) {
        return;
      }
      if (result === true && window.location.pathname !== '/setup') {
        router.history.replace('/setup');
      }
      setState(typeof result === 'string' ? result : 'ready');
    });
    return () => {
      active = false;
    };
  }, []);
  if (state === 'ready' || (state === undefined && !isStandaloneClient)) {
    return <RouterProvider router={router} />;
  }
  return (
    <main className="flex min-h-dvh items-center justify-center p-6">
      <div className="w-full max-w-md space-y-4">
        <h1 className="text-2xl font-bold text-primary">Viyu</h1>
        {state === undefined ? (
          <output className="text-muted-foreground">{t('startup.starting')}</output>
        ) : (
          <>
            <p role="alert" className="text-sm text-destructive">
              {t(state)}
            </p>
            <Button onClick={() => window.location.reload()}>{t('common.retry')}</Button>
          </>
        )}
      </div>
    </main>
  );
}

const rootElement = document.getElementById('root');
if (!rootElement) {
  throw new Error('Root element not found. Check if the id is correct.');
}

ReactDOM.createRoot(rootElement).render(
  <StrictMode>
    <I18nProvider>
      <Startup />
      <TooltipLayer />
    </I18nProvider>
  </StrictMode>,
);
