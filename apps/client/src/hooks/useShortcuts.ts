import { useEffect, useRef } from 'react';

/**
 * Global single-letter shortcuts, case-insensitive. Typing in a field, a select or a contenteditable element never
 * triggers one, and neither do modified keys or a key something else already took (the player's, say). The latest
 * handler map is used, so callers can build it inline.
 *
 * The visual counterpart lives in `TooltipLayer`: an element with `title` and `data-shortcut="<letter>"` shows the
 * key in its tooltip as a colored hint.
 */
export function useShortcuts(shortcuts: Record<string, () => void>, enabled = true): void {
  const latest = useRef(shortcuts);
  useEffect(() => {
    latest.current = shortcuts;
  }, [shortcuts]);
  useEffect(() => {
    if (!enabled) {
      return;
    }
    const listener = (event: KeyboardEvent) => {
      if (event.defaultPrevented || event.altKey || event.ctrlKey || event.metaKey || event.key.length !== 1) {
        return;
      }
      const target = event.target;
      if (target instanceof HTMLElement && target.closest('input, textarea, select, [contenteditable=""], [contenteditable="true"]')) {
        return;
      }
      const handler = latest.current[event.key.toLowerCase()];
      if (!handler) {
        return;
      }
      event.preventDefault();
      handler();
    };
    window.addEventListener('keydown', listener);
    return () => {
      window.removeEventListener('keydown', listener);
    };
  }, [enabled]);
}
