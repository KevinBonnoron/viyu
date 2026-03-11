import { useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { adminApi } from '@/lib/admin-api';
import { ApiError } from '@/lib/api-error';
import { useI18n } from '@/providers/i18n-provider';

export function AdminGlobalSettings({ retryAdmin }: { retryAdmin: () => void }) {
  const { t } = useI18n();
  const [registration, setRegistration] = useState<boolean>();
  const [ocr, setOCR] = useState<boolean>();
  const [pending, setPending] = useState<'registration' | 'ocr'>();
  const [error, setError] = useState(false);
  const [revision, setRevision] = useState(0);

  useEffect(() => {
    const controller = new AbortController();
    setError(false);
    void Promise.all([adminApi.getRegistration({ signal: controller.signal }), adminApi.getSeriesOCR({ signal: controller.signal })])
      .then(([registrationResult, ocrResult]) => {
        if (!controller.signal.aborted) {
          setRegistration(registrationResult.enabled);
          setOCR(ocrResult.enabled);
        }
      })
      .catch((reason: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        if (reason instanceof ApiError && reason.status === 403) {
          retryAdmin();
        }
        setError(true);
      });
    return () => controller.abort();
  }, [revision, retryAdmin]);

  const toggle = async (setting: 'registration' | 'ocr') => {
    const enabled = setting === 'registration' ? registration : ocr;
    if (enabled === undefined || pending) {
      return;
    }
    setPending(setting);
    setError(false);
    try {
      const result = setting === 'registration' ? await adminApi.setRegistration(!enabled) : await adminApi.setSeriesOCR(!enabled);
      if (setting === 'registration') {
        setRegistration(result.enabled);
      } else {
        setOCR(result.enabled);
      }
    } catch (reason) {
      if (reason instanceof ApiError && reason.status === 403) {
        retryAdmin();
      }
      setError(true);
    } finally {
      setPending(undefined);
    }
  };

  const loading = registration === undefined || ocr === undefined;
  return (
    <section className="overflow-hidden rounded-xl border bg-card" aria-label={t('admin.settings')}>
      {loading ? (
        <div className="space-y-3 p-5">
          {error ? (
            <Button type="button" variant="outline" onClick={() => setRevision((value) => value + 1)}>
              {t('admin.retry')}
            </Button>
          ) : (
            <output className="text-sm text-muted-foreground">{t('admin.ocrLoading')}</output>
          )}
        </div>
      ) : (
        <div className="divide-y">
          <SettingToggle
            id="registration"
            title={t('admin.registrationTitle')}
            description={t('admin.registrationDescription')}
            checked={registration}
            disabled={!!pending}
            onChange={() => void toggle('registration')}
          />
          <SettingToggle
            id="ocr"
            title={t('admin.ocrTitle')}
            description={t('admin.ocrDescription')}
            checked={ocr}
            disabled={!!pending}
            onChange={() => void toggle('ocr')}
          />
        </div>
      )}
      {error && (
        <p role="alert" className="border-t px-5 py-3 text-sm text-destructive">
          {t('admin.settingsError')}
        </p>
      )}
    </section>
  );
}

function SettingToggle({
  id,
  title,
  description,
  checked,
  disabled,
  onChange,
}: {
  id: string;
  title: string;
  description: string;
  checked: boolean;
  disabled: boolean;
  onChange: () => void;
}) {
  return (
    <div className="flex items-start justify-between gap-6 px-5 py-5">
      <div className="min-w-0 space-y-1">
        <h3 id={`admin-${id}-label`} className="text-sm font-medium">
          {title}
        </h3>
        <p id={`admin-${id}-description`} className="max-w-2xl text-sm text-muted-foreground">
          {description}
        </p>
      </div>
      <button
        type="button"
        role="switch"
        aria-checked={checked}
        aria-labelledby={`admin-${id}-label`}
        aria-describedby={`admin-${id}-description`}
        disabled={disabled}
        onClick={onChange}
        className={`relative inline-flex h-6 w-11 shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-wait disabled:opacity-50 ${checked ? 'bg-primary' : 'bg-input'}`}
      >
        <span
          className={`pointer-events-none inline-block size-5 rounded-full bg-background shadow-lg transition-transform ${checked ? 'translate-x-5' : 'translate-x-0'}`}
        />
      </button>
    </div>
  );
}
