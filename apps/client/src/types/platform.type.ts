export type Platform = string;

export interface VideoChapter {
  title: string;
  start_time: number;
  end_time: number;
}

/** `id` and `channelId` address the video and its channel by their record ids: the server never exposes platform-native ids. */
export interface VideoResult {
  id: string;
  platform: Platform;
  source?: string;
  title: string;
  originalTitle: string;
  originalTitleVerified: boolean;
  originalLanguage: string;
  channelId: string;
  channelName: string;
  channelThumbnail: string;
  creators?: string[];
  thumbnail: string;
  thumbnails?: string[];
  duration: number;
  description: string;
  tags: string[];
  chapters?: VideoChapter[];
  /** "YYYY-MM-DD" when only the day is known, an RFC3339 UTC timestamp when the exact time is known, empty when unknown. */
  published: string;
  viewCount: number;
  likeCount: number | null;
  dislikeCount: number | null;
  /** A Twitch VOD only subscribers of the channel can watch. */
  subscriberOnly?: boolean;
  /** A stream in progress: there is a live edge to go back to. */
  live?: boolean;
  /** What the conversation about the video is: a static list of comments, a chat (live, or replayed in sync with the video), or absent when the platform offers none. */
  comments?: 'list' | 'chat';
  /** What the video or stream is about as its platform puts it: a Twitch game, a YouTube or PeerTube category. */
  category?: string;
}

export interface ChannelResult {
  id: string;
  platform: Platform;
  source?: string;
  name: string;
  thumbnail: string;
}

export type SearchResult = VideoResult | ChannelResult;

export interface SearchPage {
  results: SearchResult[];
  page: number;
  hasMore: boolean;
  /** Platforms a combined search could not query, with the server's error. */
  failures?: { platform: string; code: string; message: string }[];
}

export function isChannelResult(result: SearchResult): result is ChannelResult {
  return !('duration' in result);
}

export interface ChannelInfo {
  id: string;
  platform: Platform;
  name: string;
  thumbnail: string;
  description: string;
  subscriberCount: number;
}

/** Whether a channel is broadcasting now; `recordId` is the `videos` record of the stream, which is what the player opens. */
export interface ChannelLive {
  live: boolean;
  recordId?: string;
  title?: string;
  viewers?: number;
  category?: string;
}

/** A subscribed channel that is live now, with the preview picture of its stream. */
export interface SubscriptionLive extends ChannelLive {
  platform: Platform;
  channelId: string;
  channelRecordId: string;
  thumbnail: string;
}

/** How many videos a subscribed creator released since the profile last looked. */
export interface NewVideos {
  platform: Platform;
  channelId: string;
  channelRecordId: string;
  name: string;
  thumbnail: string;
  count: number;
}

/** `asOf` is sent back to mark exactly these videos as seen. */
export interface NewVideosResponse {
  items: NewVideos[];
  asOf: string;
}
