import type { PocketBaseRecord } from './base.type';
import type { ChannelResult, Platform } from './platform.type';

export interface ChannelRecord extends PocketBaseRecord {
  platform_id: string;
  platform: Platform;
  name: string;
  thumbnail: string;
  description: string;
  subscriber_count: number;
  expires_at: string;
  /** The users whose connected source fetched the record; empty for a public one. Access is enforced by the server, never by the client. */
  owners?: string[];
}

/** Several subscribed channels of one creator on different platforms, shown as one filter entry. */
export interface ChannelGroupResult {
  id: string;
  name: string;
  thumbnail: string;
  channels: ChannelResult[];
}

export type ChannelFilterItem = ChannelResult | ChannelGroupResult;

/** The background state of a channel catalog crawl (see GET /api/channel/{id}?sync=1). */
export interface ChannelSync {
  status: 'pending' | 'running' | 'complete' | 'error';
  loaded: number;
}
