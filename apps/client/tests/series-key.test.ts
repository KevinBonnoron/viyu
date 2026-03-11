import { seriesChangeKey } from '../src/lib/series-key';
import { expect, test } from 'bun:test';

const record = (id: string, updated: string) => ({ id, updated });

test('the key does not depend on the order the records arrive in', () => {
  expect(seriesChangeKey([record('a', '1'), record('b', '2')])).toBe(seriesChangeKey([record('b', '2'), record('a', '1')]));
});

test('the key changes when a record is updated, added or removed', () => {
  const base = seriesChangeKey([record('a', '1'), record('b', '2')]);
  expect(seriesChangeKey([record('a', '1'), record('b', '3')])).not.toBe(base);
  expect(seriesChangeKey([record('a', '1'), record('b', '2'), record('c', '1')])).not.toBe(base);
  expect(seriesChangeKey([record('a', '1')])).not.toBe(base);
});

test('no records have the empty key', () => {
  expect(seriesChangeKey([])).toBe('');
});
