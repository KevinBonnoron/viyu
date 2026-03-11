import { eq, useLiveQuery } from '@tanstack/react-db';
import { createContext, type ReactNode, useContext, useMemo } from 'react';
import { subscriptionCollection } from '@/collections/subscription.collection';
import { filterEnabledPlatforms } from '@/lib/source-activation';
import type { ChannelRecord, SubscriptionRecord } from '@/types';
import { useProfiles } from './profiles-provider';
import { useSources } from './sources-provider';

/** A subscription with the channel record it carries (`expand.channel`). */
export type SubscriptionItem = SubscriptionRecord & { channelInfo?: ChannelRecord };

interface SubscriptionsContextValue {
  /** The active profile's subscriptions, those of a disabled platform left out. */
  data: SubscriptionItem[];
  /** The subscription to a channel record by the channel's id: one lookup for any number of cards. */
  byChannel: ReadonlyMap<string, SubscriptionItem>;
  isReady: boolean;
  isError: boolean;
}

const SubscriptionsContext = createContext<SubscriptionsContextValue | null>(null);

/** The one subscriptions query of the app: every card, filter and page reads this instead of asking the collection again. */
export function SubscriptionsProvider({ children }: { children: ReactNode }) {
  const { activeProfile, isReady: profileReady, isError: profileError } = useProfiles();
  const { isEnabled } = useSources();
  const profileId = activeProfile?.id;
  // The profile id is part of the query itself, so the query is rebuilt (not reused) when the active profile changes.
  const query = useLiveQuery({
    query: (q) =>
      profileId ? q.from({ subscriptions: subscriptionCollection }).where(({ subscriptions }) => eq(subscriptions.profile, profileId)) : undefined,
  });
  const value = useMemo<SubscriptionsContextValue>(() => {
    // Subscriptions of a disabled platform stay stored but are not shown; a channel not loaded is kept (nothing is hidden on doubt).
    const data = filterEnabledPlatforms(
      (query.data ?? []).map((subscription): SubscriptionItem => ({ ...subscription, channelInfo: subscription.expand?.channel })),
      (subscription) => subscription.channelInfo?.platform,
      isEnabled,
    );
    return {
      data,
      byChannel: new Map(data.map((subscription) => [subscription.channel, subscription])),
      isReady: profileReady && !!profileId && query.isReady,
      isError: profileError || query.isError,
    };
  }, [query.data, query.isReady, query.isError, profileReady, profileError, profileId, isEnabled]);
  return <SubscriptionsContext.Provider value={value}>{children}</SubscriptionsContext.Provider>;
}

export function useSubscriptions(): SubscriptionsContextValue {
  const context = useContext(SubscriptionsContext);
  if (!context) {
    throw new Error('useSubscriptions must be used within SubscriptionsProvider');
  }
  return context;
}
