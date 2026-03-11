import { type RefObject, useCallback, useEffect, useLayoutEffect, useRef } from 'react';
import { watchHistoryCollection } from '@/collections/watch-history.collection';
import { nextDurationWatched, reachedWatched, sampledSeconds } from '@/features/video/utils/playback-checkpoint';
import { playbackPositionKey } from '@/features/video/utils/playback-position';
import { pb } from '@/lib/pocketbase';
import { insertHistory } from '@/lib/watch-history';
import { useAuth } from '@/providers/auth-provider';
import { usePlaybackPrivacy } from '@/providers/playback-privacy-provider';
import type { WatchHistoryRecord } from '@/types';

/** A checkpoint is written this often while the video plays, and at every pause, seek and end. */
const SAVE_INTERVAL_MS = 5000;

/** What the playback has earned so far: `played` is the watch time of this session, in seconds. */
export interface PlaybackProgress {
  played: number;
  mediaTime: number;
  wallTime: number;
  active: boolean;
}

/** What this session knows of the history row it writes to. */
interface Tracker {
  /** The row's id; empty until it exists. */
  id: string;
  /** The `duration_watched` the row holds. */
  stored: number;
  videoDuration: number;
  /** How many of this session's played seconds `stored` already contains. */
  savedPlayed: number;
}

interface Checkpoint {
  watchedAt: string;
  /** The resume position (0 once the video ended). */
  position: number;
  /** How far the media got: the duration once the video ended, else the current time. */
  reached: number;
  played: number;
  duration: number;
}

/**
 * Persists what the profile watched: the history row of the video (position, duration watched, series) and the position
 * this device remembers, and measures the watch time it adds. Nothing is written for a stream, in private playback, while
 * the history is held (a profile that did not choose this video) or before the resume position was applied.
 *
 * Writes go through the history collection and are serialised; only the last save on `pagehide` is an independent request
 * that survives the page (see `sendFinal`).
 */
export function usePlaybackCheckpoint({
  videoRef,
  profileId,
  videoId,
  seriesId,
  live,
  holdHistory,
  resumeAt,
  resumeReady,
  initialRow,
}: {
  videoRef: RefObject<HTMLVideoElement | null>;
  profileId: string;
  videoId: string;
  seriesId?: string;
  /** Whether the video is a stream; `undefined` while its details are unknown. */
  live: boolean | undefined;
  holdHistory: boolean;
  resumeAt: RefObject<number>;
  resumeReady: boolean;
  initialRow: RefObject<WatchHistoryRecord | undefined>;
}) {
  const { user } = useAuth();
  const { privatePlayback, privatePlaybackRef, privacyVersion } = usePlaybackPrivacy();
  // True until the viewer presses play on a video they did not choose to watch (see PlayerScreen): nothing is recorded meanwhile.
  const historyHeld = useRef(holdHistory);
  const loadedVideo = useRef<HTMLVideoElement | null>(null);
  const progress = useRef<PlaybackProgress>({ played: 0, mediaTime: 0, wallTime: 0, active: false });
  const lastSave = useRef(0);
  const chain = useRef<Promise<void>>(Promise.resolve());
  const tracker = useRef<Tracker | null>(null);
  /** Set by the final save: nothing queued behind it may overwrite it. */
  const closed = useRef(false);
  const context = useRef({ userId: user?.id, profileId, videoId, seriesId, live, resumeReady });
  context.current = { userId: user?.id, profileId, videoId, seriesId, live, resumeReady };

  useLayoutEffect(() => {
    // A mode change must not count private time in the next public sample.
    progress.current.mediaTime = videoRef.current?.currentTime ?? 0;
    progress.current.wallTime = performance.now();
    lastSave.current = Date.now();
  }, [privatePlayback, videoRef]);

  const trackerOf = useCallback((): Tracker => {
    tracker.current ??= {
      id: initialRow.current?.id ?? '',
      stored: initialRow.current?.duration_watched ?? 0,
      videoDuration: initialRow.current?.video_duration ?? 0,
      savedPlayed: 0,
    };
    return tracker.current;
  }, [initialRow]);

  /** Writes the checkpoint to the history row: updates the row it knows, or creates it (and updates the row another device created first). */
  const save = useCallback(
    async (checkpoint: Checkpoint, version: number) => {
      if (closed.current || privatePlaybackRef.current || privacyVersion.current !== version) {
        return;
      }
      const { profileId, videoId, seriesId } = context.current;
      const state = trackerOf();
      const videoDuration = checkpoint.duration > 0 ? checkpoint.duration : state.videoDuration;
      let durationWatched = nextDurationWatched(state.stored, state.savedPlayed, checkpoint.played, videoDuration);
      let watched = reachedWatched(checkpoint.reached, durationWatched, videoDuration);
      const update = () =>
        watchHistoryCollection.update(state.id, (draft) => {
          draft.watched_at = checkpoint.watchedAt;
          draft.duration_watched = durationWatched;
          draft.position = checkpoint.position;
          if (watched) {
            draft.watched = true;
          }
          if (videoDuration > 0) {
            draft.video_duration = videoDuration;
          }
          if ((draft.series ?? '') !== (seriesId ?? '')) {
            draft.series = seriesId ?? '';
          }
        }).isPersisted.promise;
      if (state.id) {
        await update();
      } else {
        const inserted = await insertHistory({
          profile: profileId,
          video: videoId,
          ...(seriesId ? { series: seriesId } : {}),
          watched_at: checkpoint.watchedAt,
          duration_watched: durationWatched,
          video_duration: videoDuration,
          position: checkpoint.position,
          watched,
        });
        if ('created' in inserted) {
          state.id = inserted.created;
        } else {
          // Another device (or tab) created the row first: its watch time is the one this session adds to.
          state.id = inserted.existing.id;
          state.stored = inserted.existing.duration_watched;
          durationWatched = nextDurationWatched(state.stored, state.savedPlayed, checkpoint.played, videoDuration);
          watched = reachedWatched(checkpoint.reached, durationWatched, videoDuration);
          await update();
        }
      }
      state.stored = durationWatched;
      state.videoDuration = videoDuration;
      state.savedPlayed = Math.max(state.savedPlayed, checkpoint.played);
    },
    [privatePlaybackRef, privacyVersion, trackerOf],
  );

  /**
   * The save of a page being closed. A normal request (the SDK's included) is cancelled when the page unloads, and one
   * waiting behind a save in flight would never leave: this one is independent and `keepalive`, so the browser finishes
   * it. It updates the row this session knows, or creates one under a client-made id (the unique `(profile, video)` index
   * refuses a duplicate with a 400, which is harmless here: the row already exists).
   */
  const sendFinal = useCallback(
    (checkpoint: Checkpoint) => {
      const { profileId, videoId, seriesId } = context.current;
      const state = trackerOf();
      const videoDuration = checkpoint.duration > 0 ? checkpoint.duration : state.videoDuration;
      const durationWatched = nextDurationWatched(state.stored, state.savedPlayed, checkpoint.played, videoDuration);
      const watched = reachedWatched(checkpoint.reached, durationWatched, videoDuration);
      const fields = {
        profile: profileId,
        video: videoId,
        series: seriesId ?? '',
        watched_at: checkpoint.watchedAt,
        duration_watched: durationWatched,
        video_duration: videoDuration,
        position: checkpoint.position,
        // An existing row is only ever raised to watched, never lowered.
        ...(watched ? { watched } : {}),
      };
      const id = state.id;
      const path = `/api/collections/watch_history/records${id ? `/${encodeURIComponent(id)}` : ''}`;
      void fetch(pb.buildURL(path), {
        method: id ? 'PATCH' : 'POST',
        keepalive: true,
        headers: { Authorization: `Bearer ${pb.authStore.token}`, 'Content-Type': 'application/json' },
        body: JSON.stringify(id ? fields : { id: watchHistoryCollection.utils.newId(), ...fields, watched }),
      }).catch(() => {
        // The page is going away: nothing can retry, and the last periodic checkpoint is on the server already.
      });
      state.stored = durationWatched;
      state.videoDuration = videoDuration;
      state.savedPlayed = Math.max(state.savedPlayed, checkpoint.played);
      closed.current = true;
    },
    [trackerOf],
  );

  const persist = useCallback(
    ({ ended = false, final = false }: { ended?: boolean; final?: boolean } = {}) => {
      const video = videoRef.current ?? loadedVideo.current;
      const { userId, profileId, videoId, live, resumeReady } = context.current;
      // A live has no duration to measure progress against and nothing to resume: it leaves no history entry.
      if (
        privatePlaybackRef.current ||
        historyHeld.current ||
        !userId ||
        live !== false ||
        !video ||
        video.readyState === 0 ||
        !resumeReady ||
        resumeAt.current > 0
      ) {
        return;
      }
      lastSave.current = Date.now();
      const position = ended || video.ended ? 0 : Math.max(0, video.currentTime);
      try {
        localStorage.setItem(playbackPositionKey(userId, profileId, videoId), JSON.stringify({ position, savedAt: Date.now() }));
      } catch {
        // The authenticated server save still works without local storage.
      }
      const checkpoint: Checkpoint = {
        watchedAt: new Date().toISOString(),
        position,
        reached: ended || video.ended ? (Number.isFinite(video.duration) ? video.duration : 0) : Math.max(0, video.currentTime),
        played: progress.current.played,
        duration: Number.isFinite(video.duration) ? video.duration : 0,
      };
      if (final) {
        sendFinal(checkpoint);
        return;
      }
      const version = privacyVersion.current;
      chain.current = chain.current
        .then(() => save(checkpoint, version))
        .catch(() => {
          // Keep playback uninterrupted; the next periodic boundary retries.
        });
    },
    [videoRef, privatePlaybackRef, privacyVersion, resumeAt, save, sendFinal],
  );

  useEffect(() => {
    const unload = () => persist({ final: true });
    // A page restored from the back/forward cache is alive again: its saves resume.
    const restore = (event: PageTransitionEvent) => {
      if (event.persisted) {
        closed.current = false;
      }
    };
    window.addEventListener('pagehide', unload);
    window.addEventListener('pageshow', restore);
    return () => {
      window.removeEventListener('pagehide', unload);
      window.removeEventListener('pageshow', restore);
      persist();
    };
  }, [persist]);

  /** Adds what played since the last sample to the watch time; `includePlaybackBoundary` counts it though playback was not marked active (a play or seek just ended). */
  const sample = useCallback(
    (includePlaybackBoundary = false) => {
      const video = videoRef.current;
      if (!video) {
        return;
      }
      const now = performance.now();
      const state = progress.current;
      if ((state.active || includePlaybackBoundary) && !video.seeking && !privatePlaybackRef.current) {
        state.played += sampledSeconds(video.currentTime - state.mediaTime, (now - state.wallTime) / 1000, video.playbackRate);
      }
      state.mediaTime = video.currentTime;
      state.wallTime = now;
    },
    [videoRef, privatePlaybackRef],
  );

  /** Starts counting from the current position, as after a seek. */
  const resetSample = useCallback(() => {
    progress.current.active = false;
    progress.current.mediaTime = videoRef.current?.currentTime ?? 0;
    progress.current.wallTime = performance.now();
  }, [videoRef]);

  return {
    progress,
    sample,
    resetSample,
    /** Playback is running: from now on its time is counted, and the viewer has chosen to watch, so the history is no longer held. */
    activate: useCallback(() => {
      historyHeld.current = false;
      progress.current.active = true;
    }, []),
    /** Playback stopped or is buffering: its time is not counted until `activate`. */
    deactivate: useCallback(() => {
      progress.current.active = false;
    }, []),
    /** The viewer pressed play: the history is no longer held. */
    releaseHold: useCallback(() => {
      historyHeld.current = false;
    }, []),
    persist,
    /** Writes a checkpoint when the last one is older than the save interval. */
    persistIfDue: useCallback(() => {
      if (Date.now() - lastSave.current >= SAVE_INTERVAL_MS) {
        persist();
      }
    }, [persist]),
    /** Resolves when the checkpoints asked for so far are written. */
    whenSaved: useCallback(() => chain.current, []),
    /** The media element that played: its position is still read after React detached it. */
    noteLoaded: useCallback(() => {
      loadedVideo.current = videoRef.current;
    }, [videoRef]),
  };
}
