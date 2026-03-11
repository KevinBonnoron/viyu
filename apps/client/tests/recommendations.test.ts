import { END_SCREEN_SUGGESTIONS, endScreenKind, endScreenSuggestions, videoKey, visibleRecommendations } from '../src/lib/recommendations';
import type { VideoResult } from '../src/types';
import { expect, test } from 'bun:test';

const video = (id: string, platform = 'youtube') => ({ id, platform }) as VideoResult;
const list = ['a', 'b', 'c', 'd', 'e', 'f'].map((id) => video(id));

test('a stream has no end screen; a series with a next episode offers it, anything else offers recommendations', () => {
  expect(endScreenKind({ live: true, hasNextEpisode: true })).toBeNull();
  expect(endScreenKind({ live: true, hasNextEpisode: false })).toBeNull();
  expect(endScreenKind({ live: false, hasNextEpisode: true })).toBe('next');
  expect(endScreenKind({ live: false, hasNextEpisode: false })).toBe('suggestions');
});

test('recommendations hide the series’ episodes and what the viewer is not interested in', () => {
  const statuses = new Map([
    ['b', { reaction: 'not_interested' as const }],
    ['c', { reaction: 'like' as const }],
  ]);
  const exclude = new Set([videoKey(video('a'))]);
  expect(visibleRecommendations(list, statuses, exclude).map((item) => item.id)).toEqual(['c', 'd', 'e', 'f']);
  expect(visibleRecommendations(list, statuses).map((item) => item.id)).toEqual(['a', 'c', 'd', 'e', 'f']);
});

test('the exclusion key is per platform', () => {
  const exclude = new Set([videoKey(video('a', 'twitch'))]);
  expect(visibleRecommendations([video('a')], new Map(), exclude)).toHaveLength(1);
  expect(visibleRecommendations([video('a', 'twitch')], new Map(), exclude)).toHaveLength(0);
});

test('the end screen offers at most four visible recommendations, in order, never the video that just played', () => {
  expect(END_SCREEN_SUGGESTIONS).toBe(4);
  const statuses = new Map([['b', { reaction: 'not_interested' as const }]]);
  expect(endScreenSuggestions(list, statuses).map((item) => item.id)).toEqual(['a', 'c', 'd', 'e']);
  expect(endScreenSuggestions(list, statuses, { currentId: 'a' }).map((item) => item.id)).toEqual(['c', 'd', 'e', 'f']);
  expect(endScreenSuggestions(list.slice(0, 2), new Map())).toHaveLength(2);
  expect(endScreenSuggestions([], new Map())).toEqual([]);
});
