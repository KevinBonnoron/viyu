import type { VideoResult } from '@/types';
import { ApiError } from './api-error';

type Outcome = { video: VideoResult } | { error: unknown };
type Listener = (outcome: Outcome) => void;
type Load = (id: string, options: { source?: string; signal: AbortSignal }) => Promise<VideoResult>;
type Job = {
  key: string;
  id: string;
  source?: string;
  controller: AbortController;
  listeners: Set<Listener>;
  failures: number;
  readyAt: number;
};

function temporary(error: unknown): boolean {
  return !(error instanceof ApiError) || error.status === 408 || error.status === 429 || error.status >= 500;
}

/** Visible cards share four requests at most. Transient failures stay pending and retry with a bounded
 * delay; rate limits pause the whole queue rather than letting every card exhaust the same allowance. */
export function createVideoMetadataQueue(load: Load) {
  const jobs = new Map<string, Job>();
  const queued: Job[] = [];
  let running = 0;
  let pausedUntil = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;

  function drain() {
    clearTimeout(timer);
    timer = undefined;
    const now = Date.now();
    while (running < 4 && now >= pausedUntil) {
      const index = queued.findIndex((job) => job.readyAt <= now);
      if (index === -1) {
        break;
      }
      const [job] = queued.splice(index, 1);
      running++;
      const signal = AbortSignal.any([job.controller.signal, AbortSignal.timeout(45000)]);
      void load(job.id, { source: job.source, signal })
        .then((video) => {
          if (!job.controller.signal.aborted) {
            for (const listener of job.listeners) {
              listener({ video });
            }
          }
        })
        .catch((error: unknown) => {
          if (job.controller.signal.aborted) {
            return;
          }
          if (temporary(error)) {
            const delay = Math.max(error instanceof ApiError ? error.retryAfterMs : 0, Math.min(300000, 30000 * 2 ** Math.min(job.failures++, 4)));
            job.readyAt = Date.now() + delay;
            if (error instanceof ApiError && (error.status === 429 || error.code === 'SOURCE_RATE_LIMITED')) {
              pausedUntil = Math.max(pausedUntil, job.readyAt);
            }
            queued.push(job);
          } else {
            for (const listener of job.listeners) {
              listener({ error });
            }
          }
        })
        .finally(() => {
          running--;
          if (!queued.includes(job) && jobs.get(job.key) === job) {
            jobs.delete(job.key);
          }
          drain();
        });
    }
    if (queued.length && running < 4) {
      const next = Math.max(pausedUntil, Math.min(...queued.map((job) => job.readyAt)));
      timer = setTimeout(drain, Math.max(0, next - Date.now()));
    }
  }

  return function subscribe(key: string, id: string, source: string | undefined, listener: Listener) {
    let job = jobs.get(key);
    if (!job || job.controller.signal.aborted) {
      job = { key, id, source, controller: new AbortController(), listeners: new Set(), failures: 0, readyAt: 0 };
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
          jobs.delete(job.key);
        }
        const index = queued.indexOf(job);
        if (index !== -1) {
          queued.splice(index, 1);
        }
        drain();
      }
    };
  };
}
