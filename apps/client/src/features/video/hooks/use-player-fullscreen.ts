import { Capacitor } from '@capacitor/core';
import { ScreenOrientation } from '@capacitor/screen-orientation';
import { type RefObject, useCallback, useEffect, useRef, useState } from 'react';
import { useMediaQuery } from '@/hooks/useMediaQuery';

/** A touch screen without hover (phones, tablets): the player uses its touch layout there. */
export const TOUCH_PLAYER_QUERY = '(pointer: coarse) and (hover: none)';
/** A phone held sideways: a touch screen in landscape, too short to show the page around the video. */
const PHONE_LANDSCAPE_QUERY = `${TOUCH_PLAYER_QUERY} and (orientation: landscape) and (max-height: 600px)`;

/**
 * The player's full screen. The button and the `f` key toggle the Fullscreen API on the frame; on a touch screen entering
 * it also locks the screen in landscape (unless the video is vertical), and leaving it unlocks the screen.
 *
 * A phone turned sideways shows the video alone, as YouTube does, and turning it upright again returns to the page with the
 * video above its sections. Browsers only grant the Fullscreen API to a tap, so the sideways view is `immersive`: the frame
 * covers the window by CSS (the Android app has no system bars, so that is the whole screen), and on the web the first tap
 * in it upgrades it to the real full screen. Leaving the sideways view (button, back, Escape) keeps the page until the phone
 * has been turned upright once.
 */
export function usePlayerFullscreen(frameRef: RefObject<HTMLElement | null>, videoRef: RefObject<HTMLVideoElement | null>) {
  const touch = useMediaQuery(TOUCH_PLAYER_QUERY);
  const phoneLandscape = useMediaQuery(PHONE_LANDSCAPE_QUERY);
  const [native, setNative] = useState(false);
  const [dismissed, setDismissed] = useState(false);
  const locked = useRef(false);
  const immersive = phoneLandscape && !dismissed && !native;

  useEffect(() => {
    const update = () => {
      const active = document.fullscreenElement === frameRef.current;
      setNative(active);
      if (active) {
        return;
      }
      if (locked.current) {
        locked.current = false;
        void ScreenOrientation.unlock().catch(() => undefined);
      }
      if (window.matchMedia(PHONE_LANDSCAPE_QUERY).matches) {
        setDismissed(true);
      }
    };
    document.addEventListener('fullscreenchange', update);
    return () => document.removeEventListener('fullscreenchange', update);
  }, [frameRef]);

  useEffect(() => {
    if (phoneLandscape) {
      return;
    }
    setDismissed(false);
    if (document.fullscreenElement === frameRef.current && !locked.current) {
      void document.exitFullscreen().catch(() => undefined);
    }
  }, [phoneLandscape, frameRef]);

  useEffect(() => {
    const frame = frameRef.current;
    if (!immersive || !frame) {
      return;
    }
    const root = document.documentElement;
    const overflow = root.style.overflow;
    root.style.overflow = 'hidden';
    // A tap on the video itself (which shows the controls), not on a control: the exit button must not upgrade.
    const upgrade = (event: MouseEvent) => {
      if (event.target instanceof Element && event.target.closest('button, input, a, [role="menu"], [data-chapters]')) {
        return;
      }
      if (!document.fullscreenElement) {
        void frame.requestFullscreen({ navigationUI: 'hide' }).catch(() => undefined);
      }
    };
    const dismissOnEscape = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !event.defaultPrevented) {
        setDismissed(true);
      }
    };
    // The Android app already has the whole screen; the WebView's own full screen would only re-host the frame.
    const web = !Capacitor.isNativePlatform();
    if (web) {
      frame.addEventListener('click', upgrade);
    }
    window.addEventListener('keydown', dismissOnEscape);
    return () => {
      root.style.overflow = overflow;
      frame.removeEventListener('click', upgrade);
      window.removeEventListener('keydown', dismissOnEscape);
    };
  }, [immersive, frameRef]);

  const toggle = useCallback(async () => {
    const frame = frameRef.current;
    if (!frame) {
      return;
    }
    if (document.fullscreenElement) {
      await document.exitFullscreen();
      return;
    }
    if (immersive) {
      setDismissed(true);
      return;
    }
    await frame.requestFullscreen({ navigationUI: 'hide' });
    const video = videoRef.current;
    if (touch && (!video || video.videoWidth >= video.videoHeight)) {
      try {
        await ScreenOrientation.lock({ orientation: 'landscape' });
        locked.current = true;
      } catch {
        // No orientation lock here (iOS, some browsers): the full screen follows the phone.
      }
    }
  }, [frameRef, videoRef, immersive, touch]);

  return { fullscreen: native || immersive, immersive, touch, toggle };
}
