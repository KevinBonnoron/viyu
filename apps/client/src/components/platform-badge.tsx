import { Clapperboard, Globe } from 'lucide-react';
import type { ComponentType, SVGProps } from 'react';
import { TwitchIcon, YoutubeIcon } from '@/components/brand-icons';
import { type PlatformIconName, type PlatformTone, platformPresentation } from '@/lib/platforms';
import { cn } from '@/lib/utils';

type IconComponent = ComponentType<SVGProps<SVGSVGElement>>;

const ICONS: Record<PlatformIconName, IconComponent> = {
  youtube: YoutubeIcon,
  twitch: TwitchIcon,
  peertube: Clapperboard,
  generic: Globe,
};

/** The colour of a platform's icon alone. */
const TONE_TEXT: Record<PlatformTone, string> = {
  red: 'text-red-600 dark:text-red-400',
  violet: 'text-violet-600 dark:text-violet-400',
  orange: 'text-orange-600 dark:text-orange-400',
  neutral: 'text-muted-foreground',
};

/** The tinted tile a platform's icon sits on in a card. */
const TONE_TILE: Record<PlatformTone, string> = {
  red: 'bg-red-500/10 text-red-600 dark:text-red-400',
  violet: 'bg-violet-500/10 text-violet-600 dark:text-violet-400',
  orange: 'bg-orange-500/10 text-orange-600 dark:text-orange-400',
  neutral: 'bg-accent text-primary',
};

/** A platform's icon, unframed and undecorated; the caller colours it. */
export function PlatformIcon({ platform, className }: { platform: string; className?: string }) {
  const Icon = ICONS[platformPresentation(platform).icon];
  return <Icon aria-hidden="true" className={className} />;
}

/** The rounded tinted tile of a platform's icon, for a card or a dialog header. */
export function PlatformTile({ platform, className }: { platform: string; className?: string }) {
  return (
    <span className={cn('flex size-10 shrink-0 items-center justify-center rounded-xl', TONE_TILE[platformPresentation(platform).tone], className)}>
      <PlatformIcon platform={platform} className="size-5" />
    </span>
  );
}

export function PlatformBadge({ platform, className, decorative = false }: { platform: string; className?: string; decorative?: boolean }) {
  const { name, tone } = platformPresentation(platform);
  return (
    <span
      role="img"
      aria-label={name}
      aria-hidden={decorative || undefined}
      title={decorative ? undefined : name}
      className={cn(
        'inline-flex size-7 shrink-0 items-center justify-center rounded-md border border-border/60 bg-card/95 shadow-xs',
        TONE_TEXT[tone],
        className,
      )}
    >
      <PlatformIcon platform={platform} className="size-4" />
    </span>
  );
}
