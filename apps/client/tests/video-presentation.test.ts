import { displayTitle, needsEnrichment, thumbnailCandidates } from '../src/features/video/utils/video-presentation';
import { isTypedSearchQuery } from '../src/lib/search-query';
import { expect, test } from 'bun:test';

const listing = { title: 'Exhumed - The absolute worst of PS1', originalTitle: '', originalTitleVerified: false };

test('an unproven listing keeps its title until enriched metadata proves the original', () => {
  expect(displayTitle(listing, true)).toBe(listing.title);
  expect(displayTitle(listing, true, 'Exhumed - Pire 100% de la Ps1')).toBe('Exhumed - Pire 100% de la Ps1');
});

test('a verified listing shows its original title and ignores any other candidate', () => {
  const verified = { ...listing, originalTitle: 'Pire de la PS1', originalTitleVerified: true };
  expect(displayTitle(verified, true, 'something else')).toBe('Pire de la PS1');
});

test('the listing title is final when original titles are not preferred', () => {
  expect(displayTitle(listing, false, 'Exhumed - Pire 100% de la Ps1')).toBe(listing.title);
  expect(displayTitle({ ...listing, originalTitle: 'Pire de la PS1', originalTitleVerified: true }, false)).toBe(listing.title);
});

test('missing primary thumbnail falls back to variants, then to enriched artwork, without duplicates', () => {
  expect(thumbnailCandidates({ thumbnail: '', thumbnails: ['', ' https://i.ytimg.com/hq720.jpg '] })).toEqual(['https://i.ytimg.com/hq720.jpg']);
  expect(thumbnailCandidates({ thumbnail: 'a.jpg', thumbnails: ['a.jpg', 'b.jpg'] }, { thumbnail: 'max.jpg', thumbnails: ['b.jpg'] })).toEqual([
    'a.jpg',
    'b.jpg',
    'max.jpg',
  ]);
  expect(thumbnailCandidates({ thumbnail: '' }, undefined)).toEqual([]);
});

test('enrichment is requested only for an unproven preferred title or missing artwork', () => {
  const art = { thumbnail: 'a.jpg', thumbnails: ['a.jpg'] };
  expect(needsEnrichment({ originalTitleVerified: false, ...art }, true)).toBe(true);
  expect(needsEnrichment({ originalTitleVerified: false, ...art }, false)).toBe(false);
  expect(needsEnrichment({ originalTitleVerified: true, ...art }, true)).toBe(false);
  expect(needsEnrichment({ originalTitleVerified: true, thumbnail: '', thumbnails: [] }, true)).toBe(true);
});

test('typing launches a search only for real terms, never for a pasted video link', () => {
  expect(isTypedSearchQuery('ed')).toBe(true);
  expect(isTypedSearchQuery(' e ')).toBe(false);
  expect(isTypedSearchQuery('https://youtu.be/I_efxjQV4xc')).toBe(false);
  expect(isTypedSearchQuery('https://youtube.com/playlist?list=PL123')).toBe(true);
});
