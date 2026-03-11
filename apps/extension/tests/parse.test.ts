import { parseChannelsPage, parseDayHeader, parseHistoryPage } from '../src/youtube/parse';
import {
  channelId,
  channelsContinuation,
  channelsFirstPage,
  continuationPage,
  historyFirstPage,
  historySection,
  lockupVideo,
  shortsShelf,
  videoId,
  videoRenderer,
} from './youtube';
import { describe, expect, test } from 'bun:test';

// Friday 13 November 2026, 15:00 local time.
const now = new Date(2026, 10, 13, 15, 0);
const local = (month: number, day: number, hours = 0, minutes = 0, year = 2026) => new Date(year, month, day, hours, minutes).toISOString();

describe('parseChannelsPage', () => {
  test('lists the subscribed channels and the next page', () => {
    expect(parseChannelsPage(channelsFirstPage([channelId(1), channelId(2)], 'next'))).toEqual({
      channelIds: [channelId(1), channelId(2)],
      continuation: 'next',
    });
    expect(parseChannelsPage(channelsContinuation([channelId(3)]))).toEqual({ channelIds: [channelId(3)], continuation: undefined });
  });

  test('ignores what is not a channel id', () => {
    expect(parseChannelsPage(channelsFirstPage(['UCshort', 'PLnotachannelnotachannel00'])).channelIds).toEqual([]);
  });
});

describe('parseDayHeader', () => {
  test.each([
    ['Today', new Date(2026, 10, 13)],
    ['Yesterday', new Date(2026, 10, 12)],
    ['Monday', new Date(2026, 10, 9)],
    ['Friday', new Date(2026, 10, 6)],
    ['Nov 5', new Date(2026, 10, 5)],
    ['Dec 24', new Date(2025, 11, 24)],
    ['Sept 30', new Date(2026, 8, 30)],
    ['Nov 5, 2024', new Date(2024, 10, 5)],
  ])('reads %s', (header, day) => {
    expect(parseDayHeader(header, now)).toEqual(day);
  });

  test.each(['Last week', 'Hier', 'Foo 3', 'Nov 40'])('does not guess %s', (header) => {
    expect(parseDayHeader(header, now)).toBeUndefined();
  });
});

describe('parseHistoryPage', () => {
  test('dates each video by its section, keeping the order within the day', () => {
    const cursor = { index: 0 };
    const page = parseHistoryPage(
      historyFirstPage(
        [
          historySection('Today', [videoRenderer(videoId(1)), videoRenderer(videoId(2))]),
          historySection('Yesterday', [videoRenderer(videoId(3)), videoRenderer(videoId(4))]),
        ],
        'older',
      ),
      now,
      cursor,
    );
    expect(page.continuation).toBe('older');
    expect(page.entries.map(({ videoId, watchedAt }) => [videoId, watchedAt])).toEqual([
      [videoId(1), local(10, 13, 15, 0)],
      [videoId(2), local(10, 13, 14, 59)],
      [videoId(3), local(10, 12, 23, 59)],
      [videoId(4), local(10, 12, 23, 58)],
    ]);
  });

  test('carries the day over to the next page when it continues the same section', () => {
    const cursor = { index: 0 };
    parseHistoryPage(historyFirstPage([historySection('Yesterday', [videoRenderer(videoId(1))])], 'next'), now, cursor);
    const next = parseHistoryPage(continuationPage([historySection(undefined, [videoRenderer(videoId(2))])]), now, cursor);
    expect(next.entries[0]?.watchedAt).toBe(local(10, 12, 23, 58));
  });

  test('reads progress and length from classic, lockup and Shorts renderers', () => {
    const page = parseHistoryPage(
      historyFirstPage([
        historySection('Today', [
          videoRenderer(videoId(1), '10:00', 50),
          lockupVideo(videoId(2), '1:02:03', 95),
          videoRenderer(videoId(3), '4:00'),
          shortsShelf([videoId(4)]),
        ]),
      ]),
      now,
      { index: 0 },
    );
    expect(page.entries.map(({ watchedAt: _, ...entry }) => entry)).toEqual([
      { videoId: videoId(1), duration: 600, position: 300, watched: false },
      { videoId: videoId(2), duration: 3723, position: 3537, watched: true },
      { videoId: videoId(3), duration: 240 },
      { videoId: videoId(4) },
    ]);
  });

  test('skips playlists, channel links and repeated videos', () => {
    const playlist = { lockupViewModel: { contentId: 'PLxxxxxxxxxxxxxxxx', contentType: 'LOCKUP_CONTENT_TYPE_PLAYLIST' } };
    const page = parseHistoryPage(historyFirstPage([historySection('Today', [playlist, videoRenderer(videoId(1)), videoRenderer(videoId(1))])]), now, {
      index: 0,
    });
    expect(page.entries.map((entry) => entry.videoId)).toEqual([videoId(1)]);
  });
});
