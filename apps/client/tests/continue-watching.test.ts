import { pickResumable } from '../src/lib/continue-watching';
import type { WatchHistoryRecord } from '../src/types';
import { expect, test } from 'bun:test';

const row = (
  id: string,
  fields: { watched?: boolean; position?: number; platform?: string; channel?: string; expanded?: boolean; subscriberOnly?: boolean } = {},
) =>
  ({
    id,
    video: `v-${id}`,
    watched: fields.watched ?? false,
    position: fields.position ?? 120,
    expand:
      fields.expanded === false
        ? undefined
        : {
            video: { id: `v-${id}`, platform: fields.platform ?? 'youtube', channel: fields.channel ?? 'c1', subscriber_only: fields.subscriberOnly ?? false },
          },
  }) as unknown as WatchHistoryRecord;

const enabled = () => true;
const ids = (rows: readonly { id: string }[]) => rows.map((entry) => entry.id);

test('only started, unfinished videos are offered, in the order given', () => {
  const rows = [row('a'), row('done', { watched: true }), row('fresh', { position: 0 }), row('b')];
  expect(ids(pickResumable(rows, { isEnabled: enabled }))).toEqual(['a', 'b']);
});

test('a row whose video could not be expanded is skipped', () => {
  expect(pickResumable([row('a', { expanded: false })], { isEnabled: enabled })).toEqual([]);
});

test('a video of a turned-off platform is skipped', () => {
  const rows = [row('a', { platform: 'twitch' }), row('b')];
  expect(ids(pickResumable(rows, { isEnabled: (platform) => platform !== 'twitch' }))).toEqual(['b']);
});

test('the channel scope keeps only those channels, and an empty scope keeps nothing', () => {
  const rows = [row('a', { channel: 'c1' }), row('b', { channel: 'c2' })];
  expect(ids(pickResumable(rows, { channelIds: ['c2'], isEnabled: enabled }))).toEqual(['b']);
  expect(pickResumable(rows, { channelIds: [], isEnabled: enabled })).toEqual([]);
  expect(ids(pickResumable(rows, { isEnabled: enabled }))).toEqual(['a', 'b']);
});

test('subscriber-only videos follow the preference without excluding public unfinished videos', () => {
  const rows = [row('restricted', { platform: 'twitch', subscriberOnly: true }), row('public', { platform: 'twitch' })];
  expect(ids(pickResumable(rows, { isEnabled: enabled, hideSubscriberOnly: true }))).toEqual(['public']);
  expect(ids(pickResumable(rows, { isEnabled: enabled, hideSubscriberOnly: false }))).toEqual(['restricted', 'public']);
});
