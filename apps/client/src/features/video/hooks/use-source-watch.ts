import { type RefObject, useCallback, useEffect, useRef, useState } from 'react';
import type { PlaybackProgress } from '@/features/video/hooks/use-playback-checkpoint';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import type { TranslationKey } from '@/providers/i18n-provider';
import { usePlaybackPrivacy } from '@/providers/playback-privacy-provider';

export type WatchStatus = 'waiting' | 'marking' | 'marked' | 'failed';

/** Seconds of playback after which a video counts as watched on the connected source (or its whole length, if shorter). */
const SOURCE_WATCH_SECONDS = 30;

/**
 * Marks the video watched on the connected source it is read through, once enough of it was played. `submit()` is cheap
 * to call at every sample: it does nothing until the threshold is reached, while marking, or after a failure (which only a
 * manual `submit(true)` retries).
 */
export function useSourceWatch({
  source,
  videoId,
  enabled,
  videoRef,
  progress,
  persist,
}: {
  source?: string;
  videoId: string;
  /** Whether the source is connected and can record history. */
  enabled: boolean;
  videoRef: RefObject<HTMLVideoElement | null>;
  progress: RefObject<PlaybackProgress>;
  /** Writes the playback checkpoint, so the local history agrees with what is sent to the source. */
  persist: () => void;
}) {
  const { privatePlaybackRef } = usePlaybackPrivacy();
  const state = useRef<WatchStatus>('waiting');
  const mounted = useRef(true);
  const [status, setStatus] = useState<WatchStatus>('waiting');
  const [error, setError] = useState<TranslationKey | null>(null);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const submit = useCallback(
    async (manual = false) => {
      const duration = videoRef.current?.duration;
      if (privatePlaybackRef.current || !source || !enabled || !duration || !Number.isFinite(duration) || duration <= 0) {
        return;
      }
      if (state.current === 'marking' || state.current === 'marked' || (state.current === 'failed' && !manual)) {
        return;
      }
      const threshold = Math.min(SOURCE_WATCH_SECONDS, duration);
      if (progress.current.played + 0.000001 < threshold) {
        return;
      }
      state.current = 'marking';
      setStatus('marking');
      setError(null);
      persist();
      try {
        // Round only sub-microsecond floating-point accumulation at the boundary.
        await api.sources.markWatched(source, videoId, Math.max(threshold, progress.current.played), duration);
        if (!mounted.current) {
          return;
        }
        state.current = 'marked';
        setStatus('marked');
      } catch (failure) {
        if (!mounted.current) {
          return;
        }
        state.current = 'failed';
        setStatus('failed');
        setError(requestError(failure));
      }
    },
    [source, videoId, enabled, videoRef, progress, persist, privatePlaybackRef],
  );

  return { status, error, submit };
}
