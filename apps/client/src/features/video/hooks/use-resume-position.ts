import { useCallback, useEffect, useRef, useState } from 'react';
import { playbackPositionKey } from '@/features/video/utils/playback-position';
import { useVideoHistory } from '@/hooks/useVideoStatuses';
import { useAuth } from '@/providers/auth-provider';
import type { VideoResult, WatchHistoryRecord } from '@/types';

/**
 * Where playback of a video starts, decided once its details and the profile's history row are known: a start time shared
 * in the link wins, then the position this device remembered if it is newer than the history row, then the row's own
 * position. A stream has nothing to resume: it always starts at its live edge.
 */
export function useResumePosition({ profileId, videoId, startAt, video }: { profileId: string; videoId: string; startAt?: number; video: VideoResult | null }) {
  const { user } = useAuth();
  const { row, isReady, isError } = useVideoHistory(videoId);
  /** The position to seek to when the media is ready; 0 once applied. */
  const resumeAt = useRef(0);
  /** The history row as it was when the position was decided: the baseline the playback checkpoints add their watch time to. */
  const initialRow = useRef<WatchHistoryRecord | undefined>(undefined);
  const initialized = useRef(false);
  const [resumePosition, setResumePosition] = useState(0);
  const [ready, setReady] = useState(false);

  useEffect(() => {
    if (!user || !video || (!isReady && !isError) || initialized.current) {
      return;
    }
    initialRow.current = row;
    let position = Math.max(0, row?.position || 0);
    try {
      const saved = JSON.parse(localStorage.getItem(playbackPositionKey(user.id, profileId, videoId)) ?? 'null');
      if (
        saved &&
        Number.isFinite(saved.position) &&
        saved.position >= 0 &&
        Number.isFinite(saved.savedAt) &&
        saved.savedAt > (Date.parse(row?.watched_at ?? '') || 0)
      ) {
        position = saved.position;
      }
    } catch {
      // Server history remains available if local storage is unavailable or invalid.
    }
    if (startAt != null) {
      position = startAt;
    }
    if (video.live) {
      position = 0;
    }
    resumeAt.current = position;
    setResumePosition(position);
    initialized.current = true;
    setReady(true);
  }, [user, profileId, videoId, video, row, isReady, isError, startAt]);

  /** Asks for playback to continue from `seconds` (a retried stream, a seek before the media is ready). */
  const moveTo = useCallback((seconds: number) => {
    resumeAt.current = seconds;
    setResumePosition(seconds);
  }, []);

  /** Seeks a media whose metadata just loaded to the pending position, once. */
  const applyTo = useCallback((media: HTMLVideoElement) => {
    if (resumeAt.current > 0 && Number.isFinite(media.duration)) {
      media.currentTime = Math.min(resumeAt.current, Math.max(0, media.duration - 0.1));
      resumeAt.current = 0;
    }
  }, []);

  return { resumeAt, resumePosition, ready, initialRow, moveTo, applyTo };
}
