import type { TranslationKey } from '../providers/translations/en';

/** The colour family a platform is shown in: a brand tone, or `neutral` for a platform the client does not know. */
export type PlatformTone = 'red' | 'violet' | 'orange' | 'neutral';

/** Which drawing stands for a platform; the components map it to an actual icon. */
export type PlatformIconName = 'youtube' | 'twitch' | 'peertube' | 'generic';

export interface PlatformPresentation {
  /** The name people know the platform by; the raw platform id for an unknown platform. */
  name: string;
  tone: PlatformTone;
  icon: PlatformIconName;
  /** The translated description of the platform; missing for an unknown one, whose description is the server's (`Source.description`). */
  descriptionKey?: TranslationKey;
  /** The platform works without an account, so a source that is not connected is "public access" rather than "not connected". */
  publicAccess: boolean;
}

const PLATFORMS: Record<string, PlatformPresentation> = {
  youtube: { name: 'YouTube', tone: 'red', icon: 'youtube', descriptionKey: 'sources.youtube.description', publicAccess: false },
  twitch: { name: 'Twitch', tone: 'violet', icon: 'twitch', descriptionKey: 'sources.twitch.description', publicAccess: true },
  peertube: { name: 'PeerTube', tone: 'orange', icon: 'peertube', descriptionKey: 'sources.peertube.description', publicAccess: true },
};

/** How a platform is presented. An unknown platform gets a neutral look and its own id as name. */
export function platformPresentation(platform: string): PlatformPresentation {
  return Object.hasOwn(PLATFORMS, platform) ? PLATFORMS[platform] : { name: platform, tone: 'neutral', icon: 'generic', publicAccess: false };
}

export function platformName(platform: string): string {
  return platformPresentation(platform).name;
}

export type SourceStatus = 'connected' | 'public' | 'notConnected';

interface SourceConnection {
  platform: string;
  connected: boolean;
  connectionMethods: string[];
}

/** Whether an account can import a cookie session on this source: the only way to connect, and absent on a platform with no account connection. */
export function canConnectWithCookies(source: Pick<SourceConnection, 'connectionMethods'>): boolean {
  return source.connectionMethods.includes('cookies');
}

/** The one status badge of a source: its session when it has one, else that it needs none (`public`) or lacks one. */
export function sourceStatus(source: SourceConnection): SourceStatus {
  if (source.connected) {
    return 'connected';
  }
  return !canConnectWithCookies(source) || platformPresentation(source.platform).publicAccess ? 'public' : 'notConnected';
}
