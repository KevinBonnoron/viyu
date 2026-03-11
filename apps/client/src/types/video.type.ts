import type { PocketBaseRecord } from './base.type';
import type { ChannelRecord } from './channel.type';
import type { Platform } from './platform.type';

/** What the server keeps about a video besides its columns; absent keys read as zero values or null. */
export interface VideoMetadata {
  original_language?: string;
  original_title_verified?: boolean;
  chapters_loaded?: boolean;
  /** PocketBase stored time; empty or absent means expired. */
  metadata_expires_at?: string;
  like_count?: number | null;
  dislike_count?: number | null;
}

export interface VideoRecord extends PocketBaseRecord {
  platform_id: string;
  platform: Platform;
  /** `original` is the original title, possibly unproven; other keys are translations by language. A translation equal to the original, or empty, is absent. */
  titles: { original?: string; [language: string]: string | undefined } | null;
  /** A Twitch VOD only subscribers of the channel can watch. */
  subscriber_only?: boolean;
  channel: ChannelRecord['id'];
  thumbnails?: string[];
  creators?: string[];
  duration: number;
  description: string;
  view_count: number;
  tags: string[];
  /** What the video is about as its platform puts it: a Twitch game, a YouTube or PeerTube category. */
  category?: string;
  /** PocketBase date; a time of exactly 00:00:00.000Z means only the day is known, empty means unknown. */
  published: string;
  expires_at: string;
  /** The users whose connected source fetched the record; empty for a public one. Access is enforced by the server, never by the client. */
  owners?: string[];
  metadata?: VideoMetadata | null;
  expand?: {
    channel?: ChannelRecord;
  };
}
