import { Link } from '@tanstack/react-router';
import { type FormEvent, useEffect, useState } from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { adminApi } from '@/lib/admin-api';
import { ApiError } from '@/lib/api-error';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';
import type { AdminRole, AdminUser } from '@/types';
import { adminErrorKey } from './admin-error';

interface AdminUserFormProps {
  user?: AdminUser;
  currentUserId: string;
  onSaved: (user: AdminUser) => void;
  onCancel: () => void;
  onForbidden: () => void;
}

export function AdminUserForm({ user, currentUserId, onSaved, onCancel, onForbidden }: AdminUserFormProps) {
  const { t } = useI18n();
  const [name, setName] = useState(user?.name ?? '');
  const [email, setEmail] = useState(user?.email ?? '');
  const [role, setRole] = useState<AdminRole>(user?.role ?? 'user');
  const [password, setPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<TranslationKey>();
  const self = user?.id === currentUserId;

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !pending) {
        onCancel();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => document.removeEventListener('keydown', onKeyDown);
  }, [pending, onCancel]);

  const submit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (pending) {
      return;
    }
    setError(undefined);
    if (!self && password !== passwordConfirm) {
      setError('admin.passwordMismatch');
      return;
    }
    setPending(true);
    try {
      const updated = user
        ? await adminApi.updateUser(user.id, {
            name: name.trim(),
            ...(!self ? { email: email.trim(), role, ...(password ? { password, passwordConfirm } : {}) } : {}),
          })
        : await adminApi.createUser({ name: name.trim(), email: email.trim(), role, password, passwordConfirm });
      setPassword('');
      setPasswordConfirm('');
      onSaved(updated);
    } catch (cause) {
      if (cause instanceof ApiError && cause.status === 403) {
        onForbidden();
      } else {
        setError(adminErrorKey(cause));
      }
    } finally {
      setPending(false);
    }
  };

  return (
    /* biome-ignore lint/a11y/noStaticElementInteractions: Backdrop click dismisses the dialog without affecting its contents. */
    <div
      className="fixed inset-0 z-50 flex items-center justify-center overflow-y-auto bg-background/80 p-4 backdrop-blur-sm"
      role="presentation"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !pending) {
          onCancel();
        }
      }}
    >
      <section
        role="dialog"
        aria-modal="true"
        className="max-h-[calc(100vh-2rem)] w-full max-w-2xl overflow-y-auto rounded-2xl border bg-card p-5 shadow-xl"
        aria-labelledby="admin-user-form-title"
      >
        <h2 id="admin-user-form-title" className="mb-4 text-lg font-semibold">
          {t(user ? 'admin.editUser' : 'admin.createUser')}
        </h2>
        <form onSubmit={(event) => void submit(event)} className="space-y-4">
          {self && (
            <p className="text-sm text-muted-foreground">
              {t('admin.ownCredentials')}{' '}
              <Link to="/settings" className="text-primary underline">
                {t('admin.openSettings')}
              </Link>
            </p>
          )}
          <fieldset disabled={pending} className="grid gap-4 sm:grid-cols-2">
            <div className="space-y-2">
              <label htmlFor="admin-user-name" className="text-sm font-medium">
                {t('admin.name')}
              </label>
              <Input id="admin-user-name" value={name} onChange={(event) => setName(event.target.value)} autoComplete="off" autoFocus />
            </div>
            <div className="space-y-2">
              <label htmlFor="admin-user-email" className="text-sm font-medium">
                {t('admin.email')}
              </label>
              <Input
                id="admin-user-email"
                type="email"
                value={email}
                onChange={(event) => setEmail(event.target.value)}
                autoComplete="off"
                disabled={self}
                required
              />
            </div>
            <div className="space-y-2 sm:col-span-2">
              <label htmlFor="admin-user-role" className="text-sm font-medium">
                {t('admin.role')}
              </label>
              <Select value={role} onValueChange={(value) => setRole(value as AdminRole)} disabled={self}>
                <SelectTrigger id="admin-user-role">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="user">{t('admin.roleUser')}</SelectItem>
                  <SelectItem value="admin">{t('admin.roleAdmin')}</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <label htmlFor="admin-user-password" className="text-sm font-medium">
                {t('admin.password')}
              </label>
              <Input
                id="admin-user-password"
                type="password"
                value={password}
                onChange={(event) => setPassword(event.target.value)}
                autoComplete="new-password"
                disabled={self}
                required={!user}
                minLength={8}
                aria-describedby={user && !self ? 'admin-password-help' : undefined}
              />
            </div>
            <div className="space-y-2">
              <label htmlFor="admin-user-password-confirm" className="text-sm font-medium">
                {t('admin.passwordConfirm')}
              </label>
              <Input
                id="admin-user-password-confirm"
                type="password"
                value={passwordConfirm}
                onChange={(event) => setPasswordConfirm(event.target.value)}
                autoComplete="new-password"
                disabled={self}
                required={!user || !!password}
                minLength={8}
              />
            </div>
            {user && !self && (
              <p id="admin-password-help" className="text-sm text-muted-foreground sm:col-span-2">
                {t('admin.passwordHelp')}
              </p>
            )}
          </fieldset>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {t(error)}
            </p>
          )}
          <div className="flex gap-2">
            <Button type="submit" disabled={pending}>
              {t(pending ? 'admin.saving' : 'admin.save')}
            </Button>
            <Button type="button" variant="ghost" disabled={pending} onClick={onCancel}>
              {t('admin.cancel')}
            </Button>
          </div>
        </form>
      </section>
    </div>
  );
}
