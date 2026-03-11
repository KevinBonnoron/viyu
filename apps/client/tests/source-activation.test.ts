import { filterEnabledPlatforms, isPlatformEnabled, isSourceActive, searchPlatformOrAll } from '../src/lib/source-activation';
import { describe, expect, test } from 'bun:test';

const sources = [
  { platform: 'youtube', enabled: true },
  { platform: 'twitch', enabled: false },
];

describe('isPlatformEnabled', () => {
  test('follows the activation of a listed platform', () => {
    expect(isPlatformEnabled(sources, 'youtube')).toBe(true);
    expect(isPlatformEnabled(sources, 'twitch')).toBe(false);
  });

  test('a platform the list does not mention is not hidden', () => {
    expect(isPlatformEnabled(sources, 'vimeo')).toBe(true);
    expect(isPlatformEnabled([], 'youtube')).toBe(true);
  });

  test('nothing is hidden while the list is not known', () => {
    expect(isPlatformEnabled(sources, 'twitch', false)).toBe(true);
    expect(isPlatformEnabled([], 'twitch', false)).toBe(true);
  });
});

describe('isSourceActive', () => {
  test('requires both a connection and the activation', () => {
    expect(isSourceActive({ connected: true, enabled: true })).toBe(true);
    expect(isSourceActive({ connected: true, enabled: false })).toBe(false);
    expect(isSourceActive({ connected: false, enabled: true })).toBe(false);
  });
});

describe('filterEnabledPlatforms', () => {
  const items = [{ id: 1, platform: 'youtube' }, { id: 2, platform: 'twitch' }, { id: 3 }, { id: 4, platform: 'youtube' }];
  const enabled = (platform: string) => isPlatformEnabled(sources, platform);

  test('drops the items of a disabled platform and keeps the order of the rest', () => {
    expect(filterEnabledPlatforms(items, (item) => item.platform, enabled).map((item) => item.id)).toEqual([1, 3, 4]);
  });

  test('keeps everything when every platform is enabled or the list is unknown', () => {
    expect(
      filterEnabledPlatforms(
        items,
        (item) => item.platform,
        () => true,
      ),
    ).toHaveLength(4);
    expect(
      filterEnabledPlatforms(
        items,
        (item) => item.platform,
        (platform) => isPlatformEnabled(sources, platform, false),
      ),
    ).toHaveLength(4);
  });
});

describe('searchPlatformOrAll', () => {
  const enabled = (platform: string) => isPlatformEnabled(sources, platform);

  test('keeps all and an enabled platform', () => {
    expect(searchPlatformOrAll('all', enabled)).toBe('all');
    expect(searchPlatformOrAll('youtube', enabled)).toBe('youtube');
  });

  test('falls back to all when the remembered platform is disabled', () => {
    expect(searchPlatformOrAll('twitch', enabled)).toBe('all');
  });

  test('keeps the remembered platform while the list is not known', () => {
    expect(searchPlatformOrAll('twitch', (platform) => isPlatformEnabled([], platform, false))).toBe('twitch');
  });
});
