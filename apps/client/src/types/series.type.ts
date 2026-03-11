import type { PocketBaseRecord } from './base.type';
import type { VideoResult } from './platform.type';

/** A `series` record: the stored analysis of one channel's recurring playlist. */
export interface SeriesRecord extends PocketBaseRecord {
  name: string;
  /** The `channels` record the series belongs to. */
  channel: string;
  /** Ordered member `videos` record ids. */
  videos: string[];
}

export interface SeriesSummary {
  id: string;
  title: string;
  channelId: string;
  channelRecordId: string;
  channelName: string;
  platform: string;
  thumbnail: string;
  videoCount: number;
  latestPublishedAt: string;
  order: 'episode' | 'publication' | 'collection';
}

export interface SeriesItem {
  video: VideoResult;
  episode: number | null;
  season: number | null;
}

export interface Series extends SeriesSummary {
  items: SeriesItem[];
}

export interface SeriesSync {
  status: 'pending' | 'running' | 'complete';
  loaded: number;
}

export interface SeriesAnalysisStage {
  total: number;
  complete: number;
  running: number;
  pending: number;
  error: number;
}

export interface SeriesAnalysis {
  channels: (SeriesSync & { channelId: string; channelName: string; platform: string })[];
  metadata: SeriesAnalysisStage;
  ocr: SeriesAnalysisStage;
}

export interface SeriesResponse {
  series: SeriesSummary[];
  sync: SeriesSync;
  analysis: SeriesAnalysis;
}

export interface SeriesDetailResponse {
  series: Series;
  sync: SeriesSync;
  /** Whether episodes may still be added to the series, and whether missing publication dates are still being looked up. */
  episodes: SeriesSync['status'];
  dates: SeriesSync['status'];
}
