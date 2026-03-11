import { defaultRangeExtractor, useWindowVirtualizer } from '@tanstack/react-virtual';
import { type Key, type KeyboardEvent, type ReactNode, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';

const focusable = 'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** How many rows before the last one the grid asks for more. */
const END_REACHED_ROWS = 2;

export function VirtualGrid<T>({
  items,
  getKey,
  renderItem,
  className,
  asList = false,
  onEndReached,
}: {
  items: readonly T[];
  getKey: (item: T, index: number) => Key;
  renderItem: (item: T, index: number) => ReactNode;
  className?: string;
  asList?: boolean;
  /** Called when the last rows come into view; pass it only while more can be loaded (and none is loading), `undefined` otherwise. */
  onEndReached?: () => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  const probe = useRef<HTMLDivElement>(null);
  const [layout, setLayout] = useState({ columns: 1, gap: 0, offset: 0, width: 0 });
  const [focusedRow, setFocusedRow] = useState<number | null>(null);
  const rowCount = Math.ceil(items.length / layout.columns);
  const virtualizer = useWindowVirtualizer({
    count: rowCount,
    estimateSize: () => (asList ? 116 : 320) + layout.gap,
    overscan: 3,
    scrollMargin: layout.offset,
    getItemKey: (row) => getKey(items[row * layout.columns], row * layout.columns),
    rangeExtractor: useCallback(
      (range: Parameters<typeof defaultRangeExtractor>[0]) => {
        const rows = defaultRangeExtractor(range);
        if (focusedRow !== null && focusedRow < range.count && !rows.includes(focusedRow)) {
          rows.push(focusedRow);
          rows.sort((a, b) => a - b);
        }
        return rows;
      },
      [focusedRow],
    ),
  });

  const lastVisibleRow = virtualizer.getVirtualItems().at(-1)?.index ?? -1;
  const endReached = useRef(onEndReached);
  endReached.current = onEndReached;
  const wantsMore = onEndReached !== undefined;
  useEffect(() => {
    if (wantsMore && rowCount > 0 && lastVisibleRow >= rowCount - 1 - END_REACHED_ROWS) {
      endReached.current?.();
    }
  }, [wantsMore, lastVisibleRow, rowCount]);

  useLayoutEffect(() => {
    const element = root.current;
    const sample = probe.current;
    if (!element || !sample) {
      return;
    }
    const update = () => {
      const style = getComputedStyle(sample);
      const columns = style.display === 'grid' ? Math.max(1, style.gridTemplateColumns.split(' ').filter(Boolean).length) : 1;
      const firstMargin = sample.children[0] ? Number.parseFloat(getComputedStyle(sample.children[0]).marginBottom) || 0 : 0;
      const secondMargin = sample.children[1] ? Number.parseFloat(getComputedStyle(sample.children[1]).marginTop) || 0 : 0;
      const gap = Math.max(Number.parseFloat(style.rowGap) || 0, firstMargin + secondMargin);
      const offset = element.getBoundingClientRect().top + window.scrollY;
      const width = element.getBoundingClientRect().width;
      setLayout((previous) =>
        previous.columns === columns && previous.gap === gap && previous.offset === offset && previous.width === width
          ? previous
          : { columns, gap, offset, width },
      );
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(element);
    observer.observe(document.body);
    if (element.parentElement) {
      observer.observe(element.parentElement);
    }
    window.addEventListener('resize', update);
    window.addEventListener('scroll', update, { passive: true });
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', update);
      window.removeEventListener('scroll', update);
    };
  }, []);

  const handleKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== 'Tab' || event.altKey || event.ctrlKey || event.metaKey) {
      return;
    }
    const target = event.target as HTMLElement;
    const row = target.closest<HTMLElement>('[data-virtual-row]');
    if (!row || !root.current?.contains(row)) {
      return;
    }
    const index = Number(row.dataset.index);
    const controls = Array.from(row.querySelectorAll<HTMLElement>(focusable)).filter((control) => control.getClientRects().length > 0);
    const backward = event.shiftKey;
    if (target !== controls[backward ? 0 : controls.length - 1]) {
      return;
    }
    const next = index + (backward ? -1 : 1);
    if (next < 0 || next >= rowCount || root.current.querySelector(`[data-virtual-row][data-index="${next}"]`)) {
      return;
    }
    event.preventDefault();
    virtualizer.scrollToIndex(next, { align: backward ? 'end' : 'start' });
    const focusNext = (attempt: number) => {
      const nextRow = root.current?.querySelector<HTMLElement>(`[data-virtual-row][data-index="${next}"]`);
      const nextControls = Array.from(nextRow?.querySelectorAll<HTMLElement>(focusable) ?? []).filter((control) => control.getClientRects().length > 0);
      const control = nextControls[backward ? nextControls.length - 1 : 0];
      if (control) {
        control.focus();
      } else if (attempt < 10) {
        requestAnimationFrame(() => focusNext(attempt + 1));
      }
    };
    requestAnimationFrame(() => focusNext(0));
  };

  return (
    /* biome-ignore lint/a11y/noStaticElementInteractions: Delegates keyboard focus between virtualized rows. */
    <div
      ref={root}
      role={asList ? 'list' : undefined}
      onKeyDown={handleKeyDown}
      onFocusCapture={(event) => {
        const row = (event.target as HTMLElement).closest<HTMLElement>('[data-virtual-row]');
        if (row && event.currentTarget.contains(row)) {
          setFocusedRow(Number(row.dataset.index));
        }
      }}
      onBlurCapture={(event) => {
        if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
          setFocusedRow(null);
        }
      }}
      style={{ position: 'relative', height: virtualizer.getTotalSize() }}
    >
      <div ref={probe} className={className} aria-hidden="true" style={{ position: 'absolute', visibility: 'hidden', width: '100%', pointerEvents: 'none' }}>
        <div />
        <div />
      </div>
      {virtualizer.getVirtualItems().map((row) => {
        const start = row.index * layout.columns;
        return (
          <div
            key={row.key}
            data-index={row.index}
            data-virtual-row=""
            ref={virtualizer.measureElement}
            role={asList ? 'presentation' : undefined}
            style={{
              position: 'absolute',
              top: 0,
              left: 0,
              width: '100%',
              transform: `translateY(${row.start - layout.offset}px)`,
              paddingBottom: row.index === rowCount - 1 ? 0 : layout.gap,
            }}
          >
            <div className={className}>
              {items.slice(start, start + layout.columns).map((item, offset) => {
                const index = start + offset;
                return asList ? (
                  /* biome-ignore lint/a11y/useSemanticElements: Virtual rows require presentational wrappers within the ARIA list. */
                  <div key={getKey(item, index)} style={{ minWidth: 0 }} role="listitem" aria-posinset={index + 1} aria-setsize={items.length}>
                    {renderItem(item, index)}
                  </div>
                ) : (
                  <div key={getKey(item, index)} style={{ minWidth: 0 }}>
                    {renderItem(item, index)}
                  </div>
                );
              })}
            </div>
          </div>
        );
      })}
    </div>
  );
}
