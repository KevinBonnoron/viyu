import { mergeResults, resultIdentity } from '../src/lib/search-results';
import type { SearchResult } from '../src/types';
import { describe, expect, test } from 'bun:test';

const channel = (id: string, platform = 'youtube') => ({ id, platform, name: id, thumbnail: '' }) as SearchResult;
const video = (id: string, platform = 'youtube') => ({ id, platform, title: id, duration: 60 }) as SearchResult;

describe('search results', () => {
  test('a video and a channel with the same id are different results', () => {
    expect(resultIdentity(video('a'))).not.toBe(resultIdentity(channel('a')));
  });

  test('the same id on two platforms are different results', () => {
    expect(resultIdentity(video('a', 'youtube'))).not.toBe(resultIdentity(video('a', 'twitch')));
  });

  test('a new page is appended without the results already shown', () => {
    const merged = mergeResults([video('a'), video('b')], [video('b'), video('c'), channel('b')]);
    expect(merged.map(resultIdentity)).toEqual(['video:youtube:a', 'video:youtube:b', 'video:youtube:c', 'channel:youtube:b']);
  });

  test('a result repeated inside one page is kept once, the first occurrence winning', () => {
    const first = { ...video('a'), title: 'first' } as SearchResult;
    const second = { ...video('a'), title: 'second' } as SearchResult;
    expect(mergeResults([], [first, second])).toEqual([first]);
  });

  test('the previous results are not mutated', () => {
    const previous = [video('a')];
    mergeResults(previous, [video('b')]);
    expect(previous).toHaveLength(1);
  });
});
