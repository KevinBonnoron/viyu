/** What the timeline needs to know about one feed (a slice of the subscribed channels) that pages newest first. */
export interface TimelineFeed<T> {
  rows: readonly T[];
  hasNextPage: boolean;
}

interface Dated {
  id: string;
  /** PocketBase date ("2026-11-09 07:02:19.123Z"), empty when unknown: strings of this format compare like the instants they stand for. */
  published: string;
}

/** Newest first; an unknown publication last; ties by id so the order is total. */
export function newestFirst(a: Dated, b: Dated): number {
  return b.published.localeCompare(a.published) || a.id.localeCompare(b.id);
}

/**
 * Merges feeds that each page newest first into one newest-first list. A feed with pages left may still hold videos
 * older than the last one it loaded, but never newer: nothing below the newest of those "last loaded" dates is final yet,
 * so only the rows from that date up are returned. `pending` is the index of the feed whose next page lifts that limit
 * (the one that bounds it), or -1 when every feed is complete.
 */
export function mergeTimeline<T extends Dated>(feeds: readonly (TimelineFeed<T> | undefined)[]): { rows: T[]; pending: number } {
  const rows = feeds.flatMap((feed) => feed?.rows ?? []).sort(newestFirst);
  let watermark: string | undefined;
  let pending = -1;
  feeds.forEach((feed, index) => {
    const last = feed?.hasNextPage ? feed.rows.at(-1)?.published : undefined;
    if (last !== undefined && (watermark === undefined || last > watermark)) {
      watermark = last;
      pending = index;
    }
  });
  const limit = watermark;
  return { rows: limit === undefined ? rows : rows.filter((row) => row.published >= limit), pending };
}
