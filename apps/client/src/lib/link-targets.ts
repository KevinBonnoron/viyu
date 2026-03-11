/**
 * Where a video or a channel is opened. Everything the client shows carries the id of its PocketBase record
 * (the server never exposes platform-native ids), so a video opens on its record page and a channel on its
 * record page; a stream in progress opens on its channel's live page. Connected-source content is opened
 * the same way and only carries its `source`, which the page needs to read it privately.
 */

export interface VideoTargetInput {
  /** The `videos` record id. */
  id: string;
  /** The `channels` record id of the owner. */
  channelId?: string;
  live?: boolean;
  source?: string;
}

export type VideoTarget = { kind: 'live'; channelId: string } | { kind: 'record'; videoId: string; series?: string; source?: string };

export function videoTarget(video: VideoTargetInput, options: { series?: string; source?: string } = {}): VideoTarget {
  const source = options.source || video.source || undefined;
  if (!source && video.live && video.channelId) {
    return { kind: 'live', channelId: video.channelId };
  }
  return { kind: 'record', videoId: video.id, series: options.series || undefined, source };
}

export interface ChannelTargetInput {
  /** The `channels` record id. */
  id: string;
  platform: string;
  source?: string;
}

export interface ChannelTarget {
  channelId: string;
  source?: string;
  tab?: 'series';
}

export function channelTarget(channel: ChannelTargetInput, tab?: 'videos' | 'series'): ChannelTarget {
  // YouTube has no connected source for channel pages.
  const source = channel.platform === 'youtube' ? undefined : channel.source || undefined;
  return { channelId: channel.id, source, tab: tab === 'series' ? 'series' : undefined };
}
