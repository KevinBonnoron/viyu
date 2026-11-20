import { KeepAwake } from '@capacitor-community/keep-awake';
import { type RefObject, useEffect } from 'react';

// Keep-awake requests run one after another, whichever player made them: a release must not overtake the acquisition
// it undoes, or that acquisition, finishing late, would keep the screen on after playback paused or the player left.
let requests: Promise<unknown> = Promise.resolve();
function enqueue(request: () => Promise<unknown>) {
  requests = requests.then(request).catch(() => undefined);
}

/**
 * Keeps the screen on while the video plays, as a video app does; it may sleep again once playback pauses or ends,
 * or the player goes away. The Android app holds its window awake natively (WebViews have no Screen Wake Lock API);
 * a browser takes a screen wake lock, which it refuses while the page is hidden: playback that started then takes it
 * when the page becomes visible. Where neither exists (desktop webviews, older browsers) the screen follows the
 * device's settings. `version` is the stream version: the video element is made again with it.
 */
export function useKeepAwake(videoRef: RefObject<HTMLVideoElement | null>, version: number) {
  useEffect(() => (videoRef.current ? holdScreenWhilePlaying(videoRef.current) : undefined), [videoRef, version]);
}

/** Holds the screen awake while `video` plays; the returned function stops following it and lets the screen sleep. */
export function holdScreenWhilePlaying(video: HTMLVideoElement): () => void {
  let awake = false;
  let generation = 0;
  const keep = () => {
    if (awake) {
      return;
    }
    awake = true;
    const acquisition = ++generation;
    enqueue(() =>
      KeepAwake.keepAwake().catch(() => {
        if (generation === acquisition) {
          awake = false;
        }
      }),
    );
  };
  const allow = () => {
    if (!awake) {
      return;
    }
    awake = false;
    enqueue(() => KeepAwake.allowSleep());
  };
  const retry = () => {
    if (document.visibilityState === 'visible' && !video.paused && !video.ended) {
      keep();
    }
  };
  video.addEventListener('playing', keep);
  for (const event of ['pause', 'ended', 'emptied']) {
    video.addEventListener(event, allow);
  }
  document.addEventListener('visibilitychange', retry);
  if (!video.paused && !video.ended) {
    keep();
  }
  return () => {
    video.removeEventListener('playing', keep);
    for (const event of ['pause', 'ended', 'emptied']) {
      video.removeEventListener(event, allow);
    }
    document.removeEventListener('visibilitychange', retry);
    allow();
  };
}
