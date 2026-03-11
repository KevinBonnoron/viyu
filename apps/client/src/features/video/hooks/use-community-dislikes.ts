import { useEffect, useState } from 'react';

export function useCommunityDislikes(videoId: string | undefined): number | null {
  const [result, setResult] = useState<{ videoId: string; count: number } | null>(null);

  useEffect(() => {
    setResult(null);
    if (!videoId) {
      return;
    }
    const controller = new AbortController();
    void fetch(`https://returnyoutubedislikeapi.com/votes?${new URLSearchParams({ videoId })}`, {
      signal: controller.signal,
      credentials: 'omit',
      headers: { Accept: 'application/json' },
    })
      .then(async (response) => {
        if (!response.ok) {
          return;
        }
        const body: unknown = await response.json();
        if (
          !controller.signal.aborted &&
          body &&
          typeof body === 'object' &&
          'id' in body &&
          body.id === videoId &&
          'dislikes' in body &&
          typeof body.dislikes === 'number' &&
          Number.isSafeInteger(body.dislikes) &&
          body.dislikes >= 0 &&
          (!('deleted' in body) || body.deleted !== true)
        ) {
          setResult({ videoId, count: body.dislikes });
        }
      })
      .catch(() => {
        // Counts are optional. Unavailable, invalid and rate-limited responses stay unknown.
      });
    return () => controller.abort();
  }, [videoId]);

  return result && result.videoId === videoId ? result.count : null;
}
