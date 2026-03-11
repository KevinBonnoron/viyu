import { foldSearch } from '../src/lib/text';
import { expect, test } from 'bun:test';

test('a search text is compared without case or accents', () => {
  expect(foldSearch('Café CRÉATION')).toBe('cafe creation');
  expect(foldSearch('ZeratoR')).toBe(foldSearch('zerator'));
});

test('compatibility forms fold to their plain letters', () => {
  expect(foldSearch('ﬁnale')).toBe('finale');
});

test('text without letters with accents is only lowercased', () => {
  expect(foldSearch('Rust 2.0!')).toBe('rust 2.0!');
  expect(foldSearch('')).toBe('');
});
