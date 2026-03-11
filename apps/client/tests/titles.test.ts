import { recordTitles, TRANSLATION_LANGUAGE } from '../src/features/video/utils/titles';
import { expect, test } from 'bun:test';

test('only an original title serves as both titles', () => {
  expect(recordTitles({ original: 'Le titre' })).toEqual({ title: 'Le titre', originalTitle: 'Le titre' });
});

test('a translation is the title and the original stays the original', () => {
  expect(recordTitles({ original: 'Le titre', [TRANSLATION_LANGUAGE]: 'The title' })).toEqual({ title: 'The title', originalTitle: 'Le titre' });
});

test('a translation alone is also the original', () => {
  expect(recordTitles({ [TRANSLATION_LANGUAGE]: 'The title' })).toEqual({ title: 'The title', originalTitle: 'The title' });
});

test('no titles, null, or empty values give empty titles', () => {
  expect(recordTitles(null)).toEqual({ title: '', originalTitle: '' });
  expect(recordTitles({})).toEqual({ title: '', originalTitle: '' });
  expect(recordTitles({ original: '', [TRANSLATION_LANGUAGE]: '' })).toEqual({ title: '', originalTitle: '' });
});

test('an empty translation falls back to the original', () => {
  expect(recordTitles({ original: 'Le titre', [TRANSLATION_LANGUAGE]: '' })).toEqual({ title: 'Le titre', originalTitle: 'Le titre' });
});
