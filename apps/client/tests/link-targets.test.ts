import { channelTarget, videoTarget } from '../src/lib/link-targets';
import { describe, expect, test } from 'bun:test';

describe('videoTarget', () => {
  test('a video opens by its record id and keeps the series context', () => {
    expect(videoTarget({ id: 'rec1' }, { series: 'ser1' })).toEqual({ kind: 'record', videoId: 'rec1', series: 'ser1', source: undefined });
    expect(videoTarget({ id: 'rec1' })).toEqual({ kind: 'record', videoId: 'rec1', series: undefined, source: undefined });
  });

  test('a stream in progress opens on its channel live page', () => {
    expect(videoTarget({ id: 'rec1', channelId: 'chan1', live: true })).toEqual({ kind: 'live', channelId: 'chan1' });
  });

  test('a stream without a channel opens on its record', () => {
    expect(videoTarget({ id: 'rec1', live: true })).toEqual({ kind: 'record', videoId: 'rec1', series: undefined, source: undefined });
  });

  test('a connected source opens the same record page and carries the source to read it through', () => {
    expect(videoTarget({ id: 'rec1', channelId: 'chan1', live: true }, { source: 'twitch' })).toEqual({
      kind: 'record',
      videoId: 'rec1',
      series: undefined,
      source: 'twitch',
    });
    expect(videoTarget({ id: 'rec1', source: 'twitch' }, { series: 'ser1' })).toEqual({ kind: 'record', videoId: 'rec1', series: 'ser1', source: 'twitch' });
  });
});

describe('channelTarget', () => {
  test('a channel opens by its record id, on the requested tab', () => {
    expect(channelTarget({ id: 'chan1', platform: 'youtube' })).toEqual({ channelId: 'chan1', source: undefined, tab: undefined });
    expect(channelTarget({ id: 'chan1', platform: 'youtube' }, 'series')).toEqual({ channelId: 'chan1', source: undefined, tab: 'series' });
    expect(channelTarget({ id: 'chan1', platform: 'youtube' }, 'videos')).toEqual({ channelId: 'chan1', source: undefined, tab: undefined });
  });

  test('a channel of a connected source opens the same page with its source', () => {
    expect(channelTarget({ id: 'chan1', platform: 'twitch', source: 'twitch' })).toEqual({ channelId: 'chan1', source: 'twitch', tab: undefined });
  });

  test('YouTube has no connected source for channel pages', () => {
    expect(channelTarget({ id: 'chan1', platform: 'youtube', source: 'youtube' })).toEqual({ channelId: 'chan1', source: undefined, tab: undefined });
  });
});
