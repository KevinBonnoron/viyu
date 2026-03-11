import { limits, type SyncDeps, synchronize } from '../src/sync';
import type { HistoryEntry } from '../src/youtube/parse';
import { channelId, channelsContinuation, channelsFirstPage, continuationPage, historyFirstPage, historySection, videoId, videoRenderer } from './youtube';
import { afterEach, describe, expect, test } from 'bun:test';

const defaults = { ...limits };
afterEach(() => Object.assign(limits, defaults));

/** A YouTube that answers browse requests from fixed pages, and a Viyu that records what it receives. */
function world(pages: Record<string, unknown>, known: Set<string> = new Set()) {
  const pushed: { subscriptions: { channels: string[]; complete: boolean }[]; history: HistoryEntry[][] } = { subscriptions: [], history: [] };
  const deps: SyncDeps = {
    browse: async (request) => {
      const key = request.browseId ?? request.continuation ?? '';
      if (!(key in pages)) {
        throw new Error(`unexpected request ${key}`);
      }
      return pages[key];
    },
    pushSubscriptions: async (channels, complete) => {
      pushed.subscriptions.push({ channels, complete });
      return { created: channels.length, removed: 0, unchanged: 0, skipped: 0 };
    },
    pushHistory: async (entries) => {
      pushed.history.push(entries);
      const created = entries.filter((entry) => !known.has(entry.videoId)).length;
      for (const entry of entries) {
        known.add(entry.videoId);
      }
      return { created, updated: 0, unchanged: entries.length - created, skipped: 0 };
    },
    now: () => new Date(2026, 10, 13, 15, 0),
  };
  return { deps, pushed };
}

const emptyHistory = { FEhistory: historyFirstPage([]) };

describe('synchronize', () => {
  test('sends every page of subscriptions at once, as a complete list', async () => {
    const { deps, pushed } = world({
      FEchannels: channelsFirstPage([channelId(1), channelId(2)], 'p2'),
      p2: channelsContinuation([channelId(2), channelId(3)]),
      ...emptyHistory,
    });
    const report = await synchronize(deps, { historyComplete: false });
    expect(pushed.subscriptions).toEqual([{ channels: [channelId(1), channelId(2), channelId(3)], complete: true }]);
    expect(report.subscriptions).toMatchObject({ total: 3, complete: true });
  });

  test('marks the list incomplete when the page limit cuts it short, so Viyu removes nothing', async () => {
    limits.channelPages = 1;
    const { deps, pushed } = world({ FEchannels: channelsFirstPage([channelId(1)], 'p2'), ...emptyHistory });
    await synchronize(deps, { historyComplete: false });
    expect(pushed.subscriptions).toEqual([{ channels: [channelId(1)], complete: false }]);
  });

  test('a first sync reads the history to its end, sending each video once', async () => {
    const { deps, pushed } = world({
      FEchannels: channelsFirstPage([]),
      FEhistory: historyFirstPage([historySection('Today', [videoRenderer(videoId(1)), videoRenderer(videoId(2))])], 'h2'),
      h2: continuationPage([historySection('Yesterday', [videoRenderer(videoId(1)), videoRenderer(videoId(3))])]),
    });
    const report = await synchronize(deps, { historyComplete: false });
    expect(pushed.history.map((page) => page.map((entry) => entry.videoId))).toEqual([[videoId(1), videoId(2)], [videoId(3)]]);
    expect(report.history).toMatchObject({ created: 3, pages: 2 });
    expect(report.historyComplete).toBe(true);
  });

  test('a later sync stops at the first page that brings nothing new', async () => {
    const known = new Set([videoId(2), videoId(3)]);
    const { deps, pushed } = world(
      {
        FEchannels: channelsFirstPage([]),
        FEhistory: historyFirstPage([historySection('Today', [videoRenderer(videoId(1))])], 'h2'),
        h2: continuationPage([historySection('Yesterday', [videoRenderer(videoId(2))])], 'h3'),
        h3: continuationPage([historySection('Monday', [videoRenderer(videoId(3))])]),
      },
      known,
    );
    const report = await synchronize(deps, { historyComplete: true });
    expect(pushed.history).toHaveLength(2);
    expect(report.history).toMatchObject({ created: 1, unchanged: 1, pages: 2 });
  });

  test('a first sync cut short by the page limit still counts as done, so later ones stay incremental', async () => {
    limits.firstHistoryPages = 1;
    const { deps } = world({ FEchannels: channelsFirstPage([]), FEhistory: historyFirstPage([historySection('Today', [videoRenderer(videoId(1))])], 'h2') });
    expect((await synchronize(deps, { historyComplete: false })).historyComplete).toBe(true);
  });
});
