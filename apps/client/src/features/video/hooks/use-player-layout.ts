import { type DependencyList, type RefObject, useEffect, useLayoutEffect, useState } from 'react';

/** The smallest player height, in pixels, that still leaves room for the page footer: below it the page is allowed to scroll instead. */
const MIN_FULL_PAGE_PLAYER_HEIGHT = 240;

/**
 * Sizes the player frame: its height is capped so the video, the information row below it and (when it fits) the page
 * footer all fit in the window, and whether the frame is the fullscreen element. `relayoutOn` lists what changes the height
 * of the content around the frame (details loaded, a notice shown) and so asks for a new measure.
 */
export function usePlayerLayout(frameRef: RefObject<HTMLElement | null>, informationRef: RefObject<HTMLElement | null>, relayoutOn: DependencyList) {
  const [maxHeight, setMaxHeight] = useState<number>();
  const [fullscreen, setFullscreen] = useState(false);

  useEffect(() => {
    const update = () => setFullscreen(document.fullscreenElement === frameRef.current);
    document.addEventListener('fullscreenchange', update);
    return () => document.removeEventListener('fullscreenchange', update);
  }, [frameRef]);

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

  return { maxHeight, fullscreen };
}

/** Height kept below the video for the channel and actions row and its spacing, which the frame never takes. */
const INFORMATION_ROW = 170;
/** Width of the side panel beside the video (22rem) plus the gap, and the narrowest column the video may keep beside it. */
const SIDE_PANEL_SPACE = 352 + 16;
const MIN_VIDEO_BESIDE = 640;

/**
 * Whether the side panel goes beside the video, decided from the space the page really gives the player (its own width and
 * the window height below it), not from the viewport: browser chrome, a sidebar or a zoom level then change nothing. The
 * panel goes beside when the video keeps at least the width the available height lets a 16:9 frame take (so moving the panel
 * aside costs the video nothing) and never drops below a usable width.
 */
export function decideBeside(contentWidth: number, availableHeight: number): boolean {
  const besideWidth = contentWidth - SIDE_PANEL_SPACE;
  if (besideWidth < MIN_VIDEO_BESIDE) {
    return false;
  }
  const widthTheHeightAllows = (availableHeight * 16) / 9;
  return besideWidth >= Math.min(widthTheHeightAllows, contentWidth) * 0.9;
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
      const bounds = root.getBoundingClientRect();
      const available = window.innerHeight - Math.max(0, bounds.top) - INFORMATION_ROW;
      setPlacement(decideBeside(bounds.width, available) ? 'beside' : 'stacked');
    };
    update();
    const observer = new ResizeObserver(update);
    observer.observe(root);
    window.addEventListener('resize', update);
    return () => {
      observer.disconnect();
      window.removeEventListener('resize', update);
    };
  }, [rootRef]);
  return placement;
}
