import {
  extendBuffer,
  planReplayFetch,
  REPLAY_AHEAD,
  REPLAY_BACK_TOLERANCE,
  REPLAY_FORWARD_GAP,
  replayCovers,
  startBuffer,
  trimBuffer,
  visibleMessages,
} from '../src/lib/chat-replay';
import type { ChatMessage } from '../src/types/chat.type';
import { expect, test } from 'bun:test';

function message(id: string, at: number): ChatMessage {
  return { id, at, user: 'viewer', fragments: [{ text: id }] };
}

function run(from: number, count: number): ChatMessage[] {
  return Array.from({ length: count }, (_, index) => message(`m${from + index}`, from + index));
}

test('visible messages are the last ones at or before the playback time', () => {
  const messages = run(0, 10);
  expect(visibleMessages(messages, 4.5).map((item) => item.id)).toEqual(['m0', 'm1', 'm2', 'm3', 'm4']);
  expect(visibleMessages(messages, 4.5, 2).map((item) => item.id)).toEqual(['m3', 'm4']);
  expect(visibleMessages(messages, -1)).toEqual([]);
  expect(visibleMessages(messages, 100)).toHaveLength(10);
  expect(visibleMessages([], 5)).toEqual([]);
});

test('visible messages keep every message that shares the playback time', () => {
  const messages = [message('a', 1), message('b', 2), message('c', 2), message('d', 3)];
  expect(visibleMessages(messages, 2).map((item) => item.id)).toEqual(['a', 'b', 'c']);
});

test('visible messages default to the last 200', () => {
  const visible = visibleMessages(run(0, 500), 1000);
  expect(visible).toHaveLength(200);
  expect(visible[0]?.id).toBe('m300');
});

test('a first page starts a buffer at its offset and following pages extend it', () => {
  const first = startBuffer(10, { messages: run(10, 5), next: 'c1' });
  expect(first).toMatchObject({ from: 10, until: 14, next: 'c1', done: false });
  const second = extendBuffer(first, { messages: run(15, 5) });
  expect(second.messages.map((item) => item.id)).toEqual(run(10, 10).map((item) => item.id));
  expect(second).toMatchObject({ from: 10, until: 19, done: true });
  expect(second.next).toBeUndefined();
});

test('a page that adds nothing new ends the buffer instead of looping on its cursor', () => {
  const first = startBuffer(0, { messages: run(0, 5), next: 'c1' });
  expect(extendBuffer(first, { messages: [], next: 'c2' })).toMatchObject({ done: true, next: undefined });
  expect(extendBuffer(first, { messages: run(0, 5), next: 'c2' })).toMatchObject({ done: true, next: undefined });
});

test('overlapping and unsorted pages never duplicate or misorder messages', () => {
  const first = startBuffer(0, { messages: run(0, 5), next: 'c1' });
  const next = extendBuffer(first, { messages: [message('m7', 7), message('m4', 4), message('m5', 5)], next: 'c2' });
  expect(next.messages.map((item) => item.id)).toEqual(['m0', 'm1', 'm2', 'm3', 'm4', 'm5', 'm7']);
  expect(next.until).toBe(7);
});

test('messages without a valid time are not buffered', () => {
  const page = { messages: [{ id: 'live', user: 'viewer', fragments: [] }, message('bad', Number.NaN), message('ok', 3)] };
  expect(startBuffer(0, page).messages.map((item) => item.id)).toEqual(['ok']);
});

test('nothing is fetched ahead once the buffer is far enough, and the cursor is followed when it is not', () => {
  const buffer = startBuffer(0, { messages: run(0, 100), next: 'c1' });
  expect(planReplayFetch(buffer, 10)).toEqual({ kind: 'none' });
  expect(planReplayFetch(buffer, 99 - REPLAY_AHEAD)).toEqual({ kind: 'none' });
  expect(planReplayFetch(buffer, 99 - REPLAY_AHEAD + 1)).toEqual({ kind: 'next', cursor: 'c1' });
  expect(planReplayFetch(buffer, 99)).toEqual({ kind: 'next', cursor: 'c1' });
});

test('a buffer holding the end of the chat is never extended', () => {
  const buffer = startBuffer(0, { messages: run(0, 10) });
  expect(planReplayFetch(buffer, 9)).toEqual({ kind: 'none' });
  expect(planReplayFetch(buffer, 5000)).toEqual({ kind: 'none' });
});

test('without a buffer the chat is fetched at the whole second of the playhead', () => {
  expect(planReplayFetch(null, 12.9)).toEqual({ kind: 'offset', offset: 12 });
  expect(planReplayFetch(null, -3)).toEqual({ kind: 'offset', offset: 0 });
  expect(planReplayFetch(null, Number.NaN)).toEqual({ kind: 'offset', offset: 0 });
});

test('a seek back past the start, or far past the buffered range, starts over at the playhead', () => {
  const buffer = startBuffer(100, { messages: run(100, 30), next: 'c1' });
  expect(planReplayFetch(buffer, 100 - REPLAY_BACK_TOLERANCE)).not.toMatchObject({ kind: 'offset' });
  expect(planReplayFetch(buffer, 100 - REPLAY_BACK_TOLERANCE - 1)).toEqual({ kind: 'offset', offset: 96 });
  expect(planReplayFetch(buffer, 129 + REPLAY_FORWARD_GAP)).toEqual({ kind: 'next', cursor: 'c1' });
  expect(planReplayFetch(buffer, 129 + REPLAY_FORWARD_GAP + 1)).toEqual({ kind: 'offset', offset: 145 });
  expect(planReplayFetch(buffer, 500)).toEqual({ kind: 'offset', offset: 500 });
});

test('a seek back inside the buffered range keeps the buffer', () => {
  const buffer = startBuffer(0, { messages: run(0, 100), next: 'c1' });
  expect(planReplayFetch(buffer, 20)).toEqual({ kind: 'none' });
  expect(replayCovers(buffer, 20)).toBe(true);
});

test('a request in flight covers the stretch it will fill', () => {
  const range = { from: 50, until: 50 + REPLAY_AHEAD };
  expect(replayCovers(range, 50)).toBe(true);
  expect(replayCovers(range, 50 + REPLAY_AHEAD + REPLAY_FORWARD_GAP)).toBe(true);
  expect(replayCovers(range, 50 + REPLAY_AHEAD + REPLAY_FORWARD_GAP + 1)).toBe(false);
  expect(replayCovers(range, 10)).toBe(false);
});

test('trimming drops what is far behind the playhead and moves the start of the buffer there', () => {
  const buffer = startBuffer(0, { messages: run(0, 100), next: 'c1' });
  expect(trimBuffer(buffer, 50, 600)).toBe(buffer);
  const trimmed = trimBuffer(buffer, 90, 30);
  expect(trimmed.from).toBe(60);
  expect(trimmed.messages[0]?.id).toBe('m60');
  expect(trimmed.until).toBe(99);
  expect(planReplayFetch(trimmed, 30)).toEqual({ kind: 'offset', offset: 30 });
  expect(planReplayFetch(trimmed, 70)).toEqual({ kind: 'next', cursor: 'c1' });
});
