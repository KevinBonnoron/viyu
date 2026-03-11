import type {
  ChannelInfo,
  ChannelLive,
  ChannelSync,
  ChatReplayPage,
  MoveSubscriptionsResult,
  NewVideosResponse,
  SearchPage,
  Source,
  SourceFeed,
  SourceFeedKind,
  Stats,
  StatsPeriod,
  SubscriptionLive,
  SubscriptionRecord,
  VideoCommentsPage,
  VideoResult,
} from '@/types';
import { type ApiError, apiErrorFrom } from './api-error';
import { path, queryString, type RequestOptions, readResponse, requestInit } from './api-request';
import { apiUrl, pb } from './pocketbase';
import { handleUnauthorized } from './session';

// --- Response types ---------------------------------------------------------------------------------------------

/** A channel page: its identity (null until the source answered), the videos known so far and the state of the catalog crawl. */
export interface ChannelPage {
  channel: ChannelInfo | null;
  videos: VideoResult[];
  sync?: ChannelSync;
}

export interface VideoUrl {
  url: string;
}

export interface RefreshResult {
  refreshed: number;
  failed: number;
}

export interface SeenResult {
  ok: boolean;
}

export interface WatchMarked {
  status: 'marked';
}

/** What an account import restored, by kind. */
export interface ImportSummary {
  profiles: number;
  subscriptions: number;
  reactions: number;
  history: number;
}

// --- Transport --------------------------------------------------------------------------------------------------

/** The error of a failed response; a 401 also ends the session. */
export async function responseError(response: Response): Promise<ApiError> {
  if (response.status === 401) {
    handleUnauthorized();
  }
  return apiErrorFrom(response.status, await response.json().catch(() => null));
}

/** The one way to call the server's API: `route` is relative to `/api`. */
export function request(route: string, options: RequestOptions & { responseType: 'blob' }): Promise<Blob>;
export function request<T>(route: string, options?: RequestOptions): Promise<T>;
export async function request(route: string, options: RequestOptions = {}): Promise<unknown> {
  const response = await fetch(apiUrl(`/api${route}${queryString(options.query)}`), requestInit(options, pb.authStore.token));
  if (!response.ok) {
    throw await responseError(response);
  }
  return readResponse(response, options.responseType);
}

const inflight = new Map<string, Promise<unknown>>();

/** Runs `start` unless a call under the same key is already running, in which case that call's result is shared. */
function dedupe<T>(key: string, start: () => Promise<T>): Promise<T> {
  const running = inflight.get(key);
  if (running) {
    return running as Promise<T>;
  }
  const started = start().finally(() => inflight.delete(key));
  inflight.set(key, started);
  return started;
}

// --- URLs that are not requests ---------------------------------------------------------------------------------

/** The URL of a video's stream, played by the player rather than fetched. */
function streamUrl(videoId: string, { source }: { source?: string } = {}): string {
  return apiUrl(`/api${path`/stream/${videoId}`}${queryString({ source })}`);
}

/** The URL of a channel's live chat, an event stream opened with `fetch` so that it can carry the session. */
function chatLiveUrl(login: string): string {
  return apiUrl(`/api${path`/chat/live/${login}`}`);
}

// --- Calls ------------------------------------------------------------------------------------------------------

interface Aborts {
  signal?: AbortSignal;
}

export const api = {
  search: (q: string, { source, platform, page = 1, signal }: Aborts & { source?: string; platform?: string; page?: number } = {}) =>
    request<SearchPage>('/search', { query: { q, page, source, platform }, signal }),
  feed: (profile: string, { signal }: Aborts = {}) => request<VideoResult[]>('/feed', { query: { profile }, signal }),
  recommendations: (profile: string, video: string, { signal }: Aborts = {}) =>
    request<VideoResult[]>('/recommendations', { query: { profile, video }, signal }),
  stats: (profile: string, period: StatsPeriod, { signal }: Aborts = {}) => request<Stats>('/stats', { query: { profile, period }, signal, cache: 'no-store' }),

  videos: {
    resolve: (url: string, { signal }: Aborts = {}) => request<VideoResult>('/resolve', { method: 'POST', body: { url }, signal }),
    get: (videoId: string, { source, chapters, signal }: Aborts & { source?: string; chapters?: boolean } = {}) =>
      request<VideoResult>(path`/video/${videoId}`, { query: { source, chapters }, signal }),
    comments: (videoId: string, { source, signal }: Aborts & { source?: string } = {}) =>
      request<VideoCommentsPage>(path`/video/${videoId}/comments`, { query: { source }, signal }),
    url: (videoId: string, { signal }: Aborts = {}) => request<VideoUrl>(path`/video/${videoId}/url`, { signal }),
    stream: streamUrl,
  },

  channels: {
    get: (channelId: string, { source, sync, retry, signal }: Aborts & { source?: string; sync?: boolean; retry?: boolean } = {}) =>
      request<ChannelPage>(path`/channel/${channelId}`, { query: { source, sync, retry }, signal, cache: sync ? 'no-store' : undefined }),
    live: (channelId: string, { signal }: Aborts = {}) => request<ChannelLive>(path`/channel/${channelId}/live`, { signal }),
  },

  subscriptions: {
    create: (profile: string, channelId: string, { creator }: { creator?: string } = {}) =>
      request<SubscriptionRecord>('/subscriptions', { method: 'POST', body: { profile, channelId, creator: creator || undefined } }),
    group: (profile: string, subscriptionIds: string[]) =>
      request<SubscriptionRecord[]>('/subscriptions/group', { method: 'POST', body: { profile, subscriptionIds } }),
    ungroup: (profile: string, creator: string) => request<SubscriptionRecord[]>('/subscriptions/ungroup', { method: 'POST', body: { profile, creator } }),
    /** Moves subscriptions to another profile of the account, or copies them (the source keeps them). */
    move: (from: string, to: string, subscriptionIds: string[], { copy }: { copy?: boolean } = {}) =>
      request<MoveSubscriptionsResult>('/subscriptions/move', { method: 'POST', body: { from, to, subscriptionIds, copy: copy || undefined } }),
    live: (profile: string, { signal }: Aborts = {}) => request<SubscriptionLive[]>('/subscriptions/live', { query: { profile }, signal }),
    /** Concurrent refreshes of one profile share a single call. */
    refresh: (profile: string) =>
      dedupe(`subscriptions.refresh:${profile}`, () => request<RefreshResult>('/subscriptions/refresh', { method: 'POST', query: { profile } })),
  },

  notifications: {
    list: (profile: string, { signal }: Aborts = {}) => request<NewVideosResponse>('/notifications', { query: { profile }, signal, cache: 'no-store' }),
    markSeen: (profile: string, asOf: string, { channel }: { channel?: string } = {}) =>
      request<SeenResult>('/notifications/seen', { method: 'POST', body: { profile, asOf, channel: channel || undefined } }),
  },

  history: {
    clear: (profile: string) => request<void>('/history', { method: 'DELETE', query: { profile } }),
  },

  chat: {
    live: chatLiveUrl,
    replay: (videoId: string, from: { offset: number } | { cursor: string }, { signal }: Aborts = {}) =>
      request<ChatReplayPage>(path`/chat/vod/${videoId}`, { query: from, signal, cache: 'no-store' }),
  },

  sources: {
    list: ({ signal }: Aborts = {}) => request<Source[]>('/sources', { signal }),
    connect: (platform: string, cookies: string) => request<Source>(path`/sources/${platform}/connect`, { method: 'POST', body: { cookies } }),
    check: (platform: string) => request<Source>(path`/sources/${platform}/check`, { method: 'POST' }),
    disconnect: (platform: string) => request<void>(path`/sources/${platform}`, { method: 'DELETE' }),
    setEnabled: (platform: string, enabled: boolean) => request<Source>(path`/sources/${platform}/activation`, { method: 'PUT', body: { enabled } }),
    feed: (platform: string, kind: SourceFeedKind, page: number, { signal }: Aborts = {}) =>
      request<SourceFeed>(path`/sources/${platform}/feed`, { query: { kind, page, limit: 30 }, signal }),
    markWatched: (platform: string, videoId: string, playedSeconds: number, durationSeconds: number) =>
      request<WatchMarked>(path`/sources/${platform}/watch`, { method: 'POST', body: { videoId, playedSeconds, durationSeconds } }),
  },

  account: {
    export: () => request('/account/export', { responseType: 'blob' }),
    /** `payload` is the exported file's text, sent as it is. */
    import: (payload: string) => request<ImportSummary>('/account/import', { method: 'POST', body: payload }),
  },
};
