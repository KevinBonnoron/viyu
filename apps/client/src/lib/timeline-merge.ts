/** What the timeline needs to know about one paginated feed. */
export interface TimelineFeed<T> {
  rows: readonly T[];
  hasNextPage: boolean;
}

interface Dated {
  id: string;
  /** PocketBase date ("2026-11-09 07:02:19.123Z"), empty when unknown: strings of this format compare like the instants they stand for. */
  published: string;
}

function compareRows(a: Dated, b: Dated, order: 'asc' | 'desc'): number {
  if (!a.published && b.published) {
    return 1;
  }
  if (a.published && !b.published) {
    return -1;
  }
  const publication = a.published < b.published ? -1 : a.published > b.published ? 1 : 0;
  return (order === 'asc' ? publication : -publication) || (a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
}

export function mergeTimeline<T extends Dated>(
  feeds: readonly (TimelineFeed<T> | undefined)[],
  order: 'asc' | 'desc' = 'desc',
): { rows: T[]; pending: number } {
  const compare = (a: T, b: T) => compareRows(a, b, order);
  const rows = feeds.flatMap((feed) => feed?.rows ?? []).sort(compare);
  let watermark: T | undefined;
  let pending = -1;
  feeds.forEach((feed, index) => {
    const last = feed?.hasNextPage ? feed.rows.at(-1) : undefined;
    if (last !== undefined && (watermark === undefined || compare(last, watermark) < 0)) {
      watermark = last;
      pending = index;
    }
  });
  const limit = watermark;
  return { rows: limit === undefined ? rows : rows.filter((row) => compare(row, limit) <= 0), pending };
}
