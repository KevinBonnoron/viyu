import { PLAYBACK_DOUBLE_CLICK_WINDOW, PlaybackClickSequence } from '../src/features/video/utils/playback-clicks';
import { describe, expect, test } from 'bun:test';

const click = (detail: number, timeStamp: number) => ({ detail, timeStamp });

describe('playback click sequence', () => {
  for (const paused of [true, false]) {
    test(`a rapid native pair restores the state before its first click (paused=${paused})`, () => {
      const sequence = new PlaybackClickSequence();
      expect(sequence.click(click(1, 1000), paused)).toBe(true);
      expect(sequence.click(click(2, 1100), !paused)).toBe(false);
      expect(sequence.doubleClick()).toBe(paused);
      expect(sequence.doubleClick()).toBeNull();
    });
  }

  test('two spaced clicks both act immediately despite a native double click', () => {
    const sequence = new PlaybackClickSequence();
    expect(sequence.click(click(1, 1000), false)).toBe(true);
    expect(sequence.click(click(2, 1000 + PLAYBACK_DOUBLE_CLICK_WINDOW + 1), true)).toBe(true);
    expect(sequence.doubleClick()).toBeNull();
  });

  test('the rapid pair boundary is inclusive', () => {
    const sequence = new PlaybackClickSequence();
    sequence.click(click(1, 1000), false);
    expect(sequence.click(click(2, 1000 + PLAYBACK_DOUBLE_CLICK_WINDOW), true)).toBe(false);
    expect(sequence.doubleClick()).toBe(false);
  });

  test('a browser-restarted click count stays two single clicks', () => {
    const sequence = new PlaybackClickSequence();
    expect(sequence.click(click(1, 1000), true)).toBe(true);
    expect(sequence.click(click(1, 1100), false)).toBe(true);
    expect(sequence.doubleClick()).toBeNull();
  });

  test('four rapid clicks can seek twice without carrying the first pair forward', () => {
    const sequence = new PlaybackClickSequence();
    for (const detail of [1, 3]) {
      expect(sequence.click(click(detail, detail * 100), false)).toBe(true);
      expect(sequence.click(click(detail + 1, detail * 100 + 50), true)).toBe(false);
      expect(sequence.doubleClick()).toBe(false);
    }
  });

  test('a hold or cancellation cannot turn the next even-count click into a seek', () => {
    const sequence = new PlaybackClickSequence();
    sequence.click(click(1, 1000), false);
    sequence.reset();
    expect(sequence.click(click(2, 1100), false)).toBe(true);
    expect(sequence.doubleClick()).toBeNull();
  });

  test('assistive clicks act immediately without starting a double-click candidate', () => {
    const sequence = new PlaybackClickSequence();
    expect(sequence.click(click(0, 1000), true)).toBe(true);
    expect(sequence.click(click(2, 1100), false)).toBe(true);
    expect(sequence.doubleClick()).toBeNull();
  });
});
