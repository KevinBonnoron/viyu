import { nextDurationWatched, reachedWatched, sampledSeconds } from '../src/features/video/utils/playback-checkpoint';
import { expect, test } from 'bun:test';

test('a first checkpoint adds what was played to what the row holds', () => {
  expect(nextDurationWatched(0, 0, 12.5, 600)).toBe(12.5);
  expect(nextDurationWatched(100, 0, 20, 600)).toBe(120);
});

test('a repeated checkpoint adds only the seconds not saved yet', () => {
  // 20 s played, 20 s already saved on top of a stored 100: nothing more to add.
  expect(nextDurationWatched(120, 20, 20, 600)).toBe(120);
  expect(nextDurationWatched(120, 20, 35, 600)).toBe(135);
});

test('a re-watch never counts the same seconds twice', () => {
  let stored = 300;
  let saved = 0;
  for (const played of [5, 10, 15, 15, 40]) {
    stored = nextDurationWatched(stored, saved, played, 1000);
    saved = played;
  }
  expect(stored).toBe(340);
});

test('the total is capped at the video duration', () => {
  expect(nextDurationWatched(590, 0, 30, 600)).toBe(600);
  expect(nextDurationWatched(600, 30, 60, 600)).toBe(600);
});

test('an unknown duration applies no cap', () => {
  for (const duration of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
    expect(nextDurationWatched(100, 0, 50, duration)).toBe(150);
  }
});

test('a played counter that went backwards or is not a number adds nothing', () => {
  expect(nextDurationWatched(100, 30, 20, 600)).toBe(100);
  expect(nextDurationWatched(100, 0, Number.NaN, 600)).toBe(100);
  expect(nextDurationWatched(Number.NaN, 0, 5, 600)).toBe(5);
});

test('sums keep millisecond precision only', () => {
  expect(nextDurationWatched(0.1, 0, 0.2, 600)).toBe(0.3);
});

test('a sample earns the media time that passed when the wall clock allows it', () => {
  expect(sampledSeconds(0.25, 0.25, 1)).toBe(0.25);
  expect(sampledSeconds(2, 1, 2)).toBe(2);
});

test('a seek or a stalled clock earns nothing', () => {
  expect(sampledSeconds(120, 1, 1)).toBe(0);
  expect(sampledSeconds(-5, 1, 1)).toBe(0);
  expect(sampledSeconds(0, 1, 1)).toBe(0);
});

test('a checkpoint marks the video watched once the position or the watch time reaches 90 percent', () => {
  expect(reachedWatched(899, 0, 1000)).toBe(false);
  expect(reachedWatched(900, 0, 1000)).toBe(true);
  expect(reachedWatched(10, 899, 1000)).toBe(false);
  expect(reachedWatched(10, 900, 1000)).toBe(true);
});

test('an unknown duration never marks the video watched', () => {
  for (const duration of [0, -1, Number.NaN, Number.POSITIVE_INFINITY]) {
    expect(reachedWatched(500, 500, duration)).toBe(false);
  }
});
