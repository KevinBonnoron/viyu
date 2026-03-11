import { useEffect, useState } from 'react';
import type { SponsorSegment } from '@/types';

function parseSegments(body: unknown): SponsorSegment[] {
  if (!Array.isArray(body)) {
    return [];
  }
  const segments = new Map<string, SponsorSegment>();
  for (const item of body) {
    if (
      !item ||
      typeof item !== 'object' ||
      item.category !== 'sponsor' ||
      item.actionType !== 'skip' ||
      !('segment' in item) ||
      !Array.isArray(item.segment) ||
      item.segment.length !== 2
    ) {
      continue;
    }
    const [start, end] = item.segment;
    if (typeof start !== 'number' || typeof end !== 'number' || !Number.isFinite(start) || !Number.isFinite(end) || start < 0 || end <= start) {
      continue;
    }
    segments.set(`${start}:${end}`, { start, end });
  }
  return [...segments.values()].sort((left, right) => left.start - right.start || left.end - right.end);
}

export function useSponsorSegments(videoId: string | undefined): SponsorSegment[] {
  const [segments, setSegments] = useState<SponsorSegment[]>([]);

  useEffect(() => {
    setSegments([]);
    if (!videoId) {
      return;
    }
    const controller = new AbortController();
    const parameters = new URLSearchParams({ videoID: videoId, categories: JSON.stringify(['sponsor']), actionTypes: JSON.stringify(['skip']) });
    void fetch(`https://sponsor.ajay.app/api/skipSegments?${parameters}`, { signal: controller.signal, headers: { Accept: 'application/json' } })
      .then(async (response) => {
        if (!response.ok) {
          return [];
        }
        return parseSegments(await response.json());
      })
      .then((items) => {
        if (!controller.signal.aborted) {
          setSegments(items);
        }
      })
      .catch(() => {
        // SponsorBlock is optional; network and response failures never block playback.
      });
    return () => controller.abort();
  }, [videoId]);

  return segments;
}
