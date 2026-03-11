import type { PocketBaseRecord } from './base.type';

export const playbackQualities = ['auto', '144', '240', '360', '480', '720', '1080', '1440', '2160', '4320'] as const;
export type PlaybackQuality = (typeof playbackQualities)[number];

export interface SettingsRecord extends PocketBaseRecord {
  user: string;
  hide_shorts: boolean;
  hide_watched: boolean;
  hide_subscriber_only: boolean;
  use_original_titles: boolean;
  playback_quality: PlaybackQuality | '';
  auto_skip_sponsors: boolean;
  autoplay: boolean;
}
