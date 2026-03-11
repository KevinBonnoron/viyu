import { barPercent, formatMonth, formatWatchTime } from '../src/lib/utils';
import { expect, test } from 'bun:test';

test('watch time reads as hours and minutes in the reader’s language', () => {
  expect(formatWatchTime(12 * 3600 + 5 * 60, 'fr')).toBe('12 h 05');
  expect(formatWatchTime(12 * 3600 + 5 * 60, 'en')).toBe('12h 05m');
  expect(formatWatchTime(5 * 60, 'fr')).toBe('5 min');
  expect(formatWatchTime(5 * 60, 'en')).toBe('5m');
  expect(formatWatchTime(3600, 'fr')).toBe('1 h 00');
  expect(formatWatchTime(3599, 'en')).toBe('1h 00m');
  expect(formatWatchTime(0, 'en')).toBe('0m');
  expect(formatWatchTime(20, 'fr')).toBe('0 min');
  expect(formatWatchTime(Number.NaN, 'fr')).toBe('0 min');
  expect(formatWatchTime(-50, 'en')).toBe('0m');
  expect(formatWatchTime(1234 * 3600, 'en')).toBe(`${(1234).toLocaleString('en')}h 00m`);
});

test('a month is written in the reader’s language', () => {
  expect(formatMonth('2026-03', 'en')).toBe('March 2026');
  expect(formatMonth('2026-03', 'fr')).toBe('mars 2026');
  expect(formatMonth('2026-12', 'en', 'short')).toBe('Dec 26');
  expect(formatMonth('2026-01', 'fr', 'short')).toBe('janv. 26');
  expect(formatMonth('2026-13', 'en')).toBe('2026-13');
  expect(formatMonth('soon', 'en')).toBe('soon');
});

test('a bar is sized against the largest value', () => {
  expect(barPercent(50, 200)).toBe(25);
  expect(barPercent(200, 200)).toBe(100);
  expect(barPercent(300, 200)).toBe(100);
  expect(barPercent(0, 200)).toBe(0);
  expect(barPercent(10, 0)).toBe(0);
  expect(barPercent(-5, 100)).toBe(0);
  expect(barPercent(Number.NaN, 100)).toBe(0);
});
