import { Moon, Sun } from 'lucide-react';
import { useI18n } from '@/providers/i18n-provider';
import { useTheme } from '@/providers/theme-provider';

/**
 * Switching between light and dark. It describes the theme in effect, so with the device's own setting ("system", chosen in
 * Settings → Appearance) it shows what the device gives and toggling picks the other one.
 */
export function useThemeToggle() {
  const { t } = useI18n();
  const { theme, setTheme } = useTheme();
  const dark = theme === 'dark' || (theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches);
  return {
    dark,
    Icon: dark ? Moon : Sun,
    label: `${t('settings.appearance.theme')} : ${t(dark ? 'settings.appearance.dark' : 'settings.appearance.light')}`,
    toggle: () => setTheme(dark ? 'light' : 'dark'),
  };
}
