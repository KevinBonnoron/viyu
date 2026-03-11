import { parseVideoDescription } from '../src/features/video/utils/description';
import { expect, test } from 'bun:test';

test('description timecodes retain their full text and resolve to exact positions', () => {
  const text = '00:00:00 Introduction\n00:00:36 Les mouvements de Mars\n09:09 Les canaux\n01:02:03 Conclusion';
  const parts = parseVideoDescription(text, 4000);
  expect(parts.filter((p) => p.kind === 'time').map((p) => p.seconds)).toEqual([0, 36, 549, 3723]);
  expect(parts.map((p) => p.text).join('')).toBe(text);
});

test('malformed, embedded and out-of-video timecodes remain ordinary text', () => {
  const text = '99:99 01:99:00 abc12:30 12:300 00:10:01 00:10:00';
  expect(
    parseVideoDescription(text, 600)
      .filter((p) => p.kind === 'time')
      .map((p) => p.seconds),
  ).toEqual([600]);
});

test('HTTP links preserve balanced URL parentheses, exclude prose punctuation and contain their own time parameters', () => {
  const text = 'Voir (https://example.test/watch?t=01:30), https://example.test/wiki/Foo_(bar). javascript:alert(1)';
  const parts = parseVideoDescription(text, 600);
  expect(parts.filter((p) => p.kind === 'url').map((p) => p.href)).toEqual(['https://example.test/watch?t=01:30', 'https://example.test/wiki/Foo_(bar)']);
  expect(parts.filter((p) => p.kind === 'time')).toEqual([]);
  expect(parts.map((p) => p.text).join('')).toBe(text);
});
