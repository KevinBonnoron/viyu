import { Capacitor } from '@capacitor/core';
import { Preferences } from '@capacitor/preferences';
import { LocalAuthStore } from 'pocketbase';
import { clearPlaybackPositions } from '@/features/video/utils/playback-position';
import { pb, setApiBase } from '@/lib/pocketbase';
import { normalizeServerUrl, ServerConfigError } from '@/lib/server-url';

export const isStandaloneClient = Capacitor.isNativePlatform() || import.meta.env.VITE_CLIENT_TARGET === 'desktop';
const SERVER_KEY = 'viyu-server-url';
const STORAGE_PREFIX = 'viyu-remote';
let currentBase: string | undefined;

function sessionKey(key: string, origin: string): string {
  return `${STORAGE_PREFIX}:${origin}:${key}`;
}

export async function storedServerUrl(): Promise<string | undefined> {
  try {
    const stored = isStandaloneClient && Capacitor.isNativePlatform() ? (await Preferences.get({ key: SERVER_KEY })).value : localStorage.getItem(SERVER_KEY);
    return stored ? normalizeServerUrl(stored) : undefined;
  } catch {
    throw new ServerConfigError('storage');
  }
}

export async function bootstrapSession(): Promise<void> {
  if (!isStandaloneClient) {
    setApiBase(window.location.origin);
    return;
  }
  const origin = await storedServerUrl();
  if (!origin) {
    return;
  }
  currentBase = origin;
  setApiBase(origin);
  pb.authStore = new LocalAuthStore(sessionKey('pocketbase_auth', origin));
}

/** Ends the session on this device: the stored token and every resume position kept for the account. */
export function clearRemoteSession(): void {
  pb.authStore.clear();
  clearPlaybackPositions();
  if (currentBase) {
    try {
      localStorage.removeItem(sessionKey('pocketbase_auth', currentBase));
    } catch {
      // Logout still clears the live session when browser storage is disabled.
    }
  }
}

export async function connectToServer(value: string): Promise<string> {
  const origin = normalizeServerUrl(value);
  let response: Response;
  try {
    response = await fetch(`${origin}/api/health`, { cache: 'no-store', signal: AbortSignal.timeout(10_000) });
  } catch {
    throw new ServerConfigError('unreachable');
  }
  const health: unknown = await response.json().catch(() => null);
  if (!response.ok || !health || typeof health !== 'object' || !('status' in health) || health.status !== 'ok') {
    throw new ServerConfigError('unreachable');
  }
  try {
    if (Capacitor.isNativePlatform()) {
      await Preferences.set({ key: SERVER_KEY, value: origin });
    } else {
      localStorage.setItem(SERVER_KEY, origin);
    }
  } catch {
    throw new ServerConfigError('storage');
  }
  window.location.assign('/');
  return origin;
}

export async function changeServer(): Promise<void> {
  clearRemoteSession();
  try {
    if (Capacitor.isNativePlatform()) {
      await Preferences.remove({ key: SERVER_KEY });
    } else {
      localStorage.removeItem(SERVER_KEY);
    }
  } catch {
    throw new ServerConfigError('storage');
  }
  window.location.assign('/setup');
}
