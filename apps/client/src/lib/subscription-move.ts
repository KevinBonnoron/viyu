import type { GroupableSubscription } from '@/lib/channel-groups';
import { foldSearch } from '@/lib/text';

/** One line of the move checklist: a lone channel, or a creator group of two or more channels moving together. */
export interface MoveRow {
  /** The creator id of a group, the subscription id otherwise. */
  key: string;
  name: string;
  thumbnail: string;
  /** The platform of each member, in the order of the members. */
  platforms: string[];
  /** The channel name of each member: a group is found by any of them. */
  channelNames: string[];
  /** Every subscription the row stands for. */
  subscriptionIds: string[];
}

const byCreation = (a: GroupableSubscription, b: GroupableSubscription) => (a.created ?? '').localeCompare(b.created ?? '');

/**
 * The checklist of a profile: each creator group of two or more subscriptions is one row, named after the creator (or its first channel),
 * so selecting it selects all its members, which the server moves together. Rows are sorted by name; a subscription whose channel is not loaded is left out.
 */
export function buildMoveRows(subscriptions: GroupableSubscription[], language?: string): MoveRow[] {
  const groups = new Map<string, GroupableSubscription[]>();
  for (const subscription of subscriptions) {
    if (subscription.creator) {
      groups.set(subscription.creator, [...(groups.get(subscription.creator) ?? []), subscription]);
    }
  }
  const rows: MoveRow[] = [];
  const emitted = new Set<string>();
  for (const subscription of subscriptions) {
    const creator = subscription.creator;
    const members = creator ? groups.get(creator) : undefined;
    if (creator && members && members.length >= 2) {
      if (emitted.has(creator)) {
        continue;
      }
      emitted.add(creator);
      const shown = [...members].sort(byCreation).filter((member) => member.channelInfo);
      if (shown.length === 0) {
        continue;
      }
      rows.push({
        key: creator,
        name: members.map((member) => member.expand?.creator?.name).find((name) => name) || (shown[0].channelInfo?.name ?? ''),
        thumbnail: shown[0].channelInfo?.thumbnail ?? '',
        platforms: shown.map((member) => member.channelInfo?.platform ?? ''),
        channelNames: shown.map((member) => member.channelInfo?.name ?? ''),
        subscriptionIds: members.map((member) => member.id),
      });
    } else if (subscription.channelInfo) {
      rows.push({
        key: subscription.id,
        name: subscription.channelInfo.name,
        thumbnail: subscription.channelInfo.thumbnail,
        platforms: [subscription.channelInfo.platform],
        channelNames: [subscription.channelInfo.name],
        subscriptionIds: [subscription.id],
      });
    }
  }
  return rows.sort((a, b) => a.name.localeCompare(b.name, language, { sensitivity: 'base' }));
}

/** The rows whose name, or the name of one of their channels, contains the query without regard to case or accents. */
export function filterMoveRows(rows: MoveRow[], query: string): MoveRow[] {
  const needle = foldSearch(query.trim());
  return needle ? rows.filter((row) => [row.name, ...row.channelNames].some((name) => foldSearch(name).includes(needle))) : rows;
}

/** The subscription ids of the selected rows, in the order of the rows. */
export function selectedSubscriptionIds(rows: MoveRow[], selected: ReadonlySet<string>): string[] {
  return rows.filter((row) => selected.has(row.key)).flatMap((row) => row.subscriptionIds);
}
