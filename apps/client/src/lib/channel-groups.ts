import { foldSearch } from '@/lib/text';
import type { ChannelFilterItem, ChannelGroupResult, ChannelResult } from '@/types';

export interface GroupableSubscription {
  id: string;
  created?: string;
  creator?: string;
  expand?: { creator?: { name?: string } };
  channelInfo?: { id: string; platform: string; name: string; thumbnail: string };
}

export interface DuplicateSubscriptions<T extends GroupableSubscription> {
  key: string;
  name: string;
  platforms: string[];
  subscriptions: T[];
}

export function channelIdentity(channel: { id: string; platform: string; source?: string }): string {
  return `${channel.source || 'public'}:${channel.platform}:${channel.id}`;
}

export function isChannelGroup(item: ChannelFilterItem): item is ChannelGroupResult {
  return 'channels' in item;
}

export function filterIdentity(item: ChannelFilterItem): string {
  return isChannelGroup(item) ? `group:${item.id}` : channelIdentity(item);
}

/** Channels whose videos a filter item selects. */
export function filterChannels(item: ChannelFilterItem): ChannelResult[] {
  return isChannelGroup(item) ? item.channels : [item];
}

/** Whether a video belongs to one of the selected public channels; no selection matches everything. */
export function matchesChannels(channels: ChannelResult[] | undefined, video: { platform: string; channelId: string; source?: string }): boolean {
  return (
    !channels ||
    channels.some(
      (channel) => video.platform === channel.platform && video.channelId === channel.id && (video.source || undefined) === (channel.source || undefined),
    )
  );
}

/** Case-, accent- and punctuation-insensitive channel name used only to suggest groupings. */
export function normalizeChannelName(name: string): string {
  return foldSearch(name).replace(/[^\p{L}\p{N}]/gu, '');
}

const NAME_SEPARATORS = /[|/•·–—()[\]]|\s[-~:]\s/u;
const MIN_SEGMENT_KEY_LENGTH = 3;

/**
 * The keys of the parts of a name that bundles several names ("Yves Rougy | Yorzian"), or none for an ordinary name.
 * A part only ever matches another channel's whole name, never another part. Mirrors `channelNameSegmentKeys` on the server.
 */
export function channelNameSegmentKeys(name: string): string[] {
  const parts = name.split(NAME_SEPARATORS);
  if (parts.length < 2) {
    return [];
  }
  return [...new Set(parts.map(normalizeChannelName).filter((key) => [...key].length >= MIN_SEGMENT_KEY_LENGTH))];
}

function channelOf(subscription: GroupableSubscription): ChannelResult | undefined {
  const info = subscription.channelInfo;
  return info ? { id: info.id, platform: info.platform, name: info.name, thumbnail: info.thumbnail } : undefined;
}

function byCreation<T extends GroupableSubscription>(a: T, b: T): number {
  return (a.created ?? '').localeCompare(b.created ?? '');
}

function membersByCreator<T extends GroupableSubscription>(subscriptions: T[]): Map<string, T[]> {
  const creators = new Map<string, T[]>();
  for (const subscription of subscriptions) {
    const creator = subscription.creator;
    if (creator && subscription.channelInfo) {
      creators.set(creator, [...(creators.get(creator) ?? []), subscription]);
    }
  }
  for (const members of creators.values()) {
    members.sort(byCreation);
  }
  return creators;
}

/** One filter item per channel without a creator and per creator of two or more subscribed channels, named after the creator when it has a name. */
export function buildChannelFilters<T extends GroupableSubscription>(subscriptions: T[]): ChannelFilterItem[] {
  const creators = membersByCreator(subscriptions);
  const emitted = new Set<string>();
  const items: ChannelFilterItem[] = [];
  for (const subscription of subscriptions) {
    const channel = channelOf(subscription);
    if (!channel) {
      continue;
    }
    const creator = subscription.creator;
    const members = creator ? creators.get(creator) : undefined;
    if (!creator || !members || members.length < 2) {
      items.push(channel);
    } else if (!emitted.has(creator)) {
      emitted.add(creator);
      const channels = members.flatMap((member) => channelOf(member) ?? []);
      const name = members.map((member) => member.expand?.creator?.name).find((candidate) => candidate) || channels[0].name;
      items.push({ id: creator, name, thumbnail: channels[0].thumbnail, channels });
    }
  }
  return items;
}

/** Subscriptions without a creator with the same normalized name (or whose bundled name contains it) and exactly one channel per platform. */
export function ungroupedDuplicates<T extends GroupableSubscription>(subscriptions: T[]): DuplicateSubscriptions<T>[] {
  const creators = membersByCreator(subscriptions);
  const buckets = new Map<string, T[]>();
  const candidates: T[] = [];
  for (const subscription of subscriptions) {
    const name = subscription.channelInfo ? normalizeChannelName(subscription.channelInfo.name) : '';
    if (!name || (subscription.creator && (creators.get(subscription.creator)?.length ?? 0) > 1)) {
      continue;
    }
    buckets.set(name, [...(buckets.get(name) ?? []), subscription]);
    candidates.push(subscription);
  }
  // A bundled name also joins the bucket of each of its parts that is another channel's whole name, unless that bucket
  // already has a channel of its platform: an exact match on the same name always wins.
  for (const subscription of candidates) {
    const info = subscription.channelInfo;
    if (!info) {
      continue;
    }
    for (const key of channelNameSegmentKeys(info.name)) {
      const members = buckets.get(key);
      if (!members) {
        continue;
      }
      if (members.every((member) => member.channelInfo?.platform !== info.platform)) {
        buckets.set(key, [...members, subscription]);
      }
    }
  }
  const duplicates: DuplicateSubscriptions<T>[] = [];
  const used = new Set<string>();
  for (const [key, members] of buckets) {
    const platforms = new Set(members.flatMap((member) => (member.channelInfo ? [member.channelInfo.platform] : [])));
    if (platforms.size >= 2 && platforms.size === members.length && !members.some((member) => used.has(member.id))) {
      const sorted = [...members].sort(byCreation);
      for (const member of sorted) {
        used.add(member.id);
      }
      duplicates.push({
        key,
        name: sorted[0].channelInfo?.name ?? key,
        platforms: sorted.flatMap((member) => (member.channelInfo ? [member.channelInfo.platform] : [])),
        subscriptions: sorted,
      });
    }
  }
  return duplicates;
}

/**
 * The subscriptions a channel of `platform` can be grouped with by hand: on another platform, in no creator group of two or
 * more channels, and whose name contains `query` (compared without case or accents), sorted by name.
 */
export function groupCandidates<T extends GroupableSubscription>(subscriptions: T[], platform: string, query: string, language?: string): T[] {
  const creators = membersByCreator(subscriptions);
  const needle = foldSearch(query.trim());
  return subscriptions
    .filter((subscription) => {
      const info = subscription.channelInfo;
      if (!info || info.platform === platform || (subscription.creator && (creators.get(subscription.creator)?.length ?? 0) > 1)) {
        return false;
      }
      return !needle || foldSearch(info.name).includes(needle);
    })
    .sort(
      (a, b) =>
        (a.channelInfo?.name ?? '').localeCompare(b.channelInfo?.name ?? '', language, { sensitivity: 'base', numeric: true }) || a.id.localeCompare(b.id),
    );
}
