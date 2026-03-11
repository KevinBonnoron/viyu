import type { Source } from '@/types';

type Activation = Pick<Source, 'platform' | 'enabled'>;

/**
 * Whether content of `platform` may be shown. Only a source the server says is off hides anything: while the list is not
 * known (`known` false: still loading or failed) or does not mention the platform, nothing is hidden.
 */
export function isPlatformEnabled(sources: readonly Activation[], platform: string, known = true): boolean {
  if (!known) {
    return true;
  }
  return sources.find((source) => source.platform === platform)?.enabled !== false;
}

/** A source feeds the app with an authenticated session only when it is both connected and enabled. */
export function isSourceActive(source: Pick<Source, 'connected' | 'enabled'>): boolean {
  return source.connected && source.enabled;
}

/** Keeps the items whose platform is enabled; an item without a platform is kept (it is not known to be disabled). */
export function filterEnabledPlatforms<T>(items: readonly T[], platformOf: (item: T) => string | undefined, isEnabled: (platform: string) => boolean): T[] {
  return items.filter((item) => {
    const platform = platformOf(item);
    return platform === undefined || isEnabled(platform);
  });
}

/** The platform a search targets: a disabled one (a stale choice) falls back to every platform. */
export function searchPlatformOrAll(platform: string, isEnabled: (platform: string) => boolean): string {
  return platform === 'all' || isEnabled(platform) ? platform : 'all';
}
