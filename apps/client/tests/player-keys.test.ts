import { type KeyTarget, keyFocus, type PlayerKeyContext, type PlayerKeyFocus, playerKeyAction } from '../src/features/video/utils/player-keys';
import { expect, test } from 'bun:test';

const idle: PlayerKeyFocus = { ownsKeys: false, overlayOpen: false, activatable: false, inPlayer: false };
const context: PlayerKeyContext = { hasNext: true, hasCaptions: true };
const press = (key: string, focus: Partial<PlayerKeyFocus> = {}, ctx: Partial<PlayerKeyContext> = {}, modifiers: Record<string, boolean> = {}) =>
  playerKeyAction({ key, altKey: false, ctrlKey: false, metaKey: false, ...modifiers }, { ...idle, ...focus }, { ...context, ...ctx });

test('keys map to player actions', () => {
  expect(press(' ')).toEqual({ type: 'toggle' });
  expect(press('k')).toEqual({ type: 'toggle' });
  expect(press('K')).toEqual({ type: 'toggle' });
  expect(press('ArrowLeft')).toEqual({ type: 'seek', seconds: -5 });
  expect(press('ArrowRight')).toEqual({ type: 'seek', seconds: 5 });
  expect(press('j')).toEqual({ type: 'seek', seconds: -10 });
  expect(press('l')).toEqual({ type: 'seek', seconds: 10 });
  expect(press('ArrowUp')).toEqual({ type: 'volume', delta: 0.05 });
  expect(press('ArrowDown')).toEqual({ type: 'volume', delta: -0.05 });
  expect(press('m')).toEqual({ type: 'mute' });
  expect(press('f')).toEqual({ type: 'fullscreen' });
  expect(press('n')).toEqual({ type: 'next' });
  expect(press('c')).toEqual({ type: 'captions' });
  expect(press('x')).toBeNull();
  expect(press('Enter')).toBeNull();
});

test('next and captions exist only when the video has them, so their letters stay free for the page', () => {
  expect(press('n', {}, { hasNext: false })).toBeNull();
  expect(press('c', {}, { hasCaptions: false })).toBeNull();
});

test('modified keys are never the player’s', () => {
  for (const modifier of ['ctrlKey', 'metaKey', 'altKey']) {
    expect(press('k', {}, {}, { [modifier]: true })).toBeNull();
    expect(press('ArrowLeft', {}, {}, { [modifier]: true })).toBeNull();
    expect(press(' ', {}, {}, { [modifier]: true })).toBeNull();
  }
});

test('a field, a menu, a list or an open dialog keeps its keys', () => {
  expect(press(' ', { ownsKeys: true })).toBeNull();
  expect(press('ArrowLeft', { ownsKeys: true })).toBeNull();
  expect(press('k', { overlayOpen: true })).toBeNull();
  expect(press('ArrowUp', { overlayOpen: true })).toBeNull();
});

test('Space on a button of the page activates it, on a button of the player it toggles playback', () => {
  expect(press(' ', { activatable: true, inPlayer: false })).toBeNull();
  expect(press(' ', { activatable: true, inPlayer: true })).toEqual({ type: 'toggle' });
  expect(press('k', { activatable: true, inPlayer: false })).toEqual({ type: 'toggle' });
});

test('a held key repeats seeks and volume, never a toggle; Space is still taken so the page does not scroll', () => {
  const held = (key: string) => playerKeyAction({ key, altKey: false, ctrlKey: false, metaKey: false, repeat: true }, idle, context);
  expect(held('ArrowRight')).toEqual({ type: 'seek', seconds: 5 });
  expect(held('ArrowDown')).toEqual({ type: 'volume', delta: -0.05 });
  expect(held(' ')).toEqual({ type: 'none' });
  expect(held('f')).toEqual({ type: 'none' });
});

// A stand-in for an element: `closest` answers true when one of the selectors names its tag or role.
const element = (tag: string, attributes: Record<string, string> = {}): KeyTarget => ({
  closest: (selector) =>
    selector.split(/,(?![^[]*\])/).some((part) => {
      const [, name = '', attribute = '', exception = ''] = part.match(/^([a-z]*)(?:\[([^\]]+)\])?(?::not\(\[([^\]]+)\]\))?$/) ?? [];
      if (exception && exception in attributes) {
        return false;
      }
      if (name && name !== tag) {
        return false;
      }
      if (attribute) {
        const [key, value] = attribute.replaceAll('"', '').split('=');
        return value === undefined ? key in attributes : attributes[key] === value;
      }
      return !!name;
    }),
});

test('which elements own their keys, and which are activatable', () => {
  const place = { inPlayer: false, overlayOpen: false };
  expect(keyFocus(element('input', { type: 'text' }), place).ownsKeys).toBe(true);
  expect(keyFocus(element('textarea'), place).ownsKeys).toBe(true);
  expect(keyFocus(element('select'), place).ownsKeys).toBe(true);
  expect(keyFocus(element('div', { role: 'menu' }), place).ownsKeys).toBe(true);
  expect(keyFocus(element('div', { contenteditable: 'true' }), place).ownsKeys).toBe(true);
  expect(keyFocus(element('input', { 'data-player-seek': '' }), place).ownsKeys).toBe(false);
  expect(keyFocus(element('video'), place).ownsKeys).toBe(false);
  expect(keyFocus(null, place).ownsKeys).toBe(false);
  expect(keyFocus(element('button'), place).activatable).toBe(true);
  expect(keyFocus(element('video'), place).activatable).toBe(false);
  expect(keyFocus(element('button'), { inPlayer: true, overlayOpen: true })).toEqual({ ownsKeys: false, overlayOpen: true, activatable: true, inPlayer: true });
});
