import { useState } from 'react';
import { subscriptionCollection } from '@/collections/subscription.collection';
import { api } from '@/lib/api';
import { useAuth } from '@/providers/auth-provider';
import { useProfiles } from '@/providers/profiles-provider';
import { useSubscriptions } from '@/providers/subscriptions-provider';

export function useChannelSubscription(channel: { id: string; platform: string; name: string; thumbnail?: string } | null) {
  const { user } = useAuth();
  const { activeProfile } = useProfiles();
  const { byChannel, isReady } = useSubscriptions();
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const existing = channel ? byChannel.get(channel.id) : undefined;
  const toggle = async () => {
    if (!user || !activeProfile || !channel?.id || !isReady || pending) {
      return;
    }
    setPending(true);
    setError(false);
    try {
      if (existing) {
        await subscriptionCollection.delete(existing.id).isPersisted.promise;
      } else {
        // The server links the creator to a same-name channel on another platform on its own; the new row reaches the collection through realtime.
        await api.subscriptions.create(activeProfile.id, channel.id);
      }
    } catch {
      setError(true);
    } finally {
      setPending(false);
    }
  };
  return { subscribed: !!existing, pending, disabled: !user || !activeProfile || !channel?.id || !isReady || pending, error, toggle };
}

export function useGroupSubscriptions() {
  const { activeProfile } = useProfiles();
  const [pendingKey, setPendingKey] = useState<string>();
  const [error, setError] = useState(false);
  /** Resolves with the server's answer, or `undefined` when nothing ran or the request failed (`error` is then set). */
  const run = async <T>(key: string, request: (profile: string) => Promise<T>): Promise<T | undefined> => {
    if (!activeProfile || pendingKey) {
      return undefined;
    }
    setPendingKey(key);
    setError(false);
    try {
      return await request(activeProfile.id);
    } catch {
      setError(true);
      return undefined;
    } finally {
      setPendingKey(undefined);
    }
  };
  return {
    group: (key: string, subscriptionIds: string[]) => run(key, (profile) => api.subscriptions.group(profile, subscriptionIds)),
    ungroup: (creator: string) => run(creator, (profile) => api.subscriptions.ungroup(profile, creator)),
    dismissError: () => setError(false),
    pendingKey,
    error,
  };
}
