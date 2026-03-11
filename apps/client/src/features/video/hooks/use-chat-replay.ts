import { type RefObject, useEffect, useRef, useState } from 'react';
import { api } from '@/lib/api';
import { requestError } from '@/lib/api-error';
import { extendBuffer, planReplayFetch, REPLAY_AHEAD, type ReplayBuffer, replayCovers, startBuffer, trimBuffer, visibleMessages } from '@/lib/chat-replay';
import type { TranslationKey } from '@/providers/i18n-provider';
import type { ChatMessage } from '@/types';

/**
 * The chat of a VOD as it was at the playback time of `videoRef`. Pages are fetched one at a time to stay ahead of the playhead,
 * starting over at the new position after a seek, and not at all while the card is not `enabled`.
 */
export function useChatReplay(vodId: string, videoRef: RefObject<HTMLVideoElement | null>, version: number, enabled: boolean) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<TranslationKey>();
  const [retry, setRetry] = useState(0);
  const sync = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    setMessages((current) => (current.length > 0 ? [] : current));
    setLoading(true);
    setError(undefined);
    let buffer: ReplayBuffer | null = null;
    let request: { controller: AbortController; covers: { from: number; until: number } } | null = null;
    let shown: ChatMessage[] = [];
    let failed = false;
    const playbackTime = () => {
      const time = videoRef.current?.currentTime ?? 0;
      return Number.isFinite(time) ? Math.max(0, time) : 0;
    };
    const show = (time: number) => {
      const next = buffer ? visibleMessages(buffer.messages, time) : [];
      if (next.length !== shown.length || next[0] !== shown[0] || next.at(-1) !== shown.at(-1)) {
        shown = next;
        setMessages(next);
      }
    };
    const run = () => {
      if (failed) {
        return;
      }
      const time = playbackTime();
      show(time);
      if (request) {
        if (replayCovers(request.covers, time)) {
          return;
        }
        // A seek made the request in flight useless.
        request.controller.abort();
        request = null;
      }
      const plan = planReplayFetch(buffer, time);
      if (plan.kind === 'none') {
        return;
      }
      const controller = new AbortController();
      const base = buffer;
      if (plan.kind === 'offset') {
        buffer = null;
        show(time);
        setLoading(true);
        request = { controller, covers: { from: plan.offset, until: plan.offset + REPLAY_AHEAD } };
      } else {
        request = { controller, covers: { from: base?.from ?? time, until: base?.until ?? time } };
      }
      void api.chat
        .replay(vodId, plan.kind === 'offset' ? { offset: plan.offset } : { cursor: plan.cursor }, { signal: controller.signal })
        .then((page) => {
          if (controller.signal.aborted) {
            return;
          }
          request = null;
          buffer = plan.kind === 'offset' || !base ? startBuffer(plan.kind === 'offset' ? plan.offset : time, page) : extendBuffer(base, page);
          buffer = trimBuffer(buffer, playbackTime());
          setLoading(false);
          run();
        })
        .catch((cause: unknown) => {
          if (controller.signal.aborted) {
            return;
          }
          request = null;
          failed = true;
          setError(requestError(cause));
          setLoading(false);
        });
    };
    // Deferred a tick so a StrictMode remount cancels it before a request leaves.
    const start = window.setTimeout(() => {
      sync.current = run;
      run();
    }, 0);
    return () => {
      window.clearTimeout(start);
      sync.current = null;
      request?.controller.abort();
    };
  }, [vodId, videoRef, enabled, retry]);

  // The video element is replaced when the stream is restarted, so its listeners follow `version`.
  useEffect(() => {
    const video = videoRef.current;
    if (!enabled || !video) {
      return;
    }
    const update = () => sync.current?.();
    video.addEventListener('timeupdate', update);
    video.addEventListener('seeked', update);
    return () => {
      video.removeEventListener('timeupdate', update);
      video.removeEventListener('seeked', update);
    };
  }, [videoRef, version, enabled]);

  return { messages, loading, error, retry: () => setRetry((value) => value + 1) };
}
