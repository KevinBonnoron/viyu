import { Link } from '@tanstack/react-router';
import { Download, Upload } from 'lucide-react';
import { ClientResponseError, type RecordAuthResponse, type RecordModel } from 'pocketbase';
import { type FormEvent, useEffect, useRef, useState } from 'react';
import { SettingsAvatar } from '@/components/settings-avatar';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { api } from '@/lib/api';
import { pb } from '@/lib/pocketbase';
import { useAuth } from '@/providers/auth-provider';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';

type AccountField = 'name' | 'oldPassword' | 'password' | 'passwordConfirm';
type FormName = 'profile' | 'password';
type Feedback = { error?: TranslationKey; success?: TranslationKey; fields?: Partial<Record<AccountField, TranslationKey>> };

const fieldErrors: Record<AccountField, TranslationKey> = {
  name: 'settings.account.errors.name',
  oldPassword: 'settings.account.errors.oldPassword',
  password: 'settings.account.errors.password',
  passwordConfirm: 'settings.account.errors.passwordConfirm',
};

function accountError(error: unknown): Feedback {
  if (error instanceof ClientResponseError) {
    if (error.status === 0) {
      return { error: 'settings.account.errors.connection' };
    }
    if (error.status === 401) {
      return { error: 'settings.account.errors.session' };
    }
    const fields: Feedback['fields'] = {};
    for (const field of Object.keys(fieldErrors) as AccountField[]) {
      if (error.response.data?.[field]) {
        fields[field] = fieldErrors[field];
      }
    }
    if (Object.keys(fields).length) {
      return { fields };
    }
  }
  return { error: 'settings.account.errors.save' };
}

export function SettingsAccount() {
  const { user } = useAuth();
  const { t } = useI18n();
  const userId = user?.id;
  const displayName = String(user?.name ?? '');
  const nameAccount = useRef(userId);
  const [name, setName] = useState(String(user?.name ?? ''));
  const [oldPassword, setOldPassword] = useState('');
  const [password, setPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [capabilityState, setCapabilityState] = useState<'loading' | 'ready' | 'error'>('loading');
  const [capabilityAttempt, setCapabilityAttempt] = useState(0);
  const [pending, setPending] = useState<FormName | 'avatar' | null>(null);
  const [feedback, setFeedback] = useState<Record<FormName, Feedback>>({ profile: {}, password: {} });
  const nameRef = useRef(name);
  const failedName = useRef<string | null>(null);

  useEffect(() => {
    if (nameAccount.current === userId) {
      return;
    }
    nameAccount.current = userId;
    nameRef.current = displayName;
    failedName.current = null;
    setName(displayName);
  }, [userId, displayName]);
  useEffect(() => {
    let active = true;
    setCapabilityState('loading');
    pb.send<{ isAdmin: boolean }>('/api/account', { method: 'GET', requestKey: null })
      .then(() => {
        if (active) {
          setCapabilityState('ready');
        }
      })
      .catch(() => {
        if (active) {
          setCapabilityState('error');
        }
      });
    return () => {
      active = false;
    };
  }, [user?.id, capabilityAttempt]);

  const clearFeedback = (form: FormName) => setFeedback((current) => ({ ...current, [form]: {} }));
  useEffect(() => {
    if (!userId || pending || name === displayName || failedName.current === name) {
      return;
    }
    const timer = window.setTimeout(async () => {
      setPending('profile');
      try {
        const session = await pb.send<RecordAuthResponse<RecordModel>>('/api/account', { method: 'PATCH', body: { name }, requestKey: null });
        if (!session.token || !session.record?.id || session.record.id !== userId || pb.authStore.record?.id !== userId) {
          throw new Error('Invalid account response');
        }
        pb.authStore.save(session.token, session.record);
        if (nameRef.current === name) {
          setFeedback((current) => ({ ...current, profile: { success: 'settings.account.profileSaved' } }));
          window.setTimeout(() => setFeedback((current) => (current.profile.success ? { ...current, profile: {} } : current)), 2500);
        }
      } catch (error) {
        if (nameRef.current === name) {
          failedName.current = name;
          setFeedback((current) => ({ ...current, profile: accountError(error) }));
        }
      } finally {
        setPending(null);
      }
    }, 600);
    return () => window.clearTimeout(timer);
  }, [name, pending, userId, displayName]);

  const submitPassword = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (pending || !user || capabilityState !== 'ready') {
      return;
    }
    clearFeedback('password');
    if (password !== passwordConfirm) {
      setFeedback((current) => ({ ...current, password: { fields: { passwordConfirm: fieldErrors.passwordConfirm } } }));
      return;
    }
    setPending('password');
    try {
      const body = { password, passwordConfirm, oldPassword };
      const session = await pb.send<RecordAuthResponse<RecordModel>>('/api/account', { method: 'PATCH', body });
      if (!session.token || !session.record?.id || session.record.id !== user.id) {
        throw new Error('Invalid account response');
      }
      pb.authStore.save(session.token, session.record);
      setOldPassword('');
      setPassword('');
      setPasswordConfirm('');
      setFeedback((current) => ({ ...current, password: { success: 'settings.account.passwordUpdated' } }));
    } catch (error) {
      setFeedback((current) => ({ ...current, password: accountError(error) }));
    } finally {
      setPending(null);
    }
  };

  const renderFeedback = (form: FormName) => {
    const { error, success, fields } = feedback[form];
    const visibleError = error || fields?.oldPassword;
    return (
      <div aria-live="polite" aria-atomic="true" className="space-y-1 text-sm">
        {visibleError && (
          <p role="alert" className="text-destructive">
            {t(visibleError)}
          </p>
        )}
        {success && <output className="text-muted-foreground">{t(success)}</output>}
      </div>
    );
  };
  const fieldError = (form: FormName, field: AccountField) => feedback[form].fields?.[field];
  const renderFieldError = (form: FormName, field: AccountField) => {
    const error = fieldError(form, field);
    return error ? (
      <p id={`${form}-${field}-error`} role="alert" className="text-sm text-destructive">
        {t(error)}
      </p>
    ) : null;
  };

  return (
    <div className="space-y-6">
      <section aria-labelledby="avatar-heading" className="rounded-2xl border bg-card p-5 sm:p-6">
        <h2 id="avatar-heading" className="text-base font-semibold">
          {t('settings.account.profile')}
        </h2>
        <div className="mt-4 flex flex-col gap-6 sm:flex-row sm:items-start">
          <SettingsAvatar busy={!!pending} onBusyChange={(busy) => setPending(busy ? 'avatar' : null)} />
          <div className="w-full max-w-lg space-y-4">
            <div className="space-y-2">
              <label htmlFor="account-name" className="text-sm font-medium">
                {t('settings.account.displayName')}
              </label>
              <Input
                id="account-name"
                name="name"
                autoComplete="nickname"
                value={name}
                disabled={pending === 'avatar'}
                aria-invalid={!!fieldError('profile', 'name')}
                aria-describedby={fieldError('profile', 'name') ? 'profile-name-error' : undefined}
                onChange={(event) => {
                  nameRef.current = event.target.value;
                  failedName.current = null;
                  setName(event.target.value);
                  clearFeedback('profile');
                }}
              />
              {renderFieldError('profile', 'name')}
              {renderFeedback('profile')}
            </div>
            <dl className="space-y-1">
              <dt className="text-sm font-medium">{t('settings.account.emailAddress')}</dt>
              <dd className="break-all text-sm text-muted-foreground">{String(user?.email ?? '')}</dd>
            </dl>
          </div>
        </div>
      </section>
      {capabilityState !== 'ready' && (
        <div className="space-y-3 rounded-2xl border bg-card px-5 py-3 text-xs leading-relaxed sm:px-6">
          {capabilityState === 'loading' && <output className="text-muted-foreground">{t('settings.account.checkingSecurity')}</output>}
          {capabilityState === 'error' && (
            <div className="space-y-2">
              <p role="alert" className="text-destructive">
                {t('settings.account.securityError')}
              </p>
              <Button type="button" variant="outline" onClick={() => setCapabilityAttempt((value) => value + 1)}>
                {t('common.retry')}
              </Button>
            </div>
          )}
        </div>
      )}
      <div className="grid gap-6">
        <section aria-labelledby="password-heading" className="rounded-2xl border bg-card p-5 sm:p-6">
          <h3 id="password-heading" className="text-base font-semibold">
            {t('settings.account.changePassword')}
          </h3>
          <p className="mt-2 text-sm text-muted-foreground">{t('ui.settings.passwordHint')}</p>
          <form onSubmit={submitPassword} className="mt-5 max-w-lg space-y-4" aria-labelledby="password-heading">
            <div className="space-y-2">
              <label htmlFor="account-current-password" className="text-sm font-medium">
                {t('settings.account.currentPassword')}
              </label>
              <Input
                id="account-current-password"
                name="oldPassword"
                type="password"
                autoComplete="current-password"
                required
                value={oldPassword}
                disabled={pending === 'password'}
                aria-invalid={!!fieldError('password', 'oldPassword')}
                aria-describedby={fieldError('password', 'oldPassword') ? 'password-oldPassword-error' : undefined}
                onChange={(event) => {
                  setOldPassword(event.target.value);
                  clearFeedback('password');
                }}
              />
              {renderFieldError('password', 'oldPassword')}
            </div>
            <div className="space-y-4">
              <div className="space-y-2">
                <label htmlFor="account-new-password" className="text-sm font-medium">
                  {t('settings.account.newPassword')}
                </label>
                <Input
                  id="account-new-password"
                  name="password"
                  type="password"
                  autoComplete="new-password"
                  required
                  value={password}
                  disabled={pending === 'password'}
                  aria-invalid={!!fieldError('password', 'password')}
                  aria-describedby={fieldError('password', 'password') ? 'password-password-error' : undefined}
                  onChange={(event) => {
                    setPassword(event.target.value);
                    clearFeedback('password');
                  }}
                />
                {renderFieldError('password', 'password')}
              </div>
              <div className="space-y-2">
                <label htmlFor="account-confirm-password" className="text-sm font-medium">
                  {t('settings.account.confirmNewPassword')}
                </label>
                <Input
                  id="account-confirm-password"
                  name="passwordConfirm"
                  type="password"
                  autoComplete="new-password"
                  required
                  value={passwordConfirm}
                  disabled={pending === 'password'}
                  aria-invalid={!!fieldError('password', 'passwordConfirm')}
                  aria-describedby={fieldError('password', 'passwordConfirm') ? 'password-passwordConfirm-error' : undefined}
                  onChange={(event) => {
                    setPasswordConfirm(event.target.value);
                    clearFeedback('password');
                  }}
                />
                {renderFieldError('password', 'passwordConfirm')}
              </div>
            </div>
            {renderFeedback('password')}
            <Button type="submit" disabled={!!pending || capabilityState !== 'ready' || !password || !passwordConfirm || !oldPassword}>
              {pending === 'password' ? t('common.saving') : t('settings.account.updatePassword')}
            </Button>
          </form>
        </section>
        <AccountDataSection />
        <section aria-labelledby="licenses-heading" className="rounded-2xl border bg-card p-5 sm:p-6">
          <h3 id="licenses-heading" className="text-base font-semibold">
            {t('licenses.title')}
          </h3>
          <p className="mt-2 text-xs text-muted-foreground">{t('licenses.description')}</p>
          <Button asChild variant="outline" className="mt-4">
            <Link to="/licenses">{t('licenses.title')}</Link>
          </Button>
        </section>
      </div>
    </div>
  );
}

function AccountDataSection() {
  const { t } = useI18n();
  const [pending, setPending] = useState<'export' | 'import' | null>(null);
  const [error, setError] = useState<TranslationKey | null>(null);
  const [success, setSuccess] = useState<TranslationKey | null>(null);
  const [summary, setSummary] = useState<string>();
  const fileRef = useRef<HTMLInputElement>(null);

  const exportData = async () => {
    if (pending) {
      return;
    }
    setPending('export');
    setError(null);
    setSuccess(null);
    setSummary(undefined);
    try {
      const blob = await api.account.export();
      const url = URL.createObjectURL(blob);
      const link = document.createElement('a');
      link.href = url;
      link.download = `viyu-export-${new Date().toISOString().slice(0, 10)}.json`;
      link.click();
      URL.revokeObjectURL(url);
      setSuccess('settings.account.data.exported');
    } catch {
      setError('settings.account.data.error');
    } finally {
      setPending(null);
    }
  };

  const importData = async (file: File) => {
    if (pending) {
      return;
    }
    setPending('import');
    setError(null);
    setSuccess(null);
    setSummary(undefined);
    try {
      const counts = await api.account.import(await file.text());
      setSuccess('settings.account.data.imported');
      setSummary(
        t('settings.account.data.importSummary', {
          profiles: counts.profiles,
          subscriptions: counts.subscriptions,
          reactions: counts.reactions,
          history: counts.history,
        }),
      );
    } catch {
      setError('settings.account.data.error');
    } finally {
      setPending(null);
      if (fileRef.current) {
        fileRef.current.value = '';
      }
    }
  };

  return (
    <section aria-labelledby="account-data-heading" className="rounded-2xl border bg-card p-5 sm:p-6">
      <h3 id="account-data-heading" className="text-base font-semibold">
        {t('settings.account.data.title')}
      </h3>
      <p className="mt-2 text-xs text-muted-foreground">{t('settings.account.data.description')}</p>
      <p className="mt-2 text-xs text-muted-foreground">{t('settings.account.data.security')}</p>
      <div className="mt-4 flex flex-wrap gap-2">
        <Button type="button" variant="outline" disabled={!!pending} onClick={() => void exportData()}>
          <Download aria-hidden="true" className="size-4" />
          {pending === 'export' ? t('common.loading') : t('settings.account.data.export')}
        </Button>
        <Button type="button" variant="outline" disabled={!!pending} onClick={() => fileRef.current?.click()}>
          <Upload aria-hidden="true" className="size-4" />
          {pending === 'import' ? t('common.loading') : t('settings.account.data.import')}
        </Button>
        <input
          ref={fileRef}
          type="file"
          accept="application/json,.json"
          className="hidden"
          onChange={(event) => {
            const file = event.target.files?.[0];
            if (file) {
              void importData(file);
            }
          }}
        />
      </div>
      <div aria-live="polite" className="mt-3 text-sm">
        {error && (
          <p role="alert" className="text-destructive">
            {t(error)}
          </p>
        )}
        {success && <p className="text-muted-foreground">{t(success)}</p>}
        {summary && <p className="text-muted-foreground">{summary}</p>}
      </div>
    </section>
  );
}
