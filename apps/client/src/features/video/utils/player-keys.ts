/** What a key does to the player; `none` is a key that is taken (it must not scroll the page) but does nothing now. */
export type PlayerKeyAction =
  | { type: 'toggle' }
  | { type: 'seek'; seconds: number }
  | { type: 'volume'; delta: number }
  | { type: 'mute' }
  | { type: 'fullscreen' }
  | { type: 'next' }
  | { type: 'captions' }
  | { type: 'none' };

export const SEEK_STEP = 5;
export const SEEK_JUMP = 10;
export const VOLUME_STEP = 0.05;

export interface PlayerKeyEvent {
  key: string;
  altKey: boolean;
  ctrlKey: boolean;
  metaKey: boolean;
  repeat?: boolean;
}

/** Where the key was pressed, as far as the player cares. */
export interface PlayerKeyFocus {
  /** The element handles keys itself: a field, a select, a slider, a tab, a menu or list item, an open chapter list. */
  ownsKeys: boolean;
  /** A dialog or a menu is open: the page behind it is not the viewer's business. */
  overlayOpen: boolean;
  /** A button, link or the like: Space belongs to it, unless it is one of the player's own. */
  activatable: boolean;
  /** The element is inside the player frame. */
  inPlayer: boolean;
}

export interface PlayerKeyContext {
  /** The series has an episode after this one. */
  hasNext: boolean;
  /** The video has subtitles or captions that can be shown. */
  hasCaptions: boolean;
}

/** Anything with `closest`, so the rules can run on a DOM element or on a stand-in. */
export interface KeyTarget {
  closest(selector: string): unknown;
}

// The elements that read these keys themselves. The seek bar is a range input the player drives, so it is no exception to the rule.
const OWN_KEYS =
  'input:not([data-player-seek]),select,textarea,[contenteditable]:not([contenteditable="false"]),[role="slider"],[role="tab"],[role="radio"],[role="listbox"],[role="combobox"],[role="menu"],[role="menuitem"],[role="menuitemradio"],[role="dialog"],[role="alertdialog"],[data-chapters]';
const ACTIVATABLE = 'button,a[href],summary,[role="button"],[role="link"],[role="switch"],[role="checkbox"]';
/** An open dialog or menu anywhere on the page. */
export const OPEN_OVERLAY = 'dialog[open],[role="dialog"],[role="alertdialog"],[aria-modal="true"],[role="menu"]';

export function keyFocus(target: KeyTarget | null, { inPlayer, overlayOpen }: { inPlayer: boolean; overlayOpen: boolean }): PlayerKeyFocus {
  return {
    ownsKeys: !!target?.closest(OWN_KEYS),
    overlayOpen,
    activatable: !!target?.closest(ACTIVATABLE),
    inPlayer,
  };
}

/** The player action a key press asks for, or `null` when the key is not the player's (or must not be taken). */
export function playerKeyAction(event: PlayerKeyEvent, focus: PlayerKeyFocus, context: PlayerKeyContext): PlayerKeyAction | null {
  if (event.altKey || event.ctrlKey || event.metaKey || focus.ownsKeys || focus.overlayOpen) {
    return null;
  }
  const once = (action: PlayerKeyAction): PlayerKeyAction => (event.repeat ? { type: 'none' } : action);
  switch (event.key) {
    case ' ':
    case 'Spacebar':
      return focus.activatable && !focus.inPlayer ? null : once({ type: 'toggle' });
    case 'ArrowLeft':
      return { type: 'seek', seconds: -SEEK_STEP };
    case 'ArrowRight':
      return { type: 'seek', seconds: SEEK_STEP };
    case 'ArrowUp':
      return { type: 'volume', delta: VOLUME_STEP };
    case 'ArrowDown':
      return { type: 'volume', delta: -VOLUME_STEP };
    default:
  }
  switch (event.key.toLowerCase()) {
    case 'k':
      return once({ type: 'toggle' });
    case 'j':
      return { type: 'seek', seconds: -SEEK_JUMP };
    case 'l':
      return { type: 'seek', seconds: SEEK_JUMP };
    case 'm':
      return once({ type: 'mute' });
    case 'f':
      return once({ type: 'fullscreen' });
    case 'n':
      return context.hasNext ? once({ type: 'next' }) : null;
    case 'c':
      return context.hasCaptions ? once({ type: 'captions' }) : null;
    default:
      return null;
  }
}
