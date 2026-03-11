import type { ChatMessage, ChatReplayPage } from '../types/chat.type';
import { CHAT_MESSAGE_LIMIT } from './chat-messages';

/** How far ahead of the playhead the buffer is kept filled, in seconds. */
export const REPLAY_AHEAD = 60;
/** A jump back of at most this much stays inside the buffer. */
export const REPLAY_BACK_TOLERANCE = 3;
/** A jump forward past the last buffered message by more than this is a seek: the buffer starts over there. */
export const REPLAY_FORWARD_GAP = 15;
/** Messages older than this behind the playhead are dropped from the buffer. */
export const REPLAY_KEEP_BEHIND = 600;

/** The last `limit` messages shown at `time`: those with `at <= time`. `messages` are sorted by `at`. */
export function visibleMessages(messages: ChatMessage[], time: number, limit = CHAT_MESSAGE_LIMIT): ChatMessage[] {
  let low = 0;
  let high = messages.length;
  while (low < high) {
    const middle = (low + high) >>> 1;
    if ((messages[middle]?.at ?? 0) <= time) {
      low = middle + 1;
    } else {
      high = middle;
    }
  }
  return messages.slice(Math.max(0, low - limit), low);
}

/** The messages fetched so far for the stretch of the VOD that starts at `from`. */
export interface ReplayBuffer {
  /** Sorted by `at`. */
  messages: ChatMessage[];
  /** Playback time the buffer starts at: nothing before it is buffered. */
  from: number;
  /** Playback time up to which everything is buffered (the last message's). */
  until: number;
  /** Cursor of the next page. */
  next?: string;
  /** No page follows: the buffer covers the rest of the VOD. */
  done: boolean;
}

function sortedByTime(messages: ChatMessage[]): ChatMessage[] {
  for (let index = 1; index < messages.length; index += 1) {
    if ((messages[index]?.at ?? 0) < (messages[index - 1]?.at ?? 0)) {
      return [...messages].sort((left, right) => (left.at ?? 0) - (right.at ?? 0));
    }
  }
  return messages;
}

/** Adds a page after the buffer. A page that brings nothing new ends the buffer, so an odd server cannot make it loop. */
export function extendBuffer(buffer: ReplayBuffer, page: ChatReplayPage): ReplayBuffer {
  const timed = page.messages.filter((message) => typeof message.at === 'number' && Number.isFinite(message.at));
  const first = Math.min(...timed.map((message) => message.at ?? 0));
  const seen = new Set<string>();
  for (let index = buffer.messages.length - 1; index >= 0; index -= 1) {
    const message = buffer.messages[index];
    if (!message || (message.at ?? 0) < first) {
      break;
    }
    seen.add(message.id);
  }
  const fresh = timed.filter((message) => {
    if (seen.has(message.id)) {
      return false;
    }
    seen.add(message.id);
    return true;
  });
  if (fresh.length === 0) {
    return { ...buffer, next: undefined, done: true };
  }
  const messages = sortedByTime(buffer.messages.concat(fresh));
  return {
    messages,
    from: buffer.from,
    until: Math.max(buffer.until, messages[messages.length - 1]?.at ?? buffer.until),
    next: page.next,
    done: !page.next,
  };
}

/** A buffer made of the first page fetched at `offset`. */
export function startBuffer(offset: number, page: ChatReplayPage): ReplayBuffer {
  return extendBuffer({ messages: [], from: offset, until: offset, done: false }, page);
}

/** Drops the messages far behind `time`; the buffer then starts where the kept ones do. */
export function trimBuffer(buffer: ReplayBuffer, time: number, keep = REPLAY_KEEP_BEHIND): ReplayBuffer {
  const cut = time - keep;
  if (cut <= buffer.from || (buffer.messages[0]?.at ?? 0) >= cut) {
    return buffer;
  }
  const kept = buffer.messages.filter((message) => (message.at ?? 0) >= cut);
  return { ...buffer, messages: kept, from: cut };
}

/** Whether playback time is in the stretch of the VOD a buffer (or a request about to fill one) covers, as opposed to a seek away from it. */
export function replayCovers(range: { from: number; until: number; done?: boolean }, time: number): boolean {
  return time >= range.from - REPLAY_BACK_TOLERANCE && (range.done === true || time <= range.until + REPLAY_FORWARD_GAP);
}

export type ReplayFetch = { kind: 'none' } | { kind: 'offset'; offset: number } | { kind: 'next'; cursor: string };

/** The one request to make now: start over at the playhead after a seek, follow the cursor to stay ahead of it, or nothing. */
export function planReplayFetch(buffer: ReplayBuffer | null, time: number): ReplayFetch {
  const offset = Math.floor(Math.max(0, Number.isFinite(time) ? time : 0));
  if (!buffer || !replayCovers(buffer, time)) {
    return { kind: 'offset', offset };
  }
  if (buffer.done || !buffer.next || buffer.until >= time + REPLAY_AHEAD) {
    return { kind: 'none' };
  }
  return { kind: 'next', cursor: buffer.next };
}
