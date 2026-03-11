export type StatsPeriod = 'all' | '30d' | '90d' | '365d';

/** A topic (tag or category) with the time spent on it. `key` is normalized (see `normalizeTopic`), `name` is how it is displayed. */
export interface StatsEntry {
  key: string;
  name: string;
  seconds: number;
  count: number;
}

export interface StatsChannel {
  platform: string;
  channelId: string;
  recordId: string;
  name: string;
  thumbnail: string;
  seconds: number;
  count: number;
}

export interface StatsMonth {
  /** `YYYY-MM` */
  month: string;
  seconds: number;
  count: number;
}

export interface StatsPlatform {
  platform: string;
  seconds: number;
  count: number;
}

/** What the active profile watched: `seconds` is watch time, `count` the number of distinct videos. */
export interface Stats {
  period: string;
  totals: { videos: number; seconds: number; channels: number };
  topTags: StatsEntry[];
  topCategories: StatsEntry[];
  topChannels: StatsChannel[];
  byMonth: StatsMonth[];
  byPlatform: StatsPlatform[];
}
