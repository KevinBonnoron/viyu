import { CircleAlert, RotateCcw } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/providers/i18n-provider';

export function ProfilesError({ onRetry }: { onRetry: () => void }) {
  const { t } = useI18n();
  return (
    <div role="alert" className="flex flex-wrap items-center gap-3 rounded-xl border bg-card px-4 py-3 text-sm">
      <CircleAlert aria-hidden="true" className="size-4 shrink-0 text-destructive" />
      <span>{t('profiles.loadError')}</span>
      <Button type="button" variant="outline" size="sm" onClick={onRetry}>
        <RotateCcw aria-hidden="true" />
        {t('common.retry')}
      </Button>
    </div>
  );
}
