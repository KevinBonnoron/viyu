import { profileIcon, profileTheme } from '@/lib/profile-appearance';
import { cn } from '@/lib/utils';
import { DEFAULT_ACCENT } from '@/providers/theme-provider';
import type { ProfileRecord } from '@/types';

/** A profile's icon in its own palette, whichever profile is active; without a theme it uses the default palette. */
export function ProfileAvatar({ profile, className }: { profile: Pick<ProfileRecord, 'icon' | 'theme'> | null | undefined; className?: string }) {
  const Icon = profileIcon(profile?.icon);
  return (
    <span
      aria-hidden="true"
      data-accent={profileTheme(profile?.theme) ?? DEFAULT_ACCENT}
      className={cn('flex size-8 shrink-0 items-center justify-center rounded-full bg-primary/15 text-primary', className)}
    >
      <Icon className="size-1/2" />
    </span>
  );
}
