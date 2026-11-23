import { mergeTimeline } from '../src/lib/timeline-merge';
import { expect, test } from 'bun:test';

const row = (id: string, published: string) => ({ id, published });

test('newest first, unknown publications last, ties by id', () => {
  const rows = [row('b', ''), row('c', '2026-11-02 10:00:00.000Z'), row('a', '2026-11-02 10:00:00.000Z'), row('d', '2026-11-03 10:00:00.000Z')];
  expect(mergeTimeline([{ rows, hasNextPage: false }]).rows.map((item) => item.id)).toEqual(['d', 'a', 'c', 'b']);
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

test('oldest-first pages advance the earliest frontier without exposing later rows', () => {
  const first = mergeTimeline(
    [
      { rows: [row('a', '2026-11-01'), row('c', '2026-11-03')], hasNextPage: true },
      { rows: [row('b', '2026-11-02'), row('d', '2026-11-04')], hasNextPage: true },
    ],
    'asc',
  );
  expect(first.rows.map((item) => item.id)).toEqual(['a', 'b', 'c']);
  expect(first.pending).toBe(0);

  const second = mergeTimeline(
    [
      { rows: [row('a', '2026-11-01'), row('c', '2026-11-03'), row('e', '2026-11-05')], hasNextPage: false },
      { rows: [row('b', '2026-11-02'), row('d', '2026-11-04')], hasNextPage: true },
    ],
    'asc',
  );
  expect(second.rows.map((item) => item.id)).toEqual(['a', 'b', 'c', 'd']);
  expect(second.pending).toBe(1);

  const complete = mergeTimeline(
    [
      { rows: [row('a', '2026-11-01'), row('c', '2026-11-03'), row('e', '2026-11-05')], hasNextPage: false },
      { rows: [row('b', '2026-11-02'), row('d', '2026-11-04'), row('f', '2026-11-06')], hasNextPage: false },
    ],
    'asc',
  );
  expect(complete.rows.map((item) => item.id)).toEqual(['a', 'b', 'c', 'd', 'e', 'f']);
  expect(complete.pending).toBe(-1);
});

test('watermarks include the id tie breaker in both directions', () => {
  for (const order of ['asc', 'desc'] as const) {
    const merged = mergeTimeline(
      [
        { rows: [row('b', '2026-11-02')], hasNextPage: true },
        { rows: [row('a', '2026-11-02'), row('d', '2026-11-02')], hasNextPage: false },
      ],
      order,
    );
    expect(merged.rows.map((item) => item.id)).toEqual(['a', 'b']);
    expect(merged.pending).toBe(0);
  }
});

test('unknown dates follow all dated rows and paginate by id in both directions', () => {
  for (const order of ['asc', 'desc'] as const) {
    const dated = mergeTimeline(
      [
        { rows: [row('e', '2026-11-01')], hasNextPage: false },
        { rows: [], hasNextPage: false },
      ],
      order,
    );
    const unknown = mergeTimeline(
      [
        { rows: [row('b', '')], hasNextPage: true },
        { rows: [row('a', ''), row('d', '')], hasNextPage: false },
      ],
      order,
    );
    expect([...dated.rows, ...unknown.rows].map((item) => item.id)).toEqual(['e', 'a', 'b']);
    expect(unknown.pending).toBe(0);

    const complete = mergeTimeline(
      [
        { rows: [row('b', ''), row('c', '')], hasNextPage: false },
        { rows: [row('a', ''), row('d', '')], hasNextPage: false },
      ],
      order,
    );
    expect([...dated.rows, ...complete.rows].map((item) => item.id)).toEqual(['e', 'a', 'b', 'c', 'd']);
    expect(complete.pending).toBe(-1);
    expect(mergeTimeline([{ rows: [row('a', ''), row('e', '2026-11-01')], hasNextPage: false }], order).rows.map((item) => item.id)).toEqual(['e', 'a']);
  }
});

test('undefined and empty completed feeds do not bound an oldest-first merge', () => {
  const merged = mergeTimeline([undefined, { rows: [], hasNextPage: false }, { rows: [row('a', '')], hasNextPage: false }], 'asc');
  expect(merged.rows.map((item) => item.id)).toEqual(['a']);
  expect(merged.pending).toBe(-1);
});
