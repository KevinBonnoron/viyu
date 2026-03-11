import { buildChannelFilters, filterChannels, groupCandidates, matchesChannels, normalizeChannelName, ungroupedDuplicates } from '../src/lib/channel-groups';
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

test('channel names are compared without case, accents or punctuation', () => {
  expect(normalizeChannelName('ZeratoR')).toBe(normalizeChannelName('zerator'));
  expect(normalizeChannelName('Café  Création!')).toBe('cafecreation');
  expect(normalizeChannelName('!!!')).toBe('');
});

test('grouped subscriptions become one filter and lone groups stay plain channels', () => {
  const filters = buildChannelFilters([
    subscription('a', 'twitch', 'zerator', 'g', '2'),
    subscription('b', 'youtube', 'ZeratoR', 'g', '1'),
    subscription('c', 'youtube', 'Solo', 'lonely'),
    subscription('d', 'twitch', 'Plain'),
  ]);
  expect(filters).toHaveLength(3);
  const group = filters[0];
  expect('channels' in group && group.channels.map((channel) => channel.platform)).toEqual(['youtube', 'twitch']);
  expect(group.name).toBe('ZeratoR');
  expect(group.thumbnail).toBe('youtube.png');
  expect(filters.slice(1).every((filter) => !('channels' in filter))).toBe(true);
});

test('a creator filter takes the creator name and falls back on its first channel', () => {
  const [named] = buildChannelFilters([
    subscription('a', 'youtube', 'ZeratoR', 'g', '1', 'Zerator the creator'),
    subscription('b', 'twitch', 'zerator', 'g', '2', 'Zerator the creator'),
  ]);
  expect(named.name).toBe('Zerator the creator');
  expect(named.id).toBe('g');
});

test('a group filter matches videos from both platforms and nothing else', () => {
  const [group, other] = buildChannelFilters([
    subscription('a', 'youtube', 'ZeratoR', 'g'),
    subscription('b', 'twitch', 'zerator', 'g'),
    subscription('c', 'youtube', 'Other'),
  ]);
  const channels = filterChannels(group);
  expect(matchesChannels(channels, { platform: 'youtube', channelId: 'youtube-ZeratoR' })).toBe(true);
  expect(matchesChannels(channels, { platform: 'twitch', channelId: 'twitch-zerator' })).toBe(true);
  expect(matchesChannels(channels, { platform: 'youtube', channelId: 'youtube-Other' })).toBe(false);
  expect(matchesChannels(channels, { platform: 'twitch', channelId: 'twitch-zerator', source: 'private' })).toBe(false);
  expect(matchesChannels(filterChannels(other), { platform: 'youtube', channelId: 'youtube-Other' })).toBe(true);
  expect(matchesChannels(undefined, { platform: 'youtube', channelId: 'anything' })).toBe(true);
});

test('creators the user kept apart can be linked again explicitly, but only when unambiguous', () => {
  const duplicates = ungroupedDuplicates([
    subscription('a', 'youtube', 'ZeratoR', 'alone-a'),
    subscription('b', 'twitch', 'zerator', 'alone-b'),
    subscription('c', 'youtube', 'Done', 'g'),
    subscription('d', 'twitch', 'done', 'g'),
    subscription('e', 'youtube', 'Ambiguous'),
    subscription('f', 'youtube', 'ambiguous'),
    subscription('g', 'twitch', 'ambiguous'),
    subscription('h', 'twitch', 'Alone'),
  ]);
  expect(duplicates.map((duplicate) => duplicate.subscriptions.map((item) => item.id))).toEqual([['a', 'b']]);
  expect(duplicates[0].platforms).toEqual(['youtube', 'twitch']);
});

test('a bundled channel name meets another channel whose whole name is one of its parts', () => {
  const duplicates = ungroupedDuplicates([
    subscription('a', 'youtube', 'Yves Rougy | Yorzian', 'alone-a'),
    subscription('b', 'twitch', 'Yorzian', 'alone-b'),
    // Two unrelated creators that merely share a part stay apart.
    subscription('c', 'youtube', 'Foo | Gaming', 'alone-c'),
    subscription('d', 'twitch', 'Bar | Gaming', 'alone-d'),
    // An exact match on the same name wins over a bundled name containing it.
    subscription('e', 'youtube', 'Someone | Exact'),
    subscription('f', 'youtube', 'Exact'),
    subscription('g', 'twitch', 'exact'),
  ]);
  expect(duplicates.map((duplicate) => duplicate.subscriptions.map((item) => item.id))).toEqual([
    ['a', 'b'],
    ['f', 'g'],
  ]);
});

test('manual grouping offers subscriptions of other platforms outside any group, sorted by name', () => {
  const subscriptions = [
    subscription('a', 'youtube', 'Selected'),
    subscription('b', 'twitch', 'Zed'),
    subscription('c', 'twitch', 'alpha'),
    subscription('d', 'youtube', 'Same platform'),
    subscription('e', 'twitch', 'Grouped', 'g'),
    subscription('f', 'peertube', 'Grouped too', 'g'),
    // A creator with a single channel is not a group.
    subscription('h', 'twitch', 'Lonely creator', 'solo'),
  ];
  expect(groupCandidates(subscriptions, 'youtube', '').map((item) => item.id)).toEqual(['c', 'h', 'b']);
  expect(groupCandidates(subscriptions, 'twitch', '').map((item) => item.id)).toEqual(['d', 'a']);
});

test('manual grouping search ignores case and accents and gives nothing when nothing matches', () => {
  const subscriptions = [subscription('a', 'youtube', 'Selected'), subscription('b', 'twitch', 'Café Création'), subscription('c', 'twitch', 'Other')];
  expect(groupCandidates(subscriptions, 'youtube', '  CAFE crea ').map((item) => item.id)).toEqual(['b']);
  expect(groupCandidates(subscriptions, 'youtube', 'missing')).toEqual([]);
  expect(groupCandidates([subscription('a', 'youtube', 'Only')], 'youtube', '')).toEqual([]);
});
