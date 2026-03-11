import { Link } from '@tanstack/react-router';
import { ListVideo, Users, Video } from 'lucide-react';
import { EmptyState } from '@/components/empty-state';
import { PageTitle } from '@/components/page-title';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/providers/i18n-provider';

/** What a link to a record that does not exist (anymore) shows: a neutral explanation and a way back. */
export function RecordNotFound({ kind }: { kind: 'video' | 'channel' | 'series' }) {
  const { t } = useI18n();
  const title = t(`notFound.${kind}.title`);
  return (
    <div className="space-y-6">
      <PageTitle>{title}</PageTitle>
      <EmptyState icon={kind === 'video' ? Video : kind === 'channel' ? Users : ListVideo} title={title} description={t(`notFound.${kind}.description`)}>
        <Button asChild variant="outline">
          {kind === 'series' ? (
            <Link to="/subscriptions" search={{ tab: 'series' }}>
              {t('notFound.series.back')}
            </Link>
          ) : (
            <Link to="/">{t('notFound.home')}</Link>
          )}
        </Button>
      </EmptyState>
    </div>
  );
}
