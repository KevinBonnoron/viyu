import { eq, useLiveQuery } from '@tanstack/react-db';
import { createContext, type ReactNode, useCallback, useContext, useEffect, useMemo, useRef, useState } from 'react';
import { profileCollection } from '@/collections/profile.collection';
import { SYSTEM_FIELDS } from '@/lib/collection-writes';
import type { ProfileRecord } from '@/types';
import { useAuth } from './auth-provider';

export interface ProfileInput {
  name: string;
  /** Key of a profile icon; empty or omitted for the default. */
  icon?: string;
  /** Accent palette; empty or omitted for the device palette. */
  theme?: string;
}

const profileFields = (input: ProfileInput) => ({ name: input.name.trim(), icon: input.icon ?? '', theme: input.theme ?? '' });

interface ProfilesContextValue {
  profiles: ProfileRecord[];
  activeProfile: ProfileRecord | null;
  isReady: boolean;
  isError: boolean;
  selectProfile: (id: string) => void;
  /** Creates a profile and makes it the active one, unless `select` is false. */
  createProfile: (input: ProfileInput, options?: { select?: boolean }) => Promise<ProfileRecord>;
  updateProfile: (id: string, input: ProfileInput) => Promise<void>;
  deleteProfile: (id: string) => Promise<void>;
  retry: () => void;
}

const ProfilesContext = createContext<ProfilesContextValue | null>(null);
const storageKey = (userId: string) => `viyu-profile:${userId}`;

function rememberProfile(userId: string, profileId: string) {
  try {
    localStorage.setItem(storageKey(userId), profileId);
  } catch {
    // Selection still works for this session when browser storage is unavailable.
  }
}

export function ProfilesProvider({ children }: { children: ReactNode }) {
  const { user } = useAuth();
  const userId = user?.id;
  const [selectedId, setSelectedId] = useState<string>();
  const [revision, setRevision] = useState(0);
  const [creationFailed, setCreationFailed] = useState(false);
  const defaultRequested = useRef<string>(undefined);
  // The revision is part of the query's identity: bumping it (retry) rebuilds the query and reloads.
  const query = useLiveQuery({
    queryKey: ['profiles', userId ?? '', revision],
    query: (q) =>
      userId
        ? q
            .from({ profiles: profileCollection })
            .where(({ profiles }) => eq(profiles.user, userId))
            .orderBy(({ profiles }) => profiles.created)
        : undefined,
  });
  const profiles = useMemo<ProfileRecord[]>(() => (userId ? (query.data ?? []) : []), [userId, query.data]);

  useEffect(() => {
    setSelectedId(undefined);
    setCreationFailed(false);
    defaultRequested.current = undefined;
    if (!userId) {
      return;
    }
    try {
      setSelectedId(localStorage.getItem(storageKey(userId)) || undefined);
    } catch {
      // The first profile is the fallback when browser storage is unavailable.
    }
  }, [userId]);

  // An account without a profile (the server creates one at signup, but may have failed to) gets a default one.
  useEffect(() => {
    if (!user || !query.isReady || profiles.length > 0 || defaultRequested.current === user.id) {
      return;
    }
    defaultRequested.current = user.id;
    const created: ProfileRecord = {
      id: profileCollection.utils.newId(),
      ...SYSTEM_FIELDS,
      user: user.id,
      name: String(user.name ?? '').trim() || 'Profile 1',
      icon: '',
      theme: '',
    };
    profileCollection.insert(created).isPersisted.promise.catch(() => setCreationFailed(true));
  }, [user, query.isReady, profiles.length]);

  const activeProfile = profiles.find((profile) => profile.id === selectedId) ?? profiles[0] ?? null;
  const isReady = !!user && query.isReady && profiles.length > 0;
  const isError = !!user && (query.isError || creationFailed);
  useEffect(() => {
    if (userId && activeProfile) {
      rememberProfile(userId, activeProfile.id);
    }
  }, [userId, activeProfile]);

  const selectProfile = useCallback(
    (id: string) => {
      if (user && profiles.some((profile) => profile.id === id)) {
        setSelectedId(id);
      }
    },
    [user, profiles],
  );
  const createProfile = useCallback(
    async (input: ProfileInput, { select = true }: { select?: boolean } = {}) => {
      if (!user) {
        throw new Error('An account is required');
      }
      const profile: ProfileRecord = { id: profileCollection.utils.newId(), ...SYSTEM_FIELDS, user: user.id, ...profileFields(input) };
      await profileCollection.insert(profile).isPersisted.promise;
      if (select) {
        setSelectedId(profile.id);
      }
      return profile;
    },
    [user],
  );
  const updateProfile = useCallback(
    async (id: string, input: ProfileInput) => {
      if (!user || !input.name.trim() || !profiles.some((profile) => profile.id === id)) {
        throw new Error('Invalid profile');
      }
      const fields = profileFields(input);
      await profileCollection.update(id, (draft) => {
        Object.assign(draft, fields);
      }).isPersisted.promise;
    },
    [user, profiles],
  );
  const deleteProfile = useCallback(
    async (id: string) => {
      if (!user || profiles.length < 2 || !profiles.some((profile) => profile.id === id)) {
        throw new Error('A profile must remain available');
      }
      await profileCollection.delete(id).isPersisted.promise;
      setSelectedId((current) => (current === id ? undefined : current));
    },
    [user, profiles],
  );
  const retry = useCallback(() => {
    setCreationFailed(false);
    defaultRequested.current = undefined;
    setRevision((value) => value + 1);
  }, []);
  const value = useMemo(
    () => ({ profiles, activeProfile, isReady, isError, selectProfile, createProfile, updateProfile, deleteProfile, retry }),
    [profiles, activeProfile, isReady, isError, selectProfile, createProfile, updateProfile, deleteProfile, retry],
  );
  return <ProfilesContext.Provider value={value}>{children}</ProfilesContext.Provider>;
}

export function useProfiles() {
  const context = useContext(ProfilesContext);
  if (!context) {
    throw new Error('useProfiles must be used within ProfilesProvider');
  }
  return context;
}
