import { type ReactNode, useMemo } from 'react';
import { VirtualGrid } from '@/components/virtual-grid';

/**
 * The grid every video list is rendered with: items laid out in the shared `media-grid` columns and virtualized. The same
 * item may legitimately appear twice in a list (a stream also stored as a video), so each item's React key is its identity
 * made unique by its occurrence number.
 */
export function MediaGrid<T>({
  items,
  identity,
  renderItem,
  onEndReached,
}: {
  items: readonly T[];
  identity: (item: T) => string;
  renderItem: (item: T, index: number) => ReactNode;
  onEndReached?: () => void;
}) {
  const keyed = useMemo(() => {
    const occurrences = new Map<string, number>();
    return items.map((item) => {
      const base = identity(item);
      const occurrence = occurrences.get(base) ?? 0;
      occurrences.set(base, occurrence + 1);
      return { item, key: `${base}:${occurrence}` };
    });
  }, [items, identity]);
  return (
    <VirtualGrid
      items={keyed}
      className="media-grid"
      getKey={(entry) => entry.key}
      renderItem={(entry, index) => renderItem(entry.item, index)}
      onEndReached={onEndReached}
    />
  );
}
