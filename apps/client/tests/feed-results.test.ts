import { mergeFeedVideos } from '../src/lib/feed-results';
import type { VideoResult } from '../src/types';
import { describe, expect, test } from 'bun:test';

const video = (id: string, source?: string, platform = 'youtube'): VideoResult => ({
  id,
  platform,
  source,
  title: id,
  originalTitle: id,
  originalTitleVerified: true,
  originalLanguage: '',
  channelId: `channel-${id}`,
  channelName: id,
  channelThumbnail: '',
  thumbnail: '',
  duration: 60,
  description: '',
  tags: [],
  published: '',
  viewCount: 0,
  likeCount: null,
  dislikeCount: null,
});

describe('home feed merge', () => {
  test('overlapping source pages retain API rank and connected playback attribution', () => {
    const ranked = [video('first'), video('second'), video('third')];
    const result = mergeFeedVideos(ranked, [{ videos: [video('second', 'youtube'), video('remote-only', 'youtube'), video('first', 'youtube')] }]);
    expect(result.map(({ id, source }) => ({ id, source }))).toEqual([
      { id: 'first', source: 'youtube' },
      { id: 'second', source: 'youtube' },
      { id: 'third', source: undefined },
      { id: 'remote-only', source: 'youtube' },
    ]);
  });

  test('pagination and ranked duplicates keep the first card, without collapsing other platforms', () => {
    const first = { ...video('shared', 'youtube'), title: 'First connected copy' };
    const result = mergeFeedVideos(
      [video('shared'), video('shared')],
      [
        { videos: [first, video('one', 'youtube')] },
        { videos: [video('shared', 'youtube'), video('one', 'youtube'), video('two', 'youtube'), video('shared', 'twitch', 'twitch')] },
      ],
    );
    expect(result.map(({ id, platform, title }) => ({ id, platform, title }))).toEqual([
      { id: 'shared', platform: 'youtube', title: 'First connected copy' },
      { id: 'one', platform: 'youtube', title: 'one' },
      { id: 'two', platform: 'youtube', title: 'two' },
      { id: 'shared', platform: 'twitch', title: 'shared' },
    ]);
  });

  test('a profile without local recommendations still sees source pages in order', () => {
    const result = mergeFeedVideos([], [{ videos: [video('newer', 'youtube'), video('older', 'youtube')] }]);
    expect(result.map(({ id, source }) => ({ id, source }))).toEqual([
      { id: 'newer', source: 'youtube' },
      { id: 'older', source: 'youtube' },
    ]);
  });
});
