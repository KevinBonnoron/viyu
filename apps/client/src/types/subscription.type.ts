import type { PocketBaseRecord } from './base.type';
import type { ChannelRecord } from './channel.type';

/** The link between one creator's subscribed channels on several platforms; the server writes it, and deletes it once fewer than two channels share it. */
export interface CreatorRecord extends PocketBaseRecord {
  profile: string;
  name?: string;
}

export interface SubscriptionRecord extends PocketBaseRecord {
  profile: string;
  channel: string;
  /** The creator the channel is linked to, when it is grouped with another platform's channel. */
  creator?: string;
  /** Set once the user separated the channel from its creator: the server then never groups it again by itself. */
  ungrouped?: boolean;
  expand?: {
    channel?: ChannelRecord;
    creator?: CreatorRecord;
  };
}

/** What moving (or copying) subscriptions between two profiles did; the ids are the rows now on the target profile. */
export interface MoveSubscriptionsResult {
  /** Subscriptions that arrived on the target profile. */
  moved: number;
  /** Subscriptions the target already had: the source row was dropped, not duplicated. */
  merged: number;
  movedIds: string[];
  mergedIds: string[];
}
