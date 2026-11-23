import { catalogPolling, loadCatalogPrefix } from '../src/features/channel/catalog-prefix';
import type { ChannelPage } from '../src/lib/api';
import type { VideoResult } from '../src/types';
import { expect, test } from 'bun:test';

const video = (id: string) => ({ id, platform: 'youtube' }) as VideoResult;
const page = (number: number, ids: string[], revision: string, status: 'running' | 'complete' = 'complete', hasMore = number === 1): ChannelPage => ({
  channel: null,
  videos: ids.map(video),
  sync: { status, loaded: 4 },
  page: number,
  limit: 2,
  hasMore,
  total: 4,
  revision,
});

test('an unchanged catalog appends only the requested page', async () => {
  const calls: number[] = [];
  const result = await loadCatalogPrefix(2, { page: 1, videos: [video('a'), video('b')], revision: 'same' }, async (n) => {
    calls.push(n);
    return page(n, ['c', 'd'], 'same');
  });
  expect(calls).toEqual([2]);
  expect(result?.prefix.videos.map((v) => v.id)).toEqual(['a', 'b', 'c', 'd']);
});

test('a publication moving before the loaded boundary rebuilds the prefix', async () => {
  const calls: number[] = [];
  const result = await loadCatalogPrefix(2, { page: 1, videos: [video('a'), video('b')], revision: 'old' }, async (n) => {
    calls.push(n);
    return n === 1 ? page(1, ['moved', 'a'], 'new') : page(2, ['b', 'c'], 'new');
  });
  expect(calls).toEqual([2, 1, 2]);
  expect(result?.prefix.videos.map((v) => v.id)).toEqual(['moved', 'a', 'b', 'c']);
});

test('a mid-pass insertion discards inconsistent pages and a settled pass converges', async () => {
  const calls: number[] = [];
  const mixed = await loadCatalogPrefix(2, null, async (n) => {
    calls.push(n);
    return n === 1 ? page(1, ['a', 'b'], 'old', 'running') : page(2, ['b', 'c'], 'new');
  });
  expect(mixed).toBeNull();
  const settled = await loadCatalogPrefix(2, null, async (n) => {
    calls.push(n);
    return n === 1 ? page(1, ['inserted', 'a'], 'new') : page(2, ['b', 'c'], 'new');
  });
  expect(calls).toEqual([1, 2, 1, 2]);
  expect(settled?.prefix.videos.map((v) => v.id)).toEqual(['inserted', 'a', 'b', 'c']);
});

test('completion observed mid-pass requires one subsequent full pass', async () => {
  const calls: number[] = [];
  const pass = await loadCatalogPrefix(2, null, async (n) => {
    calls.push(n);
    return n === 1 ? page(1, ['a', 'b'], 'stable', 'running') : page(2, ['c', 'd'], 'stable');
  });
  expect(pass?.sawSync).toBe(true);
  const polling = catalogPolling(false, pass?.sawSync === true, false);
  expect(polling.crawling).toBe(true);
  const confirmation = await loadCatalogPrefix(2, null, async (n) => {
    calls.push(n);
    return n === 1 ? page(1, ['new', 'a'], 'settled') : page(2, ['b', 'c'], 'settled');
  });
  expect(calls).toEqual([1, 2, 1, 2]);
  expect(confirmation?.prefix.videos.map((v) => v.id)).toEqual(['new', 'a', 'b', 'c']);
  expect(catalogPolling(polling.needsConfirmation, confirmation?.sawSync === true, false).crawling).toBe(false);
});
