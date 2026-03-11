import { useState } from 'react';
import { useAbortableEffect } from '@/hooks/useAbortableEffect';
import { api, responseError } from '@/lib/api';
import { appendMessages, parseChatMessage } from '@/lib/chat-messages';
import { pb } from '@/lib/pocketbase';
import { delay, readSse, reconnectDelay, type SseEvent } from '@/lib/sse';
import type { ChatMessage } from '@/types';

export type ChatLiveStatus = 'connecting' | 'ready' | 'unavailable';

/** Messages are shown in groups this often, so a busy chat renders a few times a second rather than once per message. */
const FLUSH_MS = 150;

/** How one connection to the chat ended: for good (`unavailable`), or by a break the caller reconnects from (`dropped`). */
type Outcome = 'unavailable' | 'dropped';

/**
 * Opens the chat stream once and reads it to its end. The request is a plain `fetch` with the session's bearer token (the browser's built-in
 * SSE client cannot send one). A refusal that retrying cannot fix (a 4xx: bad channel, no longer allowed; 503: chat not
 * offered for this stream) is final; a 401 also ends the session. Any other failure throws and is retried by the caller.
 */
async function connect(login: string, signal: AbortSignal, onEvent: (event: SseEvent) => void): Promise<Outcome> {
  const response = await fetch(api.chat.live(login), {
    headers: { Authorization: `Bearer ${pb.authStore.token}`, Accept: 'text/event-stream' },
    cache: 'no-store',
    credentials: 'omit',
    signal,
  });
  if (!response.ok) {
    const failure = await responseError(response);
    if ((response.status >= 400 && response.status < 500) || response.status === 503) {
      return 'unavailable';
    }
    throw failure;
  }
  if (!response.body) {
    throw new Error('The chat stream has no body');
  }
  // The server ends the stream after `unavailable`: it is not a drop to reconnect from.
  let unavailable = false;
  await readSse(response.body, signal, (event) => {
    unavailable ||= event.event === 'unavailable';
    onEvent(event);
  });
  return unavailable ? 'unavailable' : 'dropped';
}

/** The chat of a stream in progress, over Server-Sent Events; the connection exists only while `enabled`, and is re-opened with a growing delay when it drops. */
export function useChatLive(login: string, enabled: boolean) {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [status, setStatus] = useState<ChatLiveStatus>('connecting');

  useAbortableEffect(
    async (signal) => {
      if (!enabled) {
        return;
      }
      setMessages((current) => (current.length > 0 ? [] : current));
      setStatus('connecting');
      let pending: ChatMessage[] = [];
      let flush: number | undefined;
      let attempt = 0;
      signal.addEventListener(
        'abort',
        () => {
          if (flush !== undefined) {
            window.clearTimeout(flush);
          }
        },
        { once: true },
      );
      const handle = (event: SseEvent) => {
        if (event.event === 'ready') {
          attempt = 0;
          setStatus('ready');
        } else if (event.event === 'message') {
          const message = parseChatMessage(event.data);
          if (!message) {
            return;
          }
          pending.push(message);
          flush ??= window.setTimeout(() => {
            flush = undefined;
            const batch = pending;
            pending = [];
            setMessages((current) => appendMessages(current, batch));
          }, FLUSH_MS);
        }
      };
      while (!signal.aborted) {
        let outcome: Outcome = 'dropped';
        try {
          outcome = await connect(login, signal, handle);
        } catch {
          // A broken connection (or an aborted one: the loop ends below) is retried.
        }
        if (signal.aborted) {
          return;
        }
        if (outcome === 'unavailable') {
          setStatus('unavailable');
          return;
        }
        setStatus('connecting');
        await delay(reconnectDelay(attempt++, Math.random()), signal);
      }
    },
    [login, enabled],
  );

  return { messages, status };
}
