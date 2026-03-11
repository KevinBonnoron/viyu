import { formatRelativePublication } from '../src/lib/utils';
import { expect, test } from 'bun:test';

test('publication age uses UTC calendar dates and localized relative units', () => {
  const now = new Date('2026-11-04T23:59:00Z');
  expect(formatRelativePublication(new Date('2026-11-04'), 'fr', now)).toBe('aujourd’hui');
  expect(formatRelativePublication(new Date('2026-11-03'), 'fr', now)).toBe('hier');
  expect(formatRelativePublication(new Date('2026-10-28'), 'en', now)).toBe('last week');
  expect(formatRelativePublication(new Date('2026-10-05'), 'en', now)).toBe('last month');
  expect(formatRelativePublication(new Date('2025-11-04'), 'en', now)).toBe('last year');
  expect(formatRelativePublication(new Date('2026-11-05'), 'en', now)).toBe('tomorrow');
});

test('same-day precise timestamps use hours or minutes without inventing a time for date-only values', () => {
  const now = new Date('2026-11-04T16:00:00Z');
  expect(formatRelativePublication(new Date('2026-11-04T13:30:00Z'), 'en', now, true)).toBe('2 hours ago');
  expect(formatRelativePublication(new Date('2026-11-04T15:45:00Z'), 'fr', now, true)).toBe('il y a 15 minutes');
  expect(formatRelativePublication(new Date('2026-11-04'), 'en', now)).toBe('today');
});
