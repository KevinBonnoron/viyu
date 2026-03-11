import type { Platform } from '@/types';

/** What a pasted address names: a video or a channel of a platform. The server reads the identifiers itself (`POST /api/resolve`). */
export interface PastedLink {
  platform: Platform;
  kind: 'video' | 'channel';
}

const youtubeHosts = ['youtube.com', 'www.youtube.com', 'm.youtube.com', 'music.youtube.com', 'youtube-nocookie.com', 'www.youtube-nocookie.com'];
const youtubeVideoId = /^[A-Za-z0-9_-]{11}$/u;
const youtubeChannelId = /^UC[A-Za-z0-9_-]{22}$/u;
const youtubeHandle = /^@[\p{L}\p{N}._-]{1,100}$/u;
const twitchHosts = ['twitch.tv', 'www.twitch.tv', 'm.twitch.tv'];
const twitchLogin = /^[A-Za-z0-9_]{1,25}$/u;
const twitchVod = /^[1-9][0-9]{0,19}$/u;
/** First path segments of twitch.tv that are pages of the site, not channel logins. */
const twitchPages: Record<string, true> = {
  directory: true,
  downloads: true,
  drops: true,
  friends: true,
  inventory: true,
  jobs: true,
  messages: true,
  p: true,
  popout: true,
  prime: true,
  search: true,
  settings: true,
  store: true,
  subscriptions: true,
  turbo: true,
  videos: true,
  wallet: true,
};

/**
 * Reads a pasted YouTube or Twitch address, the same shapes the server accepts: YouTube watch, youtu.be, shorts, live
 * and embed videos, `/@handle` and `/channel/<id>` channels (optionally on a tab); Twitch `/videos/<id>` and `/<login>`
 * (or its videos, about or schedule tab). Anything else is a search term. PeerTube instances are only known to the
 * server, so their links are searched.
 */
export function pastedLink(input: string): PastedLink | null {
  const value = input.trim();
  if (!/^(?:https?:\/\/)?(?:www\.|m\.|music\.)?(?:youtube\.com|youtu\.be|youtube-nocookie\.com|twitch\.tv)\//iu.test(value)) {
    return null;
  }
  let address: URL;
  try {
    address = new URL(/^https?:\/\//iu.test(value) ? value : `https://${value}`);
  } catch {
    return null;
  }
  if (address.username || address.password || address.port) {
    return null;
  }
  const host = address.hostname.toLowerCase();
  const parts = address.pathname
    .split('/')
    .filter(Boolean)
    .map((part) => {
      try {
        return decodeURIComponent(part);
      } catch {
        return part;
      }
    });
  if (host === 'youtu.be' || host === 'www.youtu.be') {
    return parts.length === 1 && youtubeVideoId.test(parts[0]) ? { platform: 'youtube', kind: 'video' } : null;
  }
  if (youtubeHosts.includes(host)) {
    if (address.pathname === '/watch') {
      return youtubeVideoId.test(address.searchParams.get('v') ?? '') ? { platform: 'youtube', kind: 'video' } : null;
    }
    if (parts.length === 2 && ['shorts', 'live', 'embed'].includes(parts[0]) && youtubeVideoId.test(parts[1])) {
      return { platform: 'youtube', kind: 'video' };
    }
    const channel =
      (parts.length >= 2 && parts.length <= 3 && parts[0] === 'channel' && youtubeChannelId.test(parts[1])) ||
      (parts.length >= 1 && parts.length <= 2 && youtubeHandle.test(parts[0]));
    return channel ? { platform: 'youtube', kind: 'channel' } : null;
  }
  if (twitchHosts.includes(host)) {
    if (parts.length === 2 && parts[0] === 'videos' && twitchVod.test(parts[1])) {
      return { platform: 'twitch', kind: 'video' };
    }
    const tab = parts.length === 1 || (parts.length === 2 && ['videos', 'about', 'schedule'].includes(parts[1]));
    return tab && twitchLogin.test(parts[0]) && !twitchPages[parts[0].toLowerCase()] ? { platform: 'twitch', kind: 'channel' } : null;
  }
  return null;
}
