import { useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { createVideoMetadataQueue } from '@/lib/video-metadata';
import type { VideoResult } from '@/types';

const subscribe = createVideoMetadataQueue((id, options) => api.videos.get(id, options));

export function useVideoMetadata<T extends HTMLElement = HTMLElement>(video: VideoResult, enabled: boolean, owner: string) {
  const ref = useRef<T>(null);
  const [visible, setVisible] = useState(false);
  const [metadata, setMetadata] = useState<VideoResult>();
  const [failure, setFailure] = useState<{ error: unknown }>();
  const [attempt, setAttempt] = useState(0);
  const key = `${video.source ? owner : 'public'}:${video.source || 'public'}:${video.id}`;
  useEffect(() => {
    if (!enabled || !ref.current) {
      return;
    }
    const observer = new IntersectionObserver((entries) => setVisible(entries.some((entry) => entry.isIntersecting)), { rootMargin: '300px' });
    observer.observe(ref.current);
    return () => observer.disconnect();
  }, [enabled]);
  useEffect(() => {
    if (!enabled || !visible || metadata || failure) {
      return;
    }
    return subscribe(key, video.id, video.source, (outcome) => {
      if ('video' in outcome) {
        setMetadata(outcome.video);
      } else {
        setFailure(outcome);
      }
    });
  }, [enabled, visible, metadata, failure, key, video.id, video.source, attempt]);
  return {
    ref,
    title: metadata ? metadata.originalTitle || metadata.title : undefined,
    thumbnail: metadata?.thumbnail,
    thumbnails: metadata?.thumbnails,
    published: metadata?.published,
    complete: !!metadata,
    failed: !!failure,
    error: failure?.error,
    retry: () => {
      setFailure(undefined);
      setAttempt((value) => value + 1);
    },
  };
}
