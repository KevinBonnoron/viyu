import type { HistoryCounts, SubscriptionCounts } from './viyu';
import type { BrowseRequest } from './youtube/innertube';
import { type HistoryCursor, type HistoryEntry, parseChannelsPage, parseHistoryPage } from './youtube/parse';

/** What a synchronization needs from the outside: reading YouTube in the browser, and writing to Viyu. */
export interface SyncDeps {
  browse(request: BrowseRequest): Promise<unknown>;
  pushSubscriptions(channels: string[], complete: boolean): Promise<SubscriptionCounts>;
  pushHistory(entries: HistoryEntry[]): Promise<HistoryCounts>;
  now(): Date;
}

export interface SyncReport {
  subscriptions: SubscriptionCounts & { total: number; complete: boolean };
  history: HistoryCounts & { pages: number };
  /** Whether the history has been read as far back as a first synchronization goes; later ones only read what is new. */
  historyComplete: boolean;
}

/** Bounds of one synchronization, so a huge account or a looping continuation never runs forever. */
export const limits = {
  channelPages: 100,
  firstHistoryPages: 50,
  laterHistoryPages: 10,
};

/**
 * Reads the subscriptions and the watch history of the YouTube account signed in in the browser and sends them to Viyu.
 *
 * The subscriptions are sent once, whole: Viyu removes the ones the companion added and YouTube no longer lists only
 * when the list is complete, i.e. every page was read. The history is sent page by page, newest first. The first
 * synchronization reads up to `firstHistoryPages`; after that, reading stops at the first page that brings Viyu nothing
 * new, since everything older was sent before.
 */
export async function synchronize(deps: SyncDeps, options: { historyComplete: boolean }): Promise<SyncReport> {
  const channels = new Set<string>();
  let request: BrowseRequest | undefined = { browseId: 'FEchannels' };
  let pages = 0;
  while (request && pages < limits.channelPages) {
    const page = parseChannelsPage(await deps.browse(request));
    pages += 1;
    for (const id of page.channelIds) {
      channels.add(id);
    }
    request = page.continuation ? { continuation: page.continuation } : undefined;
  }
  const complete = request === undefined;
  const subscriptions = { ...(await deps.pushSubscriptions([...channels], complete)), total: channels.size, complete };

  const history = { created: 0, updated: 0, unchanged: 0, skipped: 0, pages: 0 };
  const maxPages = options.historyComplete ? limits.laterHistoryPages : limits.firstHistoryPages;
  const now = deps.now();
  const cursor: HistoryCursor = { index: 0 };
  const sent = new Set<string>();
  let reachedEnd = false;
  request = { browseId: 'FEhistory' };
  while (request && history.pages < maxPages) {
    const page = parseHistoryPage(await deps.browse(request), now, cursor);
    history.pages += 1;
    // A video watched on two days appears twice; its most recent viewing comes first and is the one that counts.
    const entries = page.entries.filter((entry) => !sent.has(entry.videoId));
    for (const entry of entries) {
      sent.add(entry.videoId);
    }
    if (entries.length > 0) {
      const counts = await deps.pushHistory(entries);
      history.created += counts.created;
      history.updated += counts.updated;
      history.unchanged += counts.unchanged;
      history.skipped += counts.skipped;
      if (options.historyComplete && counts.created + counts.updated === 0) {
        break;
      }
    }
    request = page.continuation ? { continuation: page.continuation } : undefined;
    reachedEnd = request === undefined;
  }
  return { subscriptions, history, historyComplete: options.historyComplete || reachedEnd || history.pages >= maxPages };
}
