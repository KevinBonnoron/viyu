import { useEffect } from 'react';
import { profileTheme } from '@/lib/profile-appearance';
import { useProfiles } from '@/providers/profiles-provider';
import { DEFAULT_ACCENT, useTheme } from '@/providers/theme-provider';

/** Applies the active profile's palette to the whole application; a profile without one gets the default. */
export function ProfileTheme() {
  const { activeProfile, isReady } = useProfiles();
  const { setAccent } = useTheme();
  const theme = profileTheme(activeProfile?.theme);
  useEffect(() => {
    // Until profiles are loaded the remembered accent stays, so a reload does not flash the default.
    if (isReady) {
      setAccent(theme ?? DEFAULT_ACCENT);
    }
  }, [isReady, theme, setAccent]);
  return null;
}
