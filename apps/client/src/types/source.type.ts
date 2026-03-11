import type { VideoResult } from './platform.type';

export interface Source {
  platform: string;
  name: string;
  description: string;
  capabilities: string[];
  /** How an account connects to this platform: `["cookies"]` for an imported browser session; empty when the platform has no account connection (nothing to connect, check or disconnect). */
  connectionMethods: string[];
  /** Whether the account uses this source at all; independent from `connected`, which is only about an imported cookie session. */
  enabled: boolean;
  connected: boolean;
  connectionId?: string;
  updatedAt?: string;
}

export type SourceFeedKind = 'home' | 'subscriptions' | 'history';

export interface SourceFeed {
  videos: VideoResult[];
  page: number;
  hasMore: boolean;
}
