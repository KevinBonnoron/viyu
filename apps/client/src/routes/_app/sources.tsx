import { createFileRoute } from '@tanstack/react-router';
import { ShieldCheck } from 'lucide-react';
import { type FormEvent, useEffect, useId, useRef, useState } from 'react';
import { PageTitle } from '@/components/page-title';
import { PlatformTile } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { Input } from '@/components/ui/input';
import { Switch } from '@/components/ui/switch';
import { YoutubeCompanion } from '@/components/youtube-companion';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { canConnectWithCookies, platformPresentation, type SourceStatus, sourceStatus } from '@/lib/platforms';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useSources } from '@/providers/sources-provider';
import type { Source } from '@/types';

const STATUS_LABELS: Record<SourceStatus, TranslationKey> = {
  connected: 'sources.connected',
  public: 'sources.publicAccess',
  notConnected: 'sources.notConnected',
};

export const Route = createFileRoute('/_app/sources')({ component: SourcesPage });

function SourcesPage() {
  const { t } = useI18n();
  const { sources, loading, error, refresh } = useSources();
  const [connecting, setConnecting] = useState<Source | null>(null);
  const enabled = sources.filter((source) => source.enabled);
  const disabled = sources.filter((source) => !source.enabled);

  return (
    <div className="max-w-5xl space-y-7">
      <PageTitle description={t('ui.sources.description')}>{t('nav.sources')}</PageTitle>
      {loading && <output>{t('sources.loading')}</output>}
      {error && (
        <div role="alert" className="space-y-2 text-sm">
          <p>{t(error)}</p>
          <Button variant="outline" onClick={refresh}>
            {t('sources.retry')}
          </Button>
        </div>
      )}
      {enabled.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t('sources.enabledGroup')}</h2>
          <div className="grid gap-4 md:grid-cols-2">
            {enabled.map((source) => (
              <SourceCard key={source.platform} source={source} onConnect={() => setConnecting(source)} />
            ))}
          </div>
        </section>
      )}
      {disabled.length > 0 && (
        <section className="space-y-3">
          <h2 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t('sources.disabledGroup')}</h2>
          <div className="grid gap-4 md:grid-cols-2">
            {disabled.map((source) => (
              <SourceCard key={source.platform} source={source} onConnect={() => setConnecting(source)} />
            ))}
          </div>
        </section>
      )}
      {!loading && !error && sources.length === 0 && <p className="text-muted-foreground">{t('sources.empty')}</p>}
      <p className="flex items-start gap-2 text-xs leading-relaxed text-muted-foreground">
        <ShieldCheck aria-hidden="true" className="mt-0.5 size-4 shrink-0" />
        {t('sources.description')}
      </p>
      {connecting && canConnectWithCookies(connecting) && <ConnectDialog source={connecting} onClose={() => setConnecting(null)} />}
    </div>
  );
}

function SourceCard({ source, onConnect }: { source: Source; onConnect: () => void }) {
  const { t, language } = useI18n();
  const { update, disconnected, setEnabled } = useSources();
  const [pending, setPending] = useState(false);
  const [toggling, setToggling] = useState(false);
  const [error, setError] = useState<TranslationKey | null>(null);
  const [notice, setNotice] = useState<TranslationKey | null>(null);
  const switchLabelId = useId();
  const presentation = platformPresentation(source.platform);
  const canConnect = canConnectWithCookies(source);
  const status = sourceStatus(source);

  const check = async () => {
    setPending(true);
    setError(null);
    setNotice(null);
    try {
      update(await api.sources.check(source.platform));
      setNotice('sources.sessionValid');
    } catch (failure) {
      setError(requestError(failure));
    } finally {
      setPending(false);
    }
  };

  const disconnect = async () => {
    if (!window.confirm(t('sources.disconnectConfirm', { name: source.name }))) {
      return;
    }
    setPending(true);
    setError(null);
    setNotice(null);
    try {
      await api.sources.disconnect(source.platform);
      disconnected(source.platform);
      setNotice('sources.disconnected');
    } catch (failure) {
      setError(requestError(failure));
    } finally {
      setPending(false);
    }
  };

  const toggle = async (enabled: boolean) => {
    setToggling(true);
    setError(null);
    setNotice(null);
    try {
      await setEnabled(source.platform, enabled);
    } catch (failure) {
      const key = requestError(failure);
      setError(key === 'errors.requestFailed' ? 'sources.toggleFailed' : key);
    } finally {
      setToggling(false);
    }
  };

  return (
    <article className={`flex h-full flex-col gap-4 rounded-2xl border bg-card p-5 shadow-xs ${source.enabled ? '' : 'opacity-75'}`}>
      <div className="flex items-start gap-3">
        <PlatformTile platform={source.platform} />
        <div className="min-w-0 flex-1 space-y-1.5">
          <h3 className="text-base font-semibold leading-tight tracking-tight">{source.name}</h3>
          <div className="flex flex-wrap items-center gap-1.5">
            {!source.enabled && (
              <span className="inline-flex items-center rounded-full bg-muted px-2 py-0.5 text-[11px] font-medium text-muted-foreground">
                {t('sources.disabledBadge')}
              </span>
            )}
            <span
              className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[11px] font-medium ${status === 'connected' ? 'bg-accent text-accent-foreground' : 'bg-muted text-muted-foreground'}`}
            >
              <span aria-hidden="true" className={`size-1.5 rounded-full ${status === 'connected' ? 'bg-primary' : 'bg-muted-foreground/60'}`} />
              {t(STATUS_LABELS[status])}
            </span>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <span id={switchLabelId} className="text-sm font-medium">
            {t('sources.enabledSwitch')}
          </span>
          <Switch checked={source.enabled} onCheckedChange={(checked) => void toggle(checked)} disabled={toggling} aria-labelledby={switchLabelId} />
        </div>
      </div>
      <p className="text-sm leading-relaxed text-muted-foreground">{presentation.descriptionKey ? t(presentation.descriptionKey) : source.description}</p>
      {canConnect && source.updatedAt && (
        <p className="text-xs text-muted-foreground">{t('sources.sessionUpdated', { date: new Date(source.updatedAt).toLocaleString(language) })}</p>
      )}
      {notice && <output className="text-sm">{t(notice)}</output>}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {t(error)}
        </p>
      )}
      {!source.enabled && (
        <p className="text-sm text-muted-foreground">{t(canConnect ? 'sources.enableFirst' : 'sources.enableToUse', { name: source.name })}</p>
      )}
      {source.platform === 'youtube' && <YoutubeCompanion enabled={source.enabled} />}
      {canConnect && (
        <div className="mt-auto flex flex-wrap gap-2 border-t pt-4">
          <Button disabled={pending || !source.enabled} onClick={onConnect}>
            {source.connected ? t('sources.reconnectShort') : t('sources.connect')}
          </Button>
          {source.connected && (
            <>
              <Button variant="outline" disabled={pending || !source.enabled} onClick={() => void check()}>
                {pending ? t('sources.working') : t('sources.checkSession')}
              </Button>
              <Button variant="ghost" disabled={pending} onClick={() => void disconnect()}>
                {t('sources.disconnect')}
              </Button>
            </>
          )}
        </div>
      )}
    </article>
  );
}

function ConnectDialog({ source, onClose }: { source: Source; onClose: () => void }) {
  const { t } = useI18n();
  const fileRead = useRef(0);
  const mounted = useRef(false);
  const { update } = useSources();
  const connectLabel = source.connected ? t('sources.reconnect', { name: source.name }) : t('sources.connectNamed', { name: source.name });
  const [cookies, setCookies] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<TranslationKey | null>(null);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
      fileRead.current += 1;
    };
  }, []);

  const dismiss = () => {
    if (pending) {
      return;
    }
    fileRead.current += 1;
    setCookies('');
    onClose();
  };

  const connect = async (event: FormEvent) => {
    event.preventDefault();
    if (pending) {
      return;
    }
    if (new TextEncoder().encode(cookies).length > 256 * 1024) {
      setError('sources.cookies.tooLarge');
      return;
    }
    fileRead.current += 1;
    setPending(true);
    setError(null);
    try {
      const connected = await api.sources.connect(source.platform, cookies);
      if (mounted.current) {
        update(connected);
        setCookies('');
        onClose();
      }
    } catch (failure) {
      if (mounted.current) {
        setError(requestError(failure));
      }
    } finally {
      if (mounted.current) {
        setPending(false);
      }
    }
  };

  return (
    <Dialog
      aria-labelledby="connect-source-title"
      aria-describedby="connect-source-description"
      busy={pending}
      onClose={dismiss}
      className="max-w-lg overflow-hidden p-0 sm:p-0"
    >
      <div className="flex max-h-[90dvh] flex-col">
        <div className="min-h-0 space-y-5 overflow-y-auto p-5 sm:p-6">
          <div className="flex items-center gap-3">
            <PlatformTile platform={source.platform} />
            <h2 id="connect-source-title" className="text-lg font-semibold tracking-tight">
              {connectLabel}
            </h2>
          </div>
          <p id="connect-source-description" className="text-sm leading-relaxed text-muted-foreground">
            {t(source.platform === 'twitch' ? 'sources.twitch.manualHint' : 'ui.sources.connectionHint')}
          </p>
          {source.platform === 'youtube' && (
            <details className="rounded-xl border px-4 py-3 text-xs leading-relaxed text-muted-foreground">
              <summary className="cursor-pointer text-xs font-medium text-foreground">{t('ui.sources.details')}</summary>
              <p className="mt-3">{t('sources.watchHistoryDescription')}</p>
            </details>
          )}
          <section aria-labelledby="source-cookie-title" className="space-y-4 rounded-lg border p-3 text-sm">
            <h3 id="source-cookie-title" className="font-medium">
              {t('sources.cookies.importTitle')}
            </h3>
            <form id="source-cookie-form" onSubmit={(event) => void connect(event)} className="space-y-4">
              <p className="text-muted-foreground">
                {t(source.platform === 'twitch' ? 'sources.twitch.manualDescription' : 'sources.cookies.importDescription')}
              </p>
              <p className="rounded-lg bg-muted p-3 text-xs leading-relaxed">
                <strong>{t('sources.cookies.securityTitle')}</strong> {t('sources.cookies.securityDescription')}
              </p>
              <p className="text-muted-foreground">{t('sources.cookies.expiryDescription')}</p>
              <fieldset disabled={pending} className="space-y-4">
                <label htmlFor={`source-${source.platform}-file`} className="block space-y-1">
                  <span>{t('sources.cookies.fileLabel')}</span>
                  <Input
                    id={`source-${source.platform}-file`}
                    type="file"
                    accept=".txt,text/plain"
                    onChange={(event) => {
                      const file = event.target.files?.[0];
                      event.target.value = '';
                      const readId = ++fileRead.current;
                      if (!file) {
                        return;
                      }
                      if (!file.name.toLowerCase().endsWith('.txt') || file.size > 256 * 1024) {
                        setError('sources.cookies.invalidFile');
                        return;
                      }
                      void file
                        .text()
                        .then((text) => {
                          if (readId !== fileRead.current) {
                            return;
                          }
                          setCookies(text);
                          setError(null);
                        })
                        .catch(() => {
                          if (readId === fileRead.current) {
                            setError('sources.cookies.readFailed');
                          }
                        });
                    }}
                  />
                </label>
                <label htmlFor={`source-${source.platform}-cookies`} className="block space-y-1">
                  <span>{t('sources.cookies.pasteLabel')}</span>
                  <textarea
                    id={`source-${source.platform}-cookies`}
                    required
                    value={cookies}
                    onChange={(event) => {
                      fileRead.current += 1;
                      setCookies(event.target.value);
                    }}
                    autoComplete="off"
                    spellCheck={false}
                    rows={5}
                    className="w-full rounded-lg border border-input bg-background p-3 font-mono text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring"
                  />
                </label>
              </fieldset>
            </form>
          </section>
        </div>
        <div className="shrink-0 space-y-3 border-t bg-muted/20 p-4">
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {t(error)}
            </p>
          )}
          <div className="flex flex-col-reverse justify-end gap-2 sm:flex-row">
            <Button form="source-cookie-form" type="submit" disabled={pending || !cookies.trim()}>
              {pending ? t('sources.validating') : t('sources.cookies.connect')}
            </Button>
            <Button type="button" variant="ghost" disabled={pending} onClick={dismiss}>
              {t('common.cancel')}
            </Button>
          </div>
        </div>
      </div>
    </Dialog>
  );
}
