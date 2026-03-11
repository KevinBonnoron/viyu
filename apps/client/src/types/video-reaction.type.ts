import type { PocketBaseRecord } from './base.type';
import type { VideoRecord } from './video.type';

export type VideoReaction = 'like' | 'dislike' | 'not_interested';

export interface VideoReactionRecord extends PocketBaseRecord {
  profile: string;
  video: string;
  reaction: VideoReaction;
  expand?: {
    video?: VideoRecord;
  };
}
