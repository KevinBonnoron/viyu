import { type RefObject, type SyntheticEvent, useCallback, useEffect, useRef, useState } from 'react';

const HMR_KEY = 'viyu.hmr.playback';

/** The paused intent a video starts with: paused when its history is held, else what a Vite full reload handed over (development only), else none. */
function initialPausedIntent(holdHistory: boolean, videoId: string, profileId: string, source: string | undefined): boolean | null {
  if (holdHistory) {
    return true;
  }
  if (!import.meta.hot) {
    return null;
  }
  try {
    const raw = sessionStorage.getItem(HMR_KEY);
    sessionStorage.removeItem(HMR_KEY);
    const saved = JSON.parse(raw ?? 'null');
    return saved?.video === videoId &&
      saved.profile === profileId &&
      saved.source === (source ?? '') &&
      saved.expiresAt > Date.now() &&
      typeof saved.paused === 'boolean'
      ? saved.paused
      : null;
  } catch {
    return null;
  }
}

/**
 * Whether the video starts by itself, and how. The viewer's intent (`pausedIntent`: null until they play or pause) wins over
 * the `autoplay` setting; a profile that did not choose the video starts paused. The `autoplay` attribute alone is not
 * reliable: hls.js attaches the media after the element exists, and a browser may refuse sound without a user gesture. So
 * playback is asked for once the first data is ready; if the browser refuses, it plays muted and offers to turn the sound
 * back on, so the video still starts by itself.
 */
export function useAutoplay({
  videoRef,
  videoId,
  profileId,
  source,
  holdHistory,
  defaultAutoplay,
  streamVersion,
}: {
  videoRef: RefObject<HTMLVideoElement | null>;
  videoId: string;
  profileId: string;
  source?: string;
  holdHistory: boolean;
  defaultAutoplay: boolean;
  streamVersion: number;
}) {
  const [pausedIntent, updatePausedIntent] = useState<boolean | null>(() => initialPausedIntent(holdHistory, videoId, profileId, source));
  const pausedIntentRef = useRef(pausedIntent);
  const autoplayVersion = useRef(-1);
  const [mutedFallbackVersion, setMutedFallbackVersion] = useState<number>();

  const setPlaybackPaused = useCallback((paused: boolean) => {
    pausedIntentRef.current = paused;
    updatePausedIntent(paused);
  }, []);

  useEffect(() => {
    const hot = import.meta.hot;
    if (!hot) {
      return;
    }
    const remember = () => {
      const video = videoRef.current;
      const paused = video && video.readyState > 0 ? video.paused : (pausedIntentRef.current ?? !defaultAutoplay);
      try {
        // Dev-only handoff, consumed once after Vite's full reload; no position.
        sessionStorage.setItem(HMR_KEY, JSON.stringify({ video: videoId, profile: profileId, source: source ?? '', paused, expiresAt: Date.now() + 60_000 }));
      } catch {
        // Fast Refresh still preserves the in-memory intent without storage.
      }
    };
    hot.on('vite:beforeFullReload', remember);
    return () => hot.off('vite:beforeFullReload', remember);
  }, [videoRef, videoId, profileId, source, defaultAutoplay]);

  const handleCanPlay = (event: SyntheticEvent<HTMLVideoElement>) => {
    const video = event.currentTarget;
    if (autoplayVersion.current === streamVersion) {
      return;
    }
    autoplayVersion.current = streamVersion;
    const wanted = pausedIntentRef.current === null ? defaultAutoplay : !pausedIntentRef.current;
    if (!wanted || !video.paused || video.ended) {
      return;
    }
    video.play().catch((error: unknown) => {
      if (!(error instanceof DOMException) || error.name !== 'NotAllowedError') {
        return;
      }
      video.muted = true;
      video
        .play()
        .then(() => {
          if (videoRef.current === video) {
            setMutedFallbackVersion(streamVersion);
          }
        })
        .catch(() => undefined);
    });
  };

  return {
    /** The value of the video's `autoplay` attribute. */
    autoPlay: pausedIntent === null ? defaultAutoplay : !pausedIntent,
    /** Records that the viewer wants the video playing or paused. */
    setPlaybackPaused,
    handleCanPlay,
    /** The video plays muted because the browser refused sound: the viewer is offered to unmute. */
    mutedFallback: mutedFallbackVersion === streamVersion,
    /** The sound is on again (by the viewer's volume controls). */
    clearMutedFallback: useCallback(() => setMutedFallbackVersion(undefined), []),
    /** Turns the sound back on after the muted fallback. */
    unmute: useCallback(() => {
      const video = videoRef.current;
      if (video) {
        video.muted = false;
        if (video.volume === 0) {
          video.volume = 0.5;
        }
      }
      setMutedFallbackVersion(undefined);
    }, [videoRef]),
  };
}
