import { canConnectWithCookies, platformName, platformPresentation, sourceStatus } from '../src/lib/platforms';
import { en } from '../src/providers/translations/en';
import { fr } from '../src/providers/translations/fr';
import { describe, expect, test } from 'bun:test';

describe('platformPresentation', () => {
  test('each known platform has its name, tone, icon and description key', () => {
    expect(platformPresentation('youtube')).toEqual({
      name: 'YouTube',
      tone: 'red',
      icon: 'youtube',
      descriptionKey: 'sources.youtube.description',
      publicAccess: false,
    });
    expect(platformPresentation('twitch')).toEqual({
      name: 'Twitch',
      tone: 'violet',
      icon: 'twitch',
      descriptionKey: 'sources.twitch.description',
      publicAccess: true,
    });
    expect(platformPresentation('peertube')).toEqual({
      name: 'PeerTube',
      tone: 'orange',
      icon: 'peertube',
      descriptionKey: 'sources.peertube.description',
      publicAccess: true,
    });
  });

  test('an unknown platform is neutral, named by its id, and has no description key of its own', () => {
    expect(platformPresentation('dailymotion')).toEqual({ name: 'dailymotion', tone: 'neutral', icon: 'generic', publicAccess: false });
    expect(platformPresentation('dailymotion').descriptionKey).toBeUndefined();
  });

  test('every description key of a known platform is translated in both languages', () => {
    for (const platform of ['youtube', 'twitch', 'peertube']) {
      const key = platformPresentation(platform).descriptionKey;
      expect(key).toBeDefined();
      expect(en[key as keyof typeof en]).toBeTruthy();
      expect(fr[key as keyof typeof fr]).toBeTruthy();
    }
  });

  test('names of the object prototype are not platforms', () => {
    expect(platformPresentation('constructor')).toEqual({ name: 'constructor', tone: 'neutral', icon: 'generic', publicAccess: false });
    expect(platformPresentation('toString').tone).toBe('neutral');
  });

  test('platformName is the presentation name', () => {
    expect(platformName('peertube')).toBe('PeerTube');
    expect(platformName('mystery')).toBe('mystery');
  });
});

describe('source connection', () => {
  test('only a platform offering the cookie session can be connected', () => {
    expect(canConnectWithCookies({ connectionMethods: ['cookies'] })).toBe(true);
    expect(canConnectWithCookies({ connectionMethods: [] })).toBe(false);
  });

  test('a connected session shows as connected', () => {
    expect(sourceStatus({ platform: 'youtube', connected: true, connectionMethods: ['cookies'] })).toBe('connected');
    expect(sourceStatus({ platform: 'twitch', connected: true, connectionMethods: ['cookies'] })).toBe('connected');
  });

  test('a platform without account connection is public access', () => {
    expect(sourceStatus({ platform: 'peertube', connected: false, connectionMethods: [] })).toBe('public');
    expect(sourceStatus({ platform: 'mystery', connected: false, connectionMethods: [] })).toBe('public');
  });

  test('an optional connection is public access until connected; a required one is not connected', () => {
    expect(sourceStatus({ platform: 'twitch', connected: false, connectionMethods: ['cookies'] })).toBe('public');
    expect(sourceStatus({ platform: 'youtube', connected: false, connectionMethods: ['cookies'] })).toBe('notConnected');
    expect(sourceStatus({ platform: 'mystery', connected: false, connectionMethods: ['cookies'] })).toBe('notConnected');
  });
});
