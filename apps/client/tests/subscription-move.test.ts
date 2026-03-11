import { buildMoveRows, filterMoveRows, selectedSubscriptionIds } from '../src/lib/subscription-move';
import { expect, test } from 'bun:test';

function subscription(id: string, platform: string, name: string, creator?: string, created = id, creatorName?: string) {
  return {
    id,
    created,
    creator,
    expand: creatorName ? { creator: { name: creatorName } } : undefined,
    channelInfo: { id: `${platform}-${name}`, platform, name, thumbnail: `${platform}.png` },
  };
}

const subscriptions = [
  subscription('a', 'twitch', 'zerator', 'g', '2'),
  subscription('b', 'youtube', 'ZeratoR', 'g', '1', 'Zerator'),
  subscription('c', 'youtube', 'Solo', 'lonely'),
  subscription('d', 'twitch', 'Écureuil'),
];

test('a creator group is one row that stands for all its members, and the rows are sorted by name', () => {
  const rows = buildMoveRows(subscriptions);
  expect(rows.map((row) => row.name)).toEqual(['Écureuil', 'Solo', 'Zerator']);
  const group = rows.find((row) => row.key === 'g');
  expect(group?.subscriptionIds.sort()).toEqual(['a', 'b']);
  expect(group?.platforms).toEqual(['youtube', 'twitch']);
  expect(group?.thumbnail).toBe('youtube.png');
  // A creator with a single subscription is a plain channel.
  expect(rows.find((row) => row.key === 'c')?.subscriptionIds).toEqual(['c']);
});

test('a subscription whose channel is not loaded is not listed', () => {
  expect(buildMoveRows([{ id: 'x' }, subscription('d', 'twitch', 'Plain')]).map((row) => row.key)).toEqual(['d']);
});

test('the search ignores case and accents and finds a group by any of its channels', () => {
  const rows = buildMoveRows(subscriptions);
  expect(filterMoveRows(rows, 'ecureuil').map((row) => row.key)).toEqual(['d']);
  expect(filterMoveRows(rows, '  ZERATOR ').map((row) => row.key)).toEqual(['g']);
  expect(filterMoveRows(rows, '')).toHaveLength(3);
  expect(filterMoveRows(rows, 'nothing')).toEqual([]);
});

test('selecting a group selects all of its members', () => {
  const rows = buildMoveRows(subscriptions);
  expect(selectedSubscriptionIds(rows, new Set(['g', 'd'])).sort()).toEqual(['a', 'b', 'd']);
  expect(selectedSubscriptionIds(rows, new Set())).toEqual([]);
});
