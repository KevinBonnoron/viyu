import { useLiveQuery } from '@tanstack/react-db';
import { useMemo } from 'react';
import { settingCollection } from '@/collections/setting.collection';
import { useAuth } from '@/providers/auth-provider';
import type { SettingsRecord } from '@/types';

/** The signed-in user's settings row (the server rule keeps the collection to it); absent until saved once, so a missing row means every default. */
export function useUserSettings(): { settings: SettingsRecord | undefined; isReady: boolean; isError: boolean } {
  const { user } = useAuth();
  const { data, isReady, isError } = useLiveQuery({ query: (q) => q.from({ settings: settingCollection }) });
  const settings = useMemo(() => (user ? data?.find((setting) => setting.user === user.id) : undefined), [data, user]);
  return { settings, isReady: isReady && !!user, isError };
}

/** Whether the user chose to hide videos only a channel's subscribers can watch. Nothing is hidden until the settings are known. */
export function useHideSubscriberOnly(): boolean {
  const { settings, isReady, isError } = useUserSettings();
  return isReady && !isError && settings?.hide_subscriber_only === true;
}

/** Whether the user chose to hide the videos already watched from discovery lists. Nothing is hidden until the settings are known. */
export function useHideWatched(): boolean {
  const { settings, isReady, isError } = useUserSettings();
  return isReady && !isError && settings?.hide_watched === true;
}
