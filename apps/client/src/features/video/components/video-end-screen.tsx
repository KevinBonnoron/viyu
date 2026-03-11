import { Play, RotateCcw, Video } from 'lucide-react';
import { useEffect, useRef, useState } from 'react';
import { Button } from '@/components/ui/button';
import { useThumbnail } from '@/features/video/hooks/use-thumbnail';
import { displayTitle, thumbnailCandidates } from '@/features/video/utils/video-presentation';
import { useUserSettings } from '@/hooks/useUserSettings';
import { VideoLink } from '@/lib/links';
import { formatDuration } from '@/lib/utils';
import { useI18n } from '@/providers/i18n-provider';
import type { VideoResult } from '@/types';

/** Seconds the next episode waits before it starts by itself. */
export const NEXT_EPISODE_COUNTDOWN = 8;

/** The overlay's frame, the look of the player error overlay: a dark layer over the video, content centered above the control bar. */
function Overlay({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <section
      aria-label={label}
      className="@container/end absolute inset-0 flex flex-col items-center overflow-y-auto bg-black/90 px-3 pb-16 pt-3 text-white sm:px-6 sm:pb-20 sm:pt-6"
    >
      {children}
    </section>
  );
}

/** Puts the keyboard on the primary action when the overlay appears, unless the viewer is busy elsewhere on the page. */
function useInitialFocus(ref: React.RefObject<HTMLElement | null>) {
  useEffect(() => {
    const target = ref.current;
    const active = document.activeElement;
    const frame = target?.closest('.player-frame');
    if (target && (!active || active === document.body || frame?.contains(active))) {
      target.focus({ preventScroll: true });
    }
  }, [ref]);
}

/** The next episode of the series: a card with a countdown (when autoplay is on), "Play now", and "Cancel" to stop the countdown. */
export function NextEpisodeCard({
  title,
  countdown,
  onPlay,
  onReplay,
}: {
  title: string;
  /** Seconds to wait before playing by itself, or `null` when the viewer decides (autoplay is off). */
  countdown: number | null;
  onPlay: () => void;
  onReplay: () => void;
}) {
  const { t } = useI18n();
  const [remaining, setRemaining] = useState(countdown);
  const playRef = useRef<HTMLButtonElement>(null);
  const latestPlay = useRef(onPlay);
  useInitialFocus(playRef);

  useEffect(() => {
    latestPlay.current = onPlay;
  }, [onPlay]);

  useEffect(() => {
    if (remaining === null) {
      return;
    }
    if (remaining <= 0) {
      latestPlay.current();
      return;
    }
    const timer = window.setTimeout(() => setRemaining(remaining - 1), 1000);
    return () => window.clearTimeout(timer);
  }, [remaining]);

  return (
    <Overlay label={t('player.end.title')}>
      <div className="my-auto w-full max-w-md shrink-0 space-y-3 text-center sm:space-y-4">
        <h2 className="line-clamp-3 text-sm font-semibold sm:text-lg">{t('player.end.next', { title })}</h2>
        {remaining !== null && (
          <p className="text-xs tabular-nums text-white/70 sm:text-sm" aria-hidden="true">
            {t('player.end.countdown', { seconds: Math.max(remaining, 0) })}
          </p>
        )}
        <div className="flex flex-wrap items-center justify-center gap-2">
          <Button ref={playRef} size="sm" onClick={onPlay}>
            <Play aria-hidden="true" className="size-4" />
            {t('player.end.playNow')}
          </Button>
          {remaining !== null ? (
            <Button size="sm" variant="secondary" onClick={() => setRemaining(null)}>
              {t('common.cancel')}
            </Button>
          ) : (
            <Button size="sm" variant="secondary" onClick={onReplay}>
              <RotateCcw aria-hidden="true" className="size-4" />
              {t('player.end.replay')}
            </Button>
          )}
        </div>
      </div>
    </Overlay>
  );
}

function SuggestionCard({ video }: { video: VideoResult }) {
  const { t } = useI18n();
  const { settings } = useUserSettings();
  const title = displayTitle(video, settings?.use_original_titles ?? true);
  const thumbnail = useThumbnail(thumbnailCandidates(video));
  return (
    <VideoLink video={video} className="group block min-w-0 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-white">
      <div className="relative aspect-video overflow-hidden rounded-lg border border-white/15 bg-white/10 transition-colors group-hover:border-white/50 group-focus-visible:border-white/50">
        {thumbnail.src ? (
          <img src={thumbnail.src} alt="" decoding="async" className="size-full object-cover" onError={thumbnail.onError} />
        ) : (
          <div role="img" aria-label={t('video.noThumbnail')} className="flex size-full items-center justify-center text-white/50">
            <Video aria-hidden="true" className="size-6" />
          </div>
        )}
        {video.duration > 0 && (
          <span className="absolute bottom-1 right-1 rounded bg-black/80 px-1 py-0.5 text-[10px] font-medium tabular-nums @5xl/end:text-xs">
            {formatDuration(video.duration)}
          </span>
        )}
      </div>
      <p className="mt-1.5 line-clamp-2 text-left text-xs font-medium leading-snug sm:text-sm @5xl/end:text-base">{title}</p>
    </VideoLink>
  );
}

/** No next episode: "Replay" and a few recommended videos to open. */
export function SuggestionsCard({ videos, onReplay }: { videos: readonly VideoResult[]; onReplay: () => void }) {
  const { t } = useI18n();
  const replayRef = useRef<HTMLButtonElement>(null);
  useInitialFocus(replayRef);
  return (
    <Overlay label={t('player.end.title')}>
      <div className="my-auto w-full max-w-6xl shrink-0 space-y-3 text-center sm:space-y-4">
        <Button ref={replayRef} size="sm" onClick={onReplay}>
          <RotateCcw aria-hidden="true" className="size-4" />
          {t('player.end.replay')}
        </Button>
        {videos.length > 0 && (
          <>
            <h2 className="text-xs font-semibold uppercase tracking-[0.1em] text-white/70">{t('player.end.suggestions')}</h2>
            <div className="grid grid-cols-2 gap-2 sm:gap-3 @5xl/end:grid-cols-4">
              {videos.map((video) => (
                <SuggestionCard key={`${video.platform}:${video.source ?? 'public'}:${video.id}`} video={video} />
              ))}
            </div>
          </>
        )}
      </div>
    </Overlay>
  );
}
