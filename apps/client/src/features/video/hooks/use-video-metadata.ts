import { useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import type { VideoResult } from '@/types';

type Outcome = { video: VideoResult } | { error: true };
type Listener = (outcome: Outcome) => void;
type Job = { key: string; id: string; source?: string; controller: AbortController; listeners: Set<Listener> };
const jobs = new Map<string, Job>();
const queued: Job[] = [];
let running = 0;

function drain() {
  while (running < 4 && queued.length) {
    const job = queued.shift();
    if (!job || job.controller.signal.aborted || job.listeners.size === 0) {
      continue;
    }
    running++;
    const signal = AbortSignal.any([job.controller.signal, AbortSignal.timeout(45000)]);
    void api.videos
      .get(job.id, { source: job.source, signal })
      .then((video) => {
        if (!job.controller.signal.aborted) {
          for (const listener of job.listeners) {
            listener({ video });
          }
        }
      })
      .catch(() => {
        if (!job.controller.signal.aborted) {
          for (const listener of job.listeners) {
            listener({ error: true });
          }
        }
      })
      .finally(() => {
        running--;
        if (jobs.get(job.key) === job) {
          jobs.delete(job.key);
        }
        drain();
      });
  }
}

function subscribe(key: string, id: string, source: string | undefined, listener: Listener) {
  let job = jobs.get(key);
  if (!job || job.controller.signal.aborted) {
    job = { key, id, source, controller: new AbortController(), listeners: new Set() };
    jobs.set(key, job);
    queued.push(job);
  }
  job.listeners.add(listener);
  drain();
  return () => {
    job.listeners.delete(listener);
    if (job.listeners.size === 0) {
      job.controller.abort();
      if (jobs.get(job.key) === job) {
        jobs.delete(key);
      }
    }
  };
}

export function useVideoMetadata<T extends HTMLElement = HTMLElement>(video: VideoResult, enabled: boolean, owner: string) {
  const ref = useRef<T>(null);
  const [visible, setVisible] = useState(false);
  const [metadata, setMetadata] = useState<VideoResult>();
  const [failed, setFailed] = useState(false);
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
    if (!enabled || !visible || metadata || failed) {
      return;
    }
    return subscribe(key, video.id, video.source, (outcome) => {
      if ('video' in outcome) {
        setMetadata(outcome.video);
      } else {
        setFailed(true);
      }
    });
  }, [enabled, visible, metadata, failed, key, video.id, video.source, attempt]);
  return {
    ref,
    title: metadata ? metadata.originalTitle || metadata.title : undefined,
    thumbnail: metadata?.thumbnail,
    thumbnails: metadata?.thumbnails,
    published: metadata?.published,
    failed,
    retry: () => {
      setFailed(false);
      setAttempt((value) => value + 1);
    },
  };
}
