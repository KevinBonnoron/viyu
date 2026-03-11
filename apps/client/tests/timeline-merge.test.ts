import { mergeTimeline, newestFirst } from '../src/lib/timeline-merge';
import { expect, test } from 'bun:test';

const row = (id: string, published: string) => ({ id, published });

test('newest first, unknown publications last, ties by id', () => {
  const rows = [row('b', ''), row('c', '2026-11-02 10:00:00.000Z'), row('a', '2026-11-02 10:00:00.000Z'), row('d', '2026-11-03 10:00:00.000Z')];
  expect(rows.sort(newestFirst).map((item) => item.id)).toEqual(['d', 'a', 'c', 'b']);
});

test('complete feeds merge into one list and nothing is pending', () => {
  const merged = mergeTimeline([
    { rows: [row('a', '2026-11-05 00:00:00.000Z'), row('c', '2026-11-01 00:00:00.000Z')], hasNextPage: false },
    { rows: [row('b', '2026-11-03 00:00:00.000Z')], hasNextPage: false },
  ]);
  expect(merged.rows.map((item) => item.id)).toEqual(['a', 'b', 'c']);
  expect(merged.pending).toBe(-1);
});

test('nothing older than a feed with pages left is final yet', () => {
  const merged = mergeTimeline([
    { rows: [row('a', '2026-11-05 00:00:00.000Z'), row('c', '2026-11-04 00:00:00.000Z')], hasNextPage: true },
    { rows: [row('b', '2026-11-03 00:00:00.000Z'), row('d', '2026-11-01 00:00:00.000Z')], hasNextPage: false },
  ]);
  // The first feed may still hold videos between 4 November and the ones it loaded; the second feed's videos are older than that bound.
  expect(merged.rows.map((item) => item.id)).toEqual(['a', 'c']);
  expect(merged.pending).toBe(0);
});

test('the feed bounding the list is the one with the newest last loaded video', () => {
  const merged = mergeTimeline([
    { rows: [row('a', '2026-11-05 00:00:00.000Z'), row('b', '2026-11-02 00:00:00.000Z')], hasNextPage: true },
    { rows: [row('c', '2026-11-04 00:00:00.000Z'), row('d', '2026-11-03 00:00:00.000Z')], hasNextPage: true },
  ]);
  expect(merged.rows.map((item) => item.id)).toEqual(['a', 'c', 'd']);
  expect(merged.pending).toBe(1);
});

test('feeds that have not reported yet contribute nothing and do not bound the list', () => {
  const merged = mergeTimeline([undefined, { rows: [row('a', '2026-11-05 00:00:00.000Z')], hasNextPage: false }]);
  expect(merged.rows.map((item) => item.id)).toEqual(['a']);
  expect(merged.pending).toBe(-1);
});
