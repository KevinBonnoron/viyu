import { createContext, type ReactNode, type RefObject, useCallback, useContext, useMemo, useRef, useState } from 'react';

const storageKey = 'viyu.private-playback';
type PlaybackPrivacy = {
  privatePlayback: boolean;
  privatePlaybackRef: RefObject<boolean>;
  privacyVersion: RefObject<number>;
  setPrivatePlayback: (enabled: boolean) => void;
};
const PlaybackPrivacyContext = createContext<PlaybackPrivacy | null>(null);

export function PlaybackPrivacyProvider({ children }: { children: ReactNode }) {
  const [privatePlayback, updatePrivatePlayback] = useState(() => {
    try {
      return sessionStorage.getItem(storageKey) === 'true';
    } catch {
      return false;
    }
  });
  const privatePlaybackRef = useRef(privatePlayback);
  const privacyVersion = useRef(0);
  const setPrivatePlayback = useCallback((enabled: boolean) => {
    privatePlaybackRef.current = enabled;
    privacyVersion.current++;
    updatePrivatePlayback(enabled);
    try {
      sessionStorage.setItem(storageKey, String(enabled));
    } catch {
      // The current tab can still use private playback without storage access.
    }
  }, []);
  const value = useMemo(() => ({ privatePlayback, privatePlaybackRef, privacyVersion, setPrivatePlayback }), [privatePlayback, setPrivatePlayback]);
  return <PlaybackPrivacyContext.Provider value={value}>{children}</PlaybackPrivacyContext.Provider>;
}

export function usePlaybackPrivacy() {
  const context = useContext(PlaybackPrivacyContext);
  if (!context) {
    throw new Error('usePlaybackPrivacy must be used within PlaybackPrivacyProvider');
  }
  return context;
}
