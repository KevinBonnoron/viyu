import { createFileRoute, Link, redirect, useNavigate } from '@tanstack/react-router';
import { useEffect, useState } from 'react';
import { AuthLayout } from '@/components/auth-layout';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { request } from '@/lib/api';
import { pb } from '@/lib/pocketbase';
import { useAuth } from '@/providers/auth-provider';
import { type TranslationKey, useI18n } from '@/providers/i18n-provider';

export const Route = createFileRoute('/register')({
  beforeLoad: () => {
    if (pb.authStore.isValid) {
      throw redirect({ to: '/' });
    }
  },
  component: RegisterPage,
});

function RegisterPage() {
  const { t } = useI18n();
  const { register } = useAuth();
  const navigate = useNavigate();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [passwordConfirm, setPasswordConfirm] = useState('');
  const [error, setError] = useState<TranslationKey>();
  const [pending, setPending] = useState(false);
  const [enabled, setEnabled] = useState<boolean>();
  useEffect(() => {
    const controller = new AbortController();
    void request<{ enabled: boolean }>('/registration', { signal: controller.signal, cache: 'no-store' })
      .then((result) => setEnabled(result.enabled))
      .catch(() => setEnabled(undefined));
    return () => controller.abort();
  }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (pending) {
      return;
    }
    setError(undefined);
    if (password !== passwordConfirm) {
      setError('auth.register.passwordMismatch');
      return;
    }
    setPending(true);
    try {
      await register(email, password, passwordConfirm);
      navigate({ to: '/' });
    } catch {
      setError('auth.register.failed');
    } finally {
      setPending(false);
    }
  };

  return (
    <AuthLayout title={t('auth.register.title')} description={t('ui.auth.registerDescription')}>
      {enabled === false ? (
        <output className="block rounded-lg border bg-muted/30 p-4 text-sm text-muted-foreground">{t('auth.register.disabled')}</output>
      ) : (
        <form onSubmit={handleSubmit} className="space-y-4">
          {error && (
            <p role="alert" className="rounded-lg border border-destructive/20 bg-destructive/5 p-3 text-sm text-destructive">
              {t(error)}
            </p>
          )}
          <div>
            <label htmlFor="email" className="block text-sm font-medium mb-1">
              {t('auth.email')}
            </label>
            <Input id="email" type="email" autoComplete="username" value={email} onChange={(e) => setEmail(e.target.value)} required />
          </div>
          <div>
            <label htmlFor="password" className="block text-sm font-medium mb-1">
              {t('auth.password')}
            </label>
            <Input id="password" type="password" autoComplete="new-password" value={password} onChange={(e) => setPassword(e.target.value)} required />
          </div>
          <div>
            <label htmlFor="passwordConfirm" className="block text-sm font-medium mb-1">
              {t('auth.confirmPassword')}
            </label>
            <Input
              id="passwordConfirm"
              type="password"
              autoComplete="new-password"
              value={passwordConfirm}
              onChange={(e) => setPasswordConfirm(e.target.value)}
              required
            />
          </div>
          <Button type="submit" disabled={pending} className="w-full">
            {pending ? t('common.loading') : t('auth.register.submit')}
          </Button>
        </form>
      )}
      <p className="mt-6 text-sm text-muted-foreground">
        {t('auth.register.hasAccount')}{' '}
        <Link to="/login" className="text-primary hover:underline">
          {t('auth.login.submit')}
        </Link>
      </p>
    </AuthLayout>
  );
}
