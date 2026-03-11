import { Moon, Sun } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { useI18n } from '@/providers/i18n-provider';
import { useTheme } from '@/providers/theme-provider';

/**
 * Switches between light and dark. It shows the theme in effect, so with the device's own setting ("system", chosen in
 * Settings → Appearance) it shows what the device gives and a click picks the other one.
 */
export function ThemeToggle() {
  const { t } = useI18n();
  const { theme, setTheme } = useTheme();
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  const Icon = dark ? Moon : Sun;
  const current = t(dark ? 'settings.appearance.dark' : 'settings.appearance.light');
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      className="rounded-full"
      aria-label={`${t('settings.appearance.theme')} : ${current}`}
      aria-pressed={dark}
      title={`${t('settings.appearance.theme')} : ${current}`}
      onClick={() => setTheme(dark ? 'light' : 'dark')}
    >
      <Icon aria-hidden="true" className="size-5" />
    </Button>
  );
}
