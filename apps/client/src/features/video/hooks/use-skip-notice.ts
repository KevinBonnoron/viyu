import { type RefObject, useCallback, useEffect, useRef, useState } from 'react';
import type { SponsorSegment } from '@/types';

/** How long the notice stays once the video is back, so it can be read even when playback resumed instantly. */
export const SKIP_NOTICE_LINGER_MS = 2500;

const HAVE_CURRENT_DATA = 2;
const HAVE_FUTURE_DATA = 3;

/** The media events after which a skip's seek may have finished loading. */
const SETTLE_EVENTS = ['seeked', 'loadeddata', 'canplay', 'playing', 'pause'] as const;

type SkipNoticeVideo = Pick<HTMLVideoElement, 'addEventListener' | 'removeEventListener' | 'seeking' | 'paused' | 'readyState'>;

export interface SkipNoticeFollower {
  /** Raises the notice, or restarts its lifetime when it is already up. */
  show: () => void;
  /** Stops following the video; the notice is left as it is. */
  dispose: () => void;
}

/**
 * Follows the notice that tells an advertisement was skipped. `show` (called once the skip's seek was requested)
 * raises it and keeps it up for as long as the seek loads, however long that takes; once the video can show the new
 * position (a paused video needs the frame, a playing one enough data to carry on), it stays `linger` ms more and goes.
 * Another `show` starts the lifetime again; a replaced media source (`emptied`) clears it. `dispose` stops following.
 */
export function followSkipNotice(
  video: SkipNoticeVideo & EventTarget,
  onChange: (visible: boolean) => void,
  linger = SKIP_NOTICE_LINGER_MS,
): SkipNoticeFollower {
  let waiting = false;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const stopTimer = () => {
    clearTimeout(timer);
    timer = undefined;
  };
  const settle = () => {
    if (!waiting || video.seeking || video.readyState < (video.paused ? HAVE_CURRENT_DATA : HAVE_FUTURE_DATA)) {
      return;
    }
    waiting = false;
    stopTimer();
    timer = setTimeout(() => {
      timer = undefined;
      onChange(false);
    }, linger);
  };
  const clear = () => {
    waiting = false;
    stopTimer();
    onChange(false);
  };
  for (const event of SETTLE_EVENTS) {
    video.addEventListener(event, settle);
  }
  video.addEventListener('emptied', clear);
  return {
    show() {
      waiting = true;
      stopTimer();
      onChange(true);
    },
    dispose() {
      stopTimer();
      for (const event of SETTLE_EVENTS) {
        video.removeEventListener(event, settle);
      }
      video.removeEventListener('emptied', clear);
    },
  };
}

type SkipVideo = Pick<HTMLVideoElement, 'currentTime' | 'duration'>;

/** Only a strictly forward seek counts as a skip. Otherwise an overlong segment can seek in place at media end forever. */
export function skipSegment(
  video: SkipVideo,
  segment: Pick<SponsorSegment, 'end'>,
  pending: { current: boolean },
  clearActive: (segment: null) => void,
  notice: Pick<SkipNoticeFollower, 'show'> | null,
): boolean {
  if (pending.current) {
    return false;
  }
  const target = Number.isFinite(video.duration) ? Math.min(segment.end, video.duration) : segment.end;
  if (!(target > video.currentTime)) {
    return false;
  }
  pending.current = true;
  clearActive(null);
  video.currentTime = target;
  notice?.show();
  return true;
}

/**
 * Seeks forward past the segment, clears the active segment and raises its notice. `pending` belongs to the player,
 * which clears it on seeked, stream replacement and position re-reads. The video element changes with `version`,
 * which also clears the notice. Declare this before effects that may skip so the notice follows the new element first.
 */
export function useSponsorSkip({
  videoRef,
  version,
  pending,
  clearActive,
}: {
  videoRef: RefObject<HTMLVideoElement | null>;
  version: number;
  pending: RefObject<boolean>;
  clearActive: (segment: null) => void;
}) {
  const [noticeVisible, setNoticeVisible] = useState(false);
  const follower = useRef<SkipNoticeFollower | null>(null);

  useEffect(() => {
    const video = videoRef.current;
    if (!video) {
      return;
    }
    const current = followSkipNotice(video, setNoticeVisible);
    follower.current = current;
    return () => {
      current.dispose();
      follower.current = null;
      setNoticeVisible(false);
    };
  }, [videoRef, version]);

  const skip = useCallback(
    (segment: SponsorSegment) => {
      const video = videoRef.current;
      if (video) {
        skipSegment(video, segment, pending, clearActive, follower.current);
      }
    },
    [videoRef, pending, clearActive],
  );

  return { noticeVisible, skip };
}
