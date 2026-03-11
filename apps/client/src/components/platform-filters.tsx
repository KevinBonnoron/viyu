import { PlatformBadge } from '@/components/platform-badge';
import { Button } from '@/components/ui/button';
import { platformName } from '@/lib/platforms';
import { useI18n } from '@/providers/i18n-provider';

export function PlatformFilters({ platforms, selected, onChange }: { platforms: string[]; selected?: string; onChange: (platform?: string) => void }) {
  const { t } = useI18n();
  if (platforms.length < 2) {
    return null;
  }
  return (
    <section aria-label={t('platform.filter')} className="flex flex-wrap items-center gap-2">
      <span className="mr-1 text-xs font-medium text-muted-foreground">{t('platform.filter')}</span>
      <Button
        variant="outline"
        aria-pressed={!selected}
        onClick={() => onChange()}
        className="h-8 rounded-full px-3 text-xs aria-pressed:border-primary/40 aria-pressed:bg-accent"
      >
        {t('platform.all')}
      </Button>
      {platforms.map((platform) => (
        <Button
          key={platform}
          variant="outline"
          aria-pressed={selected === platform}
          onClick={() => onChange(platform)}
          className="h-8 gap-1.5 rounded-full px-3 text-xs aria-pressed:border-primary/40 aria-pressed:bg-accent"
        >
          <PlatformBadge platform={platform} decorative className="size-5 border-0 bg-transparent shadow-none" />
          {platformName(platform)}
        </Button>
      ))}
    </section>
  );
}
