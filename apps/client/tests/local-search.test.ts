import { LOCAL_CHANNEL_LIMIT, LOCAL_VIDEO_LIMIT, localVideoResult, searchLocalChannels, searchLocalVideos } from '@/lib/local-search';
import type { ChannelRecord, VideoRecord } from '@/types';
import { describe, expect, test } from 'bun:test';

const channel = (id: string, name: string, platform = 'youtube'): ChannelRecord => ({
  id,
  collectionId: 'channels',
  collectionName: 'channels',
  name,
  platform,
  platform_id: id,
  thumbnail: '',
  description: '',
  subscriber_count: 0,
  expires_at: '',
  created: '',
  updated: '',
});

const video = (id: string, title: string, platform = 'youtube', channelId = ''): VideoRecord => ({
  id,
  collectionId: 'videos',
  collectionName: 'videos',
  titles: { original: title },
  metadata: { original_title_verified: true, original_language: '' },
  platform,
  platform_id: id,
  channel: channelId,
  duration: 100,
  description: '',
  view_count: 0,
  tags: [],
  published: '',
  expires_at: '',
  created: '',
  updated: '',
});

describe('searchLocalChannels', () => {
  const channels = [channel('c1', 'Korben'), channel('c2', 'KorbenDetka', 'twitch'), channel('c3', 'mistermv'), channel('c4', 'Defakator')];

  test('matches fuzzy names instantly, best first', () => {
    expect(searchLocalChannels(channels, 'korben').map((c) => c.id)).toEqual(['c1', 'c2']);
    expect(searchLocalChannels(channels, 'korbn')[0]?.id).toBe('c1');
  });

  test('respects the platform filter', () => {
    expect(searchLocalChannels(channels, 'korben', 'twitch').map((c) => c.id)).toEqual(['c2']);
  });

  test('a query shorter than two characters matches nothing', () => {
    expect(searchLocalChannels(channels, 'k')).toEqual([]);
    expect(searchLocalChannels(channels, ' ')).toEqual([]);
  });

  test('caps the number of matches', () => {
    const many = Array.from({ length: LOCAL_CHANNEL_LIMIT + 4 }, (_, i) => channel(`c${i}`, `Korben ${i}`));
    expect(searchLocalChannels(many, 'korben')).toHaveLength(LOCAL_CHANNEL_LIMIT);
  });
});

describe('searchLocalVideos', () => {
  const videos = [
    video('v1', 'POKER + ROGUELITE = OUI | Balatro (découverte)'),
    video('v2', 'Balatro : le roguelite poker addictif'),
    video('v3', 'Autre sujet', 'twitch'),
  ];

  test('matches titles, a title opening with the query first', () => {
    expect(searchLocalVideos(videos, 'balatro').map((v) => v.id)).toEqual(['v2', 'v1']);
  });

  test('respects the platform filter and the limit', () => {
    expect(searchLocalVideos(videos, 'balatro', 'twitch')).toEqual([]);
    const many = Array.from({ length: LOCAL_VIDEO_LIMIT + 3 }, (_, i) => video(`v${i}`, `Balatro ${i}`));
    expect(searchLocalVideos(many, 'balatro')).toHaveLength(LOCAL_VIDEO_LIMIT);
  });
});

describe('localVideoResult', () => {
  test('presents a record like a search answer, with its channel joined', () => {
    const owner = channel('ch1', 'mistermv');
    const result = localVideoResult({ record: video('v1', 'Balatro', 'youtube', 'ch1'), channel: owner });
    expect(result.id).toBe('v1');
    expect(result.channelId).toBe('ch1');
    expect(result.channelName).toBe('mistermv');
    expect(result.originalTitle).toBe('Balatro');
  });

  test('tolerates a missing channel', () => {
    const result = localVideoResult({ record: video('v1', 'Balatro') });
    expect(result.channelId).toBe('');
    expect(result.channelName).toBe('');
  });

  test('reads publication and counts from the record', () => {
    const record = {
      ...video('v1', 'Balatro'),
      published: '2026-11-09 07:02:19.123Z',
      metadata: { like_count: 4, dislike_count: null, original_language: 'fr' },
    };
    const result = localVideoResult({ record });
    expect(result.published).toBe('2026-11-09T07:02:19.123Z');
    expect(result.likeCount).toBe(4);
    expect(result.dislikeCount).toBeNull();
    expect(result.originalLanguage).toBe('fr');
    expect(result.originalTitleVerified).toBe(false);
    expect(localVideoResult({ record: { ...record, published: '2026-11-09 00:00:00.000Z', metadata: null } }).published).toBe('2026-11-09');
  });

  test('presents the translation as the title and keeps the original', () => {
    const result = localVideoResult({ record: { ...video('v1', 'Balatro'), titles: { original: 'Le Balatro', en: 'The Balatro' } } });
    expect(result.title).toBe('The Balatro');
    expect(result.originalTitle).toBe('Le Balatro');
  });
});
