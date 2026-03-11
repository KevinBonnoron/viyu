import { groupBySeason } from '../src/lib/series-seasons';
import { expect, test } from 'bun:test';

const episodes = (...seasons: (number | null)[]) => seasons.map((season, index) => ({ id: index + 1, season }));

test('a series that names its seasons is cut into one block per season, keeping each episode’s place', () => {
  const blocks = groupBySeason(episodes(1, 1, 2, 2, 2, 3));
  expect(blocks.map((block) => block.season)).toEqual([1, 2, 3]);
  expect(blocks.map((block) => block.items.map((entry) => entry.position))).toEqual([[1, 2], [3, 4, 5], [6]]);
});

test('a series without seasons stays one block', () => {
  const blocks = groupBySeason(episodes(null, null, null));
  expect(blocks).toHaveLength(1);
  expect(blocks[0].season).toBeNull();
  expect(blocks[0].items).toHaveLength(3);
  expect(groupBySeason([])[0].items).toHaveLength(0);
});

test('episodes without a season form their own block when they are one uninterrupted run', () => {
  expect(groupBySeason(episodes(1, 1, 2, null, null)).map((block) => block.season)).toEqual([1, 2, null]);
});

test('a season that comes back later is not split, so the playing order is never rearranged', () => {
  const blocks = groupBySeason(episodes(1, 2, 1, null));
  expect(blocks).toHaveLength(1);
  expect(blocks[0].items.map((entry) => entry.item.id)).toEqual([1, 2, 3, 4]);
  expect(groupBySeason(episodes(1, null, 1))).toHaveLength(1);
});
