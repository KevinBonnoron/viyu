import { type DependencyList, type RefObject, useLayoutEffect, useState } from 'react';

/** The smallest player height, in pixels, that still leaves room for the page footer: below it the page is allowed to scroll instead. */
const MIN_FULL_PAGE_PLAYER_HEIGHT = 240;

/**
 * Sizes the player frame: its height is capped so the video, the information row below it and (when it fits) the page
 * footer all fit in the window. Nothing is measured while the frame is `fullscreen`. `relayoutOn` lists what changes the
 * height of the content around the frame (details loaded, a notice shown) and so asks for a new measure.
 */
export function usePlayerLayout(
  frameRef: RefObject<HTMLElement | null>,
  informationRef: RefObject<HTMLElement | null>,
  fullscreen: boolean,
  relayoutOn: DependencyList,
) {
  const [maxHeight, setMaxHeight] = useState<number>();

  useLayoutEffect(() => {
    const frame = frameRef.current;
    const information = informationRef.current;
    if (!frame || !information || fullscreen) {
      return;
    }
    const update = () => {
      const bounds = frame.getBoundingClientRect();
      const reserve = information.getBoundingClientRect().bottom - bounds.bottom;
      const available = window.innerHeight - (bounds.top + window.scrollY) - reserve - 16;
      // The page keeps its footer and bottom padding below the information row: leave room for them so a video that fills
      // the window does not make the page scroll, unless that would leave the video too small (short landscape screens).
      const main = document.getElementById('main-content');
      const footer = main?.querySelector(':scope > footer');
      const below =
        main && footer
          ? footer.getBoundingClientRect().height +
            Number.parseFloat(getComputedStyle(footer).marginTop) +
            Number.parseFloat(getComputedStyle(main).paddingBottom) -
            16
          : 0;
      const height = Math.max(120, available - (available - below >= MIN_FULL_PAGE_PLAYER_HEIGHT ? below : 0));
      // Only the height is capped: the frame keeps the width of its column, so the information row below lines up with the
      // frame's right edge, and a video narrower than the frame is letterboxed (object-contain on black), as on YouTube.
      frame.style.width = '100%';
      frame.style.marginInline = '0';
      setMaxHeight(height);
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(information);
    if (frame.parentElement) {
      observer.observe(frame.parentElement);
    }
    window.addEventListener('resize', update);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', update);
    };
  }, [fullscreen, frameRef, informationRef, ...relayoutOn]);

  return maxHeight;
}

/** Available page width that leaves room for the 22rem panel, its rail and a usable video column. */
const MIN_BESIDE_WIDTH = 960;

/** Keep details beside the video on wider page columns, regardless of window height or scroll position. */
export function decideBeside(contentWidth: number): boolean {
  return contentWidth >= MIN_BESIDE_WIDTH;
}

/** `beside` or `stacked` for the player root, measured on the element and kept up to date as it or the window resizes. */
export function useSidePanelPlacement(rootRef: RefObject<HTMLElement | null>): 'beside' | 'stacked' {
  const [placement, setPlacement] = useState<'beside' | 'stacked'>('stacked');
  useLayoutEffect(() => {
    const root = rootRef.current;
    if (!root) {
      return;
    }
    const update = () => {
      setPlacement(decideBeside(root.getBoundingClientRect().width) ? 'beside' : 'stacked');
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(root);
    return () => {
      observer.disconnect();
    };
  }, [rootRef]);
  return placement;
}
