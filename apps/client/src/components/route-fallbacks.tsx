import { type ErrorComponentProps, Link } from '@tanstack/react-router';
import { CircleAlert, Compass } from 'lucide-react';
import { EmptyState } from '@/components/empty-state';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/providers/i18n-provider';

/** What an address no route matches shows: a neutral explanation and a way back. */
export function RouteNotFound() {
  const { t } = useI18n();
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-2xl items-center p-6">
      <EmptyState icon={Compass} title={t('notFound.page.title')} description={t('notFound.page.description')} className="w-full">
        <Button asChild variant="outline">
          <Link to="/">{t('notFound.home')}</Link>
        </Button>
      </EmptyState>
    </main>
  );
}

/** What a page that crashed while rendering shows, instead of a blank screen: the app stays usable and the page can be retried. */
export function RouteError({ reset }: ErrorComponentProps) {
  const { t } = useI18n();
  return (
    <main className="mx-auto flex min-h-dvh w-full max-w-2xl items-center p-6">
      <EmptyState icon={CircleAlert} title={t('errors.page.title')} description={t('errors.page.description')} className="w-full">
        <Button onClick={reset}>{t('common.retry')}</Button>
        <Button asChild variant="outline">
          <Link to="/">{t('notFound.home')}</Link>
        </Button>
      </EmptyState>
    </main>
  );
}
