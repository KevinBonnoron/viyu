import { SkipForward } from 'lucide-react';
import { cn } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';

/**
 * Confirms in the player frame that an advertisement was skipped. The polite status region is always in the page, so a
 * screen reader announces what appears in it; it never takes a tap. `visible` is false over an error or the end screen.
 * Inline it sits at the top of the frame (below the corner buttons on a phone); in fullscreen it sits below the top bar,
 * where a long title runs.
 */
export function SponsorSkipNotice({ visible, fullscreen }: { visible: boolean; fullscreen: boolean }) {
  const { t } = useI18n();
  return (
    <div
      role="status"
      aria-live="polite"
      className={cn('pointer-events-none absolute inset-x-3 z-10 flex justify-center', fullscreen ? 'top-16' : 'top-14 sm:top-3')}
    >
      {visible && (
        <p className="flex max-w-full items-center gap-1.5 rounded-full bg-black/75 px-3 py-1.5 text-xs font-medium text-white shadow-lg sm:text-sm">
          <SkipForward aria-hidden="true" className="size-3.5 shrink-0 sm:size-4" />
          <span className="truncate">{t('player.sponsor.skipped')}</span>
        </p>
      )}
    </div>
  );
}
