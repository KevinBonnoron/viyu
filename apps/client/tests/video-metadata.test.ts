import { ApiError } from '../src/lib/api-error';
import { createVideoMetadataQueue } from '../src/lib/video-metadata';
import type { VideoResult } from '../src/types';
import { afterEach, beforeEach, expect, jest, test } from 'bun:test';

const video = { id: 'video', published: '2026-11-12', title: 'Recovered' } as VideoResult;
// Drain the loader's promise chain without advancing the retry clock.
async function settle() {
  for (let i = 0; i < 8; i++) {
    await Promise.resolve();
  }
}

beforeEach(() => jest.useFakeTimers());
afterEach(() => jest.useRealTimers());

test('a 429 keeps the card pending, pauses other cards and retries without remounting', async () => {
  const calls: string[] = [];
  const subscribe = createVideoMetadataQueue(async (id) => {
    calls.push(id);
    if (calls.length === 1) {
      throw new ApiError('TOO_MANY_REQUESTS', 429);
    }
    return { ...video, id };
  });
  const answers: unknown[] = [];
  const stop = subscribe('one', 'one', undefined, (outcome) => answers.push(outcome));
  await settle();
  const stopOther = subscribe('two', 'two', undefined, (outcome) => answers.push(outcome));
  await settle();
  expect(calls).toEqual(['one']);
  expect(answers).toEqual([]);
  jest.advanceTimersByTime(29999);
  await settle();
  expect(calls).toEqual(['one']);
  jest.advanceTimersByTime(1);
  await settle();
  expect(calls).toEqual(['one', 'one', 'two']);
  expect(answers).toEqual([{ video: { ...video, id: 'one' } }, { video: { ...video, id: 'two' } }]);
  stop();
  stopOther();
});

test('the source rate limit also pauses newly visible cards, with increasing retry delays', async () => {
  const calls: string[] = [];
  const subscribe = createVideoMetadataQueue(async (id) => {
    calls.push(id);
    throw new ApiError('SOURCE_RATE_LIMITED', 503);
  });
  const stop = subscribe('one', 'one', undefined, () => {});
  await settle();
  jest.advanceTimersByTime(30000);
  await settle();
  expect(calls).toEqual(['one', 'one']);
  const stopOther = subscribe('two', 'two', undefined, () => {});
  jest.advanceTimersByTime(59999);
  await settle();
  expect(calls).toEqual(['one', 'one']);
  jest.advanceTimersByTime(1);
  await settle();
  expect(calls).toEqual(['one', 'one', 'one', 'two']);
  stop();
  stopOther();
});

test('unmounted cards cancel their request and stop retrying after a transport failure', async () => {
  let calls = 0;
  let signal: AbortSignal | undefined;
  const subscribe = createVideoMetadataQueue(async (_id, options) => {
    signal = options.signal;
    if (++calls === 1) {
      throw new TypeError('connection lost');
    }
    return video;
  });
  const answers: unknown[] = [];
  const stop = subscribe('one', 'one', undefined, (outcome) => answers.push(outcome));
  await settle();
  stop();
  expect(signal?.aborted).toBe(true);
  jest.advanceTimersByTime(300000);
  await settle();
  expect(calls).toBe(1);
  expect(answers).toEqual([]);
});

test.each([new TypeError('connection lost'), new DOMException('timed out', 'TimeoutError')])('a temporary transport failure recovers: %s', async (error) => {
  let calls = 0;
  const subscribe = createVideoMetadataQueue(async () => {
    if (++calls === 1) {
      throw error;
    }
    return video;
  });
  const answers: unknown[] = [];
  const stop = subscribe('one', 'one', undefined, (outcome) => answers.push(outcome));
  await settle();
  expect(answers).toEqual([]);
  jest.advanceTimersByTime(30000);
  await settle();
  expect(answers).toEqual([{ video }]);
  expect(calls).toBe(2);
  stop();
});

test('non-retryable failures reach the card and do not retry automatically', async () => {
  let calls = 0;
  const error = new ApiError('NOT_FOUND', 404);
  const subscribe = createVideoMetadataQueue(async () => {
    calls++;
    throw error;
  });
  const answers: unknown[] = [];
  const stop = subscribe('one', 'one', undefined, (outcome) => answers.push(outcome));
  await settle();
  jest.advanceTimersByTime(300000);
  await settle();
  expect(answers).toEqual([{ error }]);
  expect(calls).toBe(1);
  stop();
});

test('duplicate cards share the fetch, and only the last listener leaving cancels it', async () => {
  let calls = 0;
  let signal: AbortSignal | undefined;
  let finish!: (value: VideoResult) => void;
  const subscribe = createVideoMetadataQueue((_id, options) => {
    calls++;
    signal = options.signal;
    const { promise, resolve } = Promise.withResolvers<VideoResult>();
    finish = resolve;
    return promise;
  });
  const first: unknown[] = [];
  const second: unknown[] = [];
  const stopFirst = subscribe('one', 'one', undefined, (outcome) => first.push(outcome));
  const stopSecond = subscribe('one', 'one', undefined, (outcome) => second.push(outcome));
  stopFirst();
  expect(signal?.aborted).toBe(false);
  finish(video);
  await settle();
  expect(calls).toBe(1);
  expect(first).toEqual([]);
  expect(second).toEqual([{ video }]);
  stopSecond();
  expect(signal?.aborted).toBe(true);
});

test('no more than four metadata fetches run at once', async () => {
  const started: string[] = [];
  const finish: ((value: VideoResult) => void)[] = [];
  const subscribe = createVideoMetadataQueue((id) => {
    started.push(id);
    const { promise, resolve } = Promise.withResolvers<VideoResult>();
    finish.push(resolve);
    return promise;
  });
  const stop = Array.from({ length: 6 }, (_, i) => subscribe(String(i), String(i), undefined, () => {}));
  expect(started).toEqual(['0', '1', '2', '3']);
  finish[0](video);
  await settle();
  expect(started).toEqual(['0', '1', '2', '3', '4']);
  for (const unsubscribe of stop) {
    unsubscribe();
  }
  for (const resolve of finish) {
    resolve(video);
  }
  await settle();
  expect(started).toEqual(['0', '1', '2', '3', '4']);
});

test('metadata waits at least as long as the server asks, even past the local delay ceiling', async () => {
  let calls = 0;
  const subscribe = createVideoMetadataQueue(async () => {
    if (++calls === 1) {
      throw new ApiError('SOURCE_RATE_LIMITED', 503, undefined, 600000);
    }
    return video;
  });
  const answers: unknown[] = [];
  const stop = subscribe('one', 'one', undefined, (outcome) => answers.push(outcome));
  await settle();
  jest.advanceTimersByTime(599999);
  await settle();
  expect(calls).toBe(1);
  expect(answers).toEqual([]);
  jest.advanceTimersByTime(1);
  await settle();
  expect(calls).toBe(2);
  expect(answers).toEqual([{ video }]);
  stop();
});
