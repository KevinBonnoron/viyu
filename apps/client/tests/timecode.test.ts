import { formatTimecode, parseTimecode } from '../src/features/video/utils/timecode';
import { expect, test } from 'bun:test';

test('timecodes format compactly for URLs', () => {
  expect(formatTimecode(0)).toBe('0s');
  expect(formatTimecode(45)).toBe('45s');
  expect(formatTimecode(90)).toBe('1m30s');
  expect(formatTimecode(3600)).toBe('1h0m0s');
  expect(formatTimecode(3723)).toBe('1h2m3s');
  expect(formatTimecode(-5)).toBe('0s');
});

test('timecodes parse from plain seconds and the compact form', () => {
  expect(parseTimecode(90)).toBe(90);
  expect(parseTimecode('90')).toBe(90);
  expect(parseTimecode('45s')).toBe(45);
  expect(parseTimecode('1m30s')).toBe(90);
  expect(parseTimecode('1h2m3s')).toBe(3723);
  expect(parseTimecode('2H')).toBe(7200);
});

test('anything that is not a positive time means from the start', () => {
  expect(parseTimecode(undefined)).toBeUndefined();
  expect(parseTimecode('')).toBeUndefined();
  expect(parseTimecode(0)).toBeUndefined();
  expect(parseTimecode(-10)).toBeUndefined();
  expect(parseTimecode('abc')).toBeUndefined();
  expect(parseTimecode('1m2')).toBeUndefined();
});
