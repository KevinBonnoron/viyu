import type { PocketBaseRecord } from './base.type';
import type { VideoRecord } from './video.type';

/** A profile's history of one video: one row per `(profile, video)`, its platform and channel are those of the linked video. */
export interface WatchHistoryRecord extends PocketBaseRecord {
  profile: string;
  video: string;
  series?: string;
  watched_at: string;
  duration_watched: number;
  video_duration: number;
  position: number;
  /** The single source of the watched status: raised by a playback checkpoint past 90 % or by marking watched, removed with the row. */
  watched: boolean;
  expand?: {
    video?: VideoRecord;
  };
}
