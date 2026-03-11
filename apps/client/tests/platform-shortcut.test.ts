import { modifierLabelFor } from '../src/lib/platform-shortcut';
import { expect, test } from 'bun:test';

test('Apple platforms use the command key', () => {
  for (const platform of ['MacIntel', 'macOS', 'iPhone', 'iPad']) {
    expect(modifierLabelFor(platform)).toBe('⌘');
  }
});

test('other platforms use Ctrl', () => {
  for (const platform of ['Win32', 'Windows', 'Linux x86_64', 'Android', '']) {
    expect(modifierLabelFor(platform)).toBe('Ctrl');
  }
});
