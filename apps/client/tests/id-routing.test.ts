import { createMemoryHistory, createRootRoute, createRoute, createRouter } from '@tanstack/react-router';
import { channelTarget, videoTarget } from '../src/lib/link-targets';
import { expect, test } from 'bun:test';

// Everything is addressed by PocketBase record ids: 15 plain characters, whatever the platform (a PeerTube video's
// native id, `framatube.org:<uuid>`, or channel's, `name@framatube.org`, never reaches a URL).
const VIDEO_ID = 'x7k2m9q4w1e8r5t';
const CHANNEL_ID = 'h3j6n0b8v2c4z1a';

const root = createRootRoute();
const player = createRoute({ getParentRoute: () => root, path: '/player/$videoId' });
const channel = createRoute({ getParentRoute: () => root, path: '/channel/$channelId' });
const live = createRoute({ getParentRoute: () => root, path: '/live/$channelId' });
const router = createRouter({ routeTree: root.addChildren([player, channel, live]), history: createMemoryHistory({ initialEntries: ['/'] }) });

/** Builds the link a route would render for these params, then matches that URL back to its params. */
function roundTrip(to: '/player/$videoId' | '/channel/$channelId' | '/live/$channelId', params: Record<string, string>) {
  const { href, pathname } = router.buildLocation({ to, params } as never);
  return { href, params: router.matchRoutes(pathname).at(-1)?.params as Record<string, string> };
}

test('a video record id survives the player route', () => {
  const { href, params } = roundTrip('/player/$videoId', { videoId: VIDEO_ID });
  expect(href).toBe(`/player/${VIDEO_ID}`);
  expect(params).toEqual({ videoId: VIDEO_ID });
});

test('a channel record id survives the channel and live routes', () => {
  for (const to of ['/channel/$channelId', '/live/$channelId'] as const) {
    const { href, params } = roundTrip(to, { channelId: CHANNEL_ID });
    expect(href.split('/')).toHaveLength(3);
    expect(params).toEqual({ channelId: CHANNEL_ID });
  }
});

test('link targets carry record ids untouched', () => {
  expect(videoTarget({ id: VIDEO_ID })).toEqual({ kind: 'record', videoId: VIDEO_ID, series: undefined, source: undefined });
  expect(videoTarget({ id: VIDEO_ID, channelId: CHANNEL_ID, live: true })).toEqual({ kind: 'live', channelId: CHANNEL_ID });
  expect(channelTarget({ id: CHANNEL_ID, platform: 'peertube' })).toEqual({ channelId: CHANNEL_ID, source: undefined, tab: undefined });
});
