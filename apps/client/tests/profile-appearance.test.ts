import { DEFAULT_PROFILE_ICON, isProfileIcon, PROFILE_ICON_KEYS, PROFILE_ICONS, profileIcon, profileTheme } from '../src/lib/profile-appearance';
import { expect, test } from 'bun:test';

test('a stored icon key resolves to its icon and anything else to the default', () => {
  expect(profileIcon('rocket')).toBe(PROFILE_ICONS.rocket);
  expect(profileIcon(undefined)).toBe(PROFILE_ICONS[DEFAULT_PROFILE_ICON]);
  expect(profileIcon('')).toBe(PROFILE_ICONS[DEFAULT_PROFILE_ICON]);
  expect(profileIcon('removed-icon')).toBe(PROFILE_ICONS[DEFAULT_PROFILE_ICON]);
  // Inherited object keys are not icons.
  expect(isProfileIcon('constructor')).toBe(false);
  expect(isProfileIcon('toString')).toBe(false);
});

test('icon keys match the server pattern so any of them can be saved', () => {
  for (const key of PROFILE_ICON_KEYS) {
    expect(key).toMatch(/^[a-z0-9-]{1,32}$/);
  }
});

test('only known palettes are themes; an empty or unknown value keeps the device palette', () => {
  expect(profileTheme('rose')).toBe('rose');
  expect(profileTheme('')).toBeUndefined();
  expect(profileTheme(undefined)).toBeUndefined();
  expect(profileTheme('neon')).toBeUndefined();
});
