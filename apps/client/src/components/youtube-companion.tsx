import { Check, Copy } from 'lucide-react';
import { useCallback, useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Dialog } from '@/components/ui/dialog';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { pairingString } from '@/lib/companion-pairing';
import { serverBase } from '@/lib/pocketbase';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import { useProfiles } from '@/providers/profiles-provider';
import type { CompanionDevice, CompanionPairing } from '@/types';

/**
 * The YouTube card's section for the companion browser extension: pairing it to the active profile and managing the
 * paired devices. The server cannot tell the page when an extension completes pairing, so the device list is reloaded
 * when the pairing view closes and after a revocation.
 */
export function YoutubeCompanion({ enabled }: { enabled: boolean }) {
  const { t, language } = useI18n();
  const { activeProfile } = useProfiles();
  const [devices, setDevices] = useState<CompanionDevice[] | null>(null);
  const [devicesError, setDevicesError] = useState<TranslationKey | null>(null);
  const [pairing, setPairing] = useState<{ pairing: CompanionPairing; profileName: string } | null>(null);
  const [pending, setPending] = useState(false);
  const [revoking, setRevoking] = useState<string | null>(null);
  const [error, setError] = useState<TranslationKey | null>(null);
  const loads = useRef(0);
  const mounted = useRef(false);

  const load = useCallback(async () => {
    const attempt = ++loads.current;
    try {
      const list = await api.companion.devices();
      if (mounted.current && attempt === loads.current) {
        setDevices(list);
        setDevicesError(null);
      }
    } catch (failure) {
      if (mounted.current && attempt === loads.current) {
        setDevicesError(requestError(failure));
      }
    }
  }, []);

  useEffect(() => {
    mounted.current = true;
    void load();
    return () => {
      mounted.current = false;
    };
  }, [load]);

  const pair = async () => {
    if (!activeProfile) {
      return;
    }
    setPending(true);
    setError(null);
    try {
      const created = await api.companion.createPairing(activeProfile.id);
      if (mounted.current) {
        setPairing({ pairing: created, profileName: activeProfile.name });
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

  const closePairing = () => {
    setPairing(null);
    void load();
  };

  const revoke = async (device: CompanionDevice) => {
    if (!window.confirm(t('sources.companion.revokeConfirm', { name: device.name }))) {
      return;
    }
    setRevoking(device.id);
    setError(null);
    try {
      await api.companion.revoke(device.id);
    } catch (failure) {
      if (mounted.current) {
        setError(requestError(failure));
      }
    } finally {
      if (mounted.current) {
        setRevoking(null);
        void load();
      }
    }
  };

  return (
    <section aria-labelledby="youtube-companion-title" className="space-y-3 rounded-xl border p-4">
      <h4 id="youtube-companion-title" className="text-sm font-semibold">
        {t('sources.companion.title')}
      </h4>
      <p className="text-sm leading-relaxed text-muted-foreground">{t('sources.companion.description')}</p>
      <div className="flex flex-wrap items-center gap-3">
        <Button variant="outline" disabled={pending || !enabled || !activeProfile} onClick={() => void pair()}>
          {pending ? t('sources.working') : t('sources.companion.pair')}
        </Button>
        {activeProfile ? (
          <span className="text-xs text-muted-foreground">{t('sources.companion.forProfile', { name: activeProfile.name })}</span>
        ) : (
          <span className="text-xs text-muted-foreground">{t('sources.companion.noProfile')}</span>
        )}
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {t(error)}
        </p>
      )}
      <div className="space-y-2">
        <h5 className="text-xs font-semibold uppercase tracking-wide text-muted-foreground">{t('sources.companion.devices')}</h5>
        {devicesError ? (
          <div role="alert" className="flex flex-wrap items-center gap-2 text-sm">
            <span className="text-destructive">{t(devicesError)}</span>
            <Button variant="outline" size="sm" onClick={() => void load()}>
              {t('common.retry')}
            </Button>
          </div>
        ) : devices === null ? (
          <output className="text-sm text-muted-foreground">{t('common.loading')}</output>
        ) : devices.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t('sources.companion.noDevices')}</p>
        ) : (
          <ul className="divide-y rounded-lg border">
            {devices.map((device) => (
              <li key={device.id} className="flex items-center gap-3 px-3 py-2">
                <div className="min-w-0 flex-1 space-y-0.5">
                  <p className="truncate text-sm font-medium">{device.name}</p>
                  <p className="text-xs text-muted-foreground">
                    {t('sources.companion.deviceProfile', { name: device.profileName })} ·{' '}
                    {device.lastSyncAt
                      ? t('sources.companion.lastSync', { date: new Date(device.lastSyncAt).toLocaleString(language) })
                      : t('sources.companion.neverSynced')}
                  </p>
                </div>
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={revoking === device.id}
                  aria-label={t('sources.companion.revokeNamed', { name: device.name })}
                  onClick={() => void revoke(device)}
                >
                  {t('sources.companion.revoke')}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </div>
      {pairing && <PairingDialog pairing={pairing.pairing} profileName={pairing.profileName} onClose={closePairing} />}
    </section>
  );
}

function PairingDialog({ pairing, profileName, onClose }: { pairing: CompanionPairing; profileName: string; onClose: () => void }) {
  const { t, language } = useI18n();
  const [copied, setCopied] = useState(false);
  const [copyError, setCopyError] = useState(false);
  const mounted = useRef(false);
  const value = pairingString(serverBase(), pairing.code);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      if (mounted.current) {
        setCopied(true);
        setCopyError(false);
      }
    } catch {
      if (mounted.current) {
        setCopyError(true);
      }
    }
  };

  return (
    <Dialog aria-labelledby="companion-pairing-title" aria-describedby="companion-pairing-description" onClose={onClose} className="max-w-lg space-y-4">
      <h2 id="companion-pairing-title" className="text-lg font-semibold tracking-tight">
        {t('sources.companion.pairingTitle', { name: profileName })}
      </h2>
      <p id="companion-pairing-description" className="text-sm leading-relaxed text-muted-foreground">
        {t('sources.companion.pairingInstructions')}
      </p>
      <textarea
        readOnly
        rows={4}
        value={value}
        onFocus={(event) => event.target.select()}
        aria-label={t('sources.companion.pairingLabel')}
        spellCheck={false}
        className="w-full resize-none break-all rounded-lg border bg-background p-3 font-mono text-xs"
      />
      <p className="text-xs text-muted-foreground">
        {t('sources.companion.expires', { time: new Date(pairing.expiresAt).toLocaleTimeString(language, { hour: '2-digit', minute: '2-digit' }) })}
      </p>
      {copyError && (
        <p role="alert" className="text-sm text-destructive">
          {t('sources.companion.copyError')}
        </p>
      )}
      <div className="flex flex-col-reverse justify-end gap-2 sm:flex-row">
        <Button type="button" variant="ghost" onClick={onClose}>
          {t('sources.companion.done')}
        </Button>
        <Button type="button" variant="secondary" onClick={() => void copy()} aria-live="polite">
          {copied ? <Check aria-hidden="true" className="size-4 text-success" /> : <Copy aria-hidden="true" className="size-4" />}
          {copied ? t('sources.companion.copied') : t('sources.companion.copy')}
        </Button>
      </div>
    </Dialog>
  );
}
