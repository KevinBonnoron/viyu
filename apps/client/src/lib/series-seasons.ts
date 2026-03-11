/** A run of a series' episodes that belong to one season; `season` is null for episodes without one. */
export interface SeasonBlock<T> {
  season: number | null;
  /** `position` is the episode's 1-based place in the whole series, whatever the season. */
  items: { item: T; position: number }[];
}

/**
 * Splits an ordered series into one block per season. It only does so when the series names a
 * season at all and each season is one uninterrupted run: otherwise the order is publication order
 * and cutting it into blocks would reorder what plays next, so the series stays a single block.
 */
export function groupBySeason<T extends { season: number | null }>(items: T[]): SeasonBlock<T>[] {
  const blocks: SeasonBlock<T>[] = [];
  const seen = new Set<number | null>();
  let interrupted = false;
  items.forEach((item, index) => {
    const last = blocks[blocks.length - 1];
    if (last && last.season === item.season) {
      last.items.push({ item, position: index + 1 });
      return;
    }
    if (seen.has(item.season)) {
      interrupted = true;
    }
    seen.add(item.season);
    blocks.push({ season: item.season, items: [{ item, position: index + 1 }] });
  });
  if (interrupted || !items.some((item) => item.season !== null)) {
    return [{ season: null, items: items.map((item, index) => ({ item, position: index + 1 })) }];
  }
  return blocks;
}
