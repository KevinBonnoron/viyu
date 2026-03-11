import { useState } from 'react';

/** First candidate that has not failed to load; a failing URL is skipped, never retried. */
export function useThumbnail(candidates: string[]) {
  const [failed, setFailed] = useState<string[]>([]);
  const src = candidates.find((candidate) => !failed.includes(candidate));
  return {
    src,
    onError: () => {
      if (src) {
        setFailed((previous) => [...previous, src]);
      }
    },
  };
}
