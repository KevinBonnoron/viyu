import { isRecord } from '../guards';

/**
 * Reading YouTube's own browse responses (the JSON youtube.com renders its pages from). Their layout changes often and
 * differs between accounts, so nothing here follows a fixed path: the responses are walked as a tree and the few
 * renderers that carry a channel, a video or a continuation are recognized wherever they sit.
 */

export interface ChannelsPage {
  channelIds: string[];
  continuation?: string;
}

export interface HistoryEntry {
  videoId: string;
  /** RFC 3339. The day is YouTube's; the time within the day only keeps the history in order. */
  watchedAt: string;
  /** Seconds, when YouTube shows how far the video was watched and how long it is. */
  position?: number;
  duration?: number;
  /** Whether YouTube shows the video watched past 90 %, Viyu's own threshold; absent when it shows no progress. */
  watched?: boolean;
}

export interface HistoryPage {
  entries: HistoryEntry[];
  continuation?: string;
}

/** Where the walk of the history is: the day of the last section header and how many videos that day already had. */
export interface HistoryCursor {
  dayStart?: number;
  index: number;
}

const channelIdPattern = /^UC[0-9A-Za-z_-]{22}$/;
const videoIdPattern = /^[0-9A-Za-z_-]{11}$/;
const durationPattern = /^(?:(\d{1,2}):)?(\d{1,2}):(\d{2})$/;
const watchedPercent = 90;
const minute = 60_000;

/** Walks every object of a JSON tree depth first, in key order. The visitor answers whether to descend into the value. */
function walk(node: unknown, visit: (key: string, value: Record<string, unknown>) => boolean): void {
  if (Array.isArray(node)) {
    for (const item of node) {
      walk(item, visit);
    }
    return;
  }
  if (!isRecord(node)) {
    return;
  }
  for (const [key, value] of Object.entries(node)) {
    if (isRecord(value)) {
      if (visit(key, value)) {
        walk(value, visit);
      }
    } else if (Array.isArray(value)) {
      walk(value, visit);
    }
  }
}

/** The first value found under one of the keys, anywhere below node. */
function find(node: unknown, keys: readonly string[], accept: (value: unknown) => boolean): unknown {
  let found: unknown;
  const search = (value: unknown): void => {
    if (found !== undefined) {
      return;
    }
    if (Array.isArray(value)) {
      for (const item of value) {
        search(item);
      }
      return;
    }
    if (!isRecord(value)) {
      return;
    }
    for (const [key, child] of Object.entries(value)) {
      if (keys.includes(key) && accept(child)) {
        found = child;
        return;
      }
      search(child);
    }
  };
  search(node);
  return found;
}

/** The text of a YouTube text object: `simpleText`, the joined `runs`, or a view model's `content`. */
function text(node: unknown): string | undefined {
  if (typeof node === 'string') {
    return node;
  }
  if (!isRecord(node)) {
    return undefined;
  }
  if (typeof node.simpleText === 'string') {
    return node.simpleText;
  }
  if (Array.isArray(node.runs)) {
    return node.runs.map((run) => (isRecord(run) && typeof run.text === 'string' ? run.text : '')).join('');
  }
  if (typeof node.content === 'string') {
    return node.content;
  }
  return undefined;
}

/** The token of the next page: a `continuationItemRenderer` at the end of a list, or the older `nextContinuationData`. */
function continuationOf(response: unknown): string | undefined {
  let token: string | undefined;
  walk(response, (key, value) => {
    if (token !== undefined) {
      return false;
    }
    if (key === 'continuationItemRenderer') {
      const command = find(value, ['continuationCommand'], (child) => isRecord(child) && typeof child.token === 'string');
      if (isRecord(command) && typeof command.token === 'string') {
        token = command.token;
      }
      return false;
    }
    if (key === 'nextContinuationData' && typeof value.continuation === 'string') {
      token = value.continuation;
      return false;
    }
    return true;
  });
  return token;
}

/** The channels of one page of the subscriptions list (`FEchannels`). */
export function parseChannelsPage(response: unknown): ChannelsPage {
  const channelIds: string[] = [];
  walk(response, (key, value) => {
    if (key === 'channelRenderer' || key === 'gridChannelRenderer') {
      if (typeof value.channelId === 'string' && channelIdPattern.test(value.channelId)) {
        channelIds.push(value.channelId);
      }
      return false;
    }
    return true;
  });
  return { channelIds, continuation: continuationOf(response) };
}

function startOfDay(date: Date): Date {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate());
}

const weekdays = ['sunday', 'monday', 'tuesday', 'wednesday', 'thursday', 'friday', 'saturday'];
const months = ['jan', 'feb', 'mar', 'apr', 'may', 'jun', 'jul', 'aug', 'sep', 'oct', 'nov', 'dec'];

/**
 * The local day a history section header names, in English (the extension asks YouTube for `hl=en`): "Today",
 * "Yesterday", a weekday of the past week, "Nov 5" in the past twelve months or "Nov 5, 2024". Anything else is unknown.
 */
export function parseDayHeader(header: string, now: Date): Date | undefined {
  const value = header.trim().toLowerCase();
  const today = startOfDay(now);
  if (value === 'today') {
    return today;
  }
  if (value === 'yesterday') {
    return new Date(today.getFullYear(), today.getMonth(), today.getDate() - 1);
  }
  const weekday = weekdays.indexOf(value);
  if (weekday >= 0) {
    const back = (today.getDay() - weekday + 7) % 7 || 7;
    return new Date(today.getFullYear(), today.getMonth(), today.getDate() - back);
  }
  const match = /^([a-z]{3,9})\.? (\d{1,2})(?:, (\d{4}))?$/.exec(value);
  if (!match?.[1] || !match[2]) {
    return undefined;
  }
  const month = months.indexOf(match[1].slice(0, 3));
  const day = Number(match[2]);
  if (month < 0 || day < 1 || day > 31) {
    return undefined;
  }
  if (match[3]) {
    return new Date(Number(match[3]), month, day);
  }
  const date = new Date(today.getFullYear(), month, day);
  return date > today ? new Date(today.getFullYear() - 1, month, day) : date;
}

function durationSeconds(value: string | undefined): number | undefined {
  const match = value ? durationPattern.exec(value.trim()) : null;
  if (!match) {
    return undefined;
  }
  return Number(match[1] ?? 0) * 3600 + Number(match[2]) * 60 + Number(match[3]);
}

/** The video a history renderer shows, if it is one: classic and Shorts renderers, and the newer lockup view models. */
function videoIdOf(key: string, value: Record<string, unknown>): string | undefined {
  let id: unknown;
  if (key === 'videoRenderer' || key === 'reelItemRenderer') {
    id = value.videoId;
  } else if (key === 'lockupViewModel' && value.contentType === 'LOCKUP_CONTENT_TYPE_VIDEO') {
    id = value.contentId;
  } else if (key === 'shortsLockupViewModel') {
    const endpoint = find(value, ['reelWatchEndpoint'], (child) => isRecord(child) && typeof child.videoId === 'string');
    id = isRecord(endpoint) ? endpoint.videoId : typeof value.entityId === 'string' ? value.entityId.replace(/^shorts-shelf-item-/, '') : undefined;
  } else {
    return undefined;
  }
  return typeof id === 'string' && videoIdPattern.test(id) ? id : '';
}

/** How long the video is, from its length text or its duration badge. */
function videoDuration(video: Record<string, unknown>): number | undefined {
  const length = find(video, ['lengthText'], (child) => durationSeconds(text(child)) !== undefined);
  if (length !== undefined) {
    return durationSeconds(text(length));
  }
  const badge = find(video, ['thumbnailOverlayTimeStatusRenderer', 'thumbnailBadgeViewModel'], (child) =>
    isRecord(child) ? durationSeconds(text(child.text)) !== undefined : false,
  );
  return isRecord(badge) ? durationSeconds(text(badge.text)) : undefined;
}

/** How far the video was watched, in percent, from its resume overlay or its progress bar. */
function watchedPercentOf(video: Record<string, unknown>): number | undefined {
  const percent = find(video, ['percentDurationWatched', 'startPercent'], (child) => typeof child === 'number' && child >= 0 && child <= 100);
  return typeof percent === 'number' ? percent : undefined;
}

/** Reads a section header and moves the cursor to the day it names; the same day again keeps counting. */
function enterDay(header: unknown, now: Date, cursor: HistoryCursor): void {
  const title = find(header, ['title'], (child) => text(child) !== undefined);
  const day = parseDayHeader(text(title) ?? '', now);
  if (day && day.getTime() !== cursor.dayStart) {
    cursor.dayStart = day.getTime();
    cursor.index = 0;
  }
}

/**
 * The videos of one page of the watch history (`FEhistory`), newest first. YouTube only gives the day of each section;
 * within it, the n-th video is placed n minutes before the end of the day (before now, today) so the order holds. The
 * cursor carries the day over to the next page, whose first videos may continue the same section.
 */
export function parseHistoryPage(response: unknown, now: Date, cursor: HistoryCursor): HistoryPage {
  const entries: HistoryEntry[] = [];
  const seen = new Set<string>();
  const today = startOfDay(now).getTime();
  walk(response, (key, value) => {
    if (key === 'itemSectionRenderer') {
      enterDay(value.header, now, cursor);
      return true;
    }
    if (key === 'itemSectionHeaderRenderer') {
      enterDay(value, now, cursor);
      return false;
    }
    const videoId = videoIdOf(key, value);
    if (videoId === undefined) {
      return true;
    }
    if (videoId === '' || seen.has(videoId)) {
      return false;
    }
    seen.add(videoId);
    const dayStart = cursor.dayStart ?? today;
    const start = new Date(dayStart);
    const end = dayStart === today ? now.getTime() : new Date(start.getFullYear(), start.getMonth(), start.getDate(), 23, 59).getTime();
    const watchedAt = new Date(Math.max(dayStart, end - cursor.index * minute));
    cursor.index += 1;
    const entry: HistoryEntry = { videoId, watchedAt: watchedAt.toISOString() };
    const duration = videoDuration(value);
    const percent = watchedPercentOf(value);
    if (duration !== undefined) {
      entry.duration = duration;
    }
    if (percent !== undefined) {
      entry.watched = percent >= watchedPercent;
      if (duration !== undefined) {
        entry.position = Math.round((duration * percent) / 100);
      }
    }
    entries.push(entry);
    return false;
  });
  return { entries, continuation: continuationOf(response) };
}
