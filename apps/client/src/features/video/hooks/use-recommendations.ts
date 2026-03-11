import { useState } from 'react';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import type { TranslationKey } from '@/providers/i18n-provider';
import type { VideoResult } from '@/types';

const NONE: VideoResult[] = [];

/** The server's recommendations for a video, loaded once for everything on the player page that shows them. */
export function useRecommendations(profileId: string, videoId: string, enabled = true) {
  const key = `${profileId}:${videoId}`;
  const [result, setResult] = useState<{ key: string; videos: VideoResult[] }>();
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<TranslationKey>();
  const [attempt, setAttempt] = useState(0);

  useAbortableEffect(
    (signal) => {
      if (!enabled) {
        setLoading(false);
        setError(undefined);
        return;
      }
      setLoading(true);
      setError(undefined);
      return api
        .recommendations(profileId, videoId, { signal })
        .then((results) => {
          if (!signal.aborted) {
            setResult({ key, videos: results });
            setLoading(false);
          }
        })
        .catch((cause: unknown) => {
          if (!signal.aborted) {
            setError(requestError(cause));
            setLoading(false);
          }
        });
    },
    [key, enabled, attempt],
  );

  // Recommendations of another video never show for this one while its own load is in flight.
  return { videos: result?.key === key ? result.videos : NONE, loading, error, retry: () => setAttempt((value) => value + 1) };
}
