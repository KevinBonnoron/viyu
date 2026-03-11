import {
  comparePublicationsNewestFirst,
  hasPublicationTime,
  mergePublication,
  publicationInstant,
  publishedFromRecord,
} from '../src/features/video/utils/publication';
import { expect, test } from 'bun:test';

test('an unknown publication stays empty', () => {
  expect(publishedFromRecord('')).toBe('');
  expect(publishedFromRecord(undefined)).toBe('');
  expect(publishedFromRecord(null)).toBe('');
});

test('a publication stored at UTC midnight is only a day', () => {
  expect(publishedFromRecord('2026-11-09 00:00:00.000Z')).toBe('2026-11-09');
  expect(publishedFromRecord('2026-11-09 00:00:00Z')).toBe('2026-11-09');
  expect(hasPublicationTime('2026-11-09')).toBe(false);
});

test('any other stored time is an exact RFC3339 timestamp', () => {
  expect(publishedFromRecord('2026-11-09 07:02:19.123Z')).toBe('2026-11-09T07:02:19.123Z');
  expect(publishedFromRecord('2026-11-09 07:02:19Z')).toBe('2026-11-09T07:02:19Z');
  expect(publishedFromRecord('2026-11-09 00:00:00.500Z')).toBe('2026-11-09T00:00:00.500Z');
  expect(hasPublicationTime('2026-11-09T07:02:19Z')).toBe(true);
  expect(hasPublicationTime('')).toBe(false);
});

test('a publication reads as a UTC instant, unknown as null', () => {
  expect(publicationInstant('2026-11-09')?.toISOString()).toBe('2026-11-09T00:00:00.000Z');
  expect(publicationInstant('2026-11-09T07:02:19.123Z')?.toISOString()).toBe('2026-11-09T07:02:19.123Z');
  expect(publicationInstant('')).toBeNull();
  expect(publicationInstant('nonsense')).toBeNull();
});

test('publications sort by instant, newest first, unknown last', () => {
  const published = ['', '2026-11-09', '2026-11-09T07:02:19Z', '2026-11-08T23:00:00Z'];
  expect([...published].sort(comparePublicationsNewestFirst)).toEqual(['2026-11-09T07:02:19Z', '2026-11-09', '2026-11-08T23:00:00Z', '']);
});

test('a fetched publication completes a card only with what it lacks', () => {
  expect(mergePublication('', '2026-11-09T07:02:19Z')).toBe('2026-11-09T07:02:19Z');
  expect(mergePublication('', undefined)).toBe('');
  expect(mergePublication('2026-11-09', '2026-11-09T07:02:19Z')).toBe('2026-11-09T07:02:19Z');
  expect(mergePublication('2026-11-09', '2026-11-10T07:02:19Z')).toBe('2026-11-09');
  expect(mergePublication('2026-11-09T05:00:00Z', '2026-11-09T07:02:19Z')).toBe('2026-11-09T05:00:00Z');
});
