import { createFileRoute } from '@tanstack/react-router';
import { type FormEvent, useState } from 'react';
import { AuthLayout } from '@/components/auth-layout';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { connectToServer } from '@/lib/server';
import { ServerConfigError } from '@/lib/server-url';
import { useI18n } from '@/providers/i18n-provider';

export const Route = createFileRoute('/setup')({ component: SetupPage });

function SetupPage() {
  const { t } = useI18n();
  const [value, setValue] = useState('');
  const [error, setError] = useState<ServerConfigError['code']>();
  const [pending, setPending] = useState(false);
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    setPending(true);
    setError(undefined);
    try {
      await connectToServer(value);
    } catch (reason) {
      setError(reason instanceof ServerConfigError ? reason.code : 'unreachable');
      setPending(false);
    }
  };
  return (
    <AuthLayout title={t('server.title')} description={t('server.description')}>
      <form onSubmit={submit} className="space-y-4">
        <div>
          <label htmlFor="server-url" className="mb-1 block text-sm font-medium">
            {t('server.url')}
          </label>
          <Input
            id="server-url"
            type="url"
            inputMode="url"
            autoCapitalize="none"
            autoCorrect="off"
            spellCheck={false}
            placeholder="https://viyu.example.com"
            value={value}
            onChange={(event) => setValue(event.target.value)}
            required
          />
        </div>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {t(`server.errors.${error}`)}
          </p>
        )}
        <Button type="submit" className="w-full" disabled={pending}>
          {pending ? t('common.loading') : t('server.connect')}
        </Button>
      </form>
    </AuthLayout>
  );
}
