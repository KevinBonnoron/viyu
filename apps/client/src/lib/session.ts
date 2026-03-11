import { isTokenExpired } from 'pocketbase';
import { pb } from '@/lib/pocketbase';
import { clearRemoteSession } from '@/lib/server';

const ANONYMOUS_PATHS = new Set(['/login', '/register', '/setup']);
const REFRESH_INTERVAL_MS = 12 * 60 * 60 * 1000;
/** A token that expires within this many seconds is renewed when the window regains focus. */
const NEAR_EXPIRY_SECONDS = 24 * 60 * 60;

/**
 * The server refused the session (401): the stored token is useless, so it is dropped with everything kept for the
 * account, and the viewer is sent to the login page. A refusal while nobody is signed in changes nothing.
 */
export function handleUnauthorized(): void {
  if (!pb.authStore.token) {
    return;
  }
  clearRemoteSession();
  if (!ANONYMOUS_PATHS.has(window.location.pathname)) {
    window.location.assign('/login');
  }
}

/** Makes every PocketBase request that the server answers with 401 end the session. Call once at boot. */
export function installSessionHandlers(): void {
  pb.afterSend = (response: Response, data: unknown) => {
    if (response.status === 401) {
      handleUnauthorized();
    }
    return data;
  };
}

function refresh(): void {
  if (pb.authStore.token) {
    // A refusal (401) ends the session through `installSessionHandlers`; an unreachable server keeps the current one.
    void pb
      .collection('users')
      .authRefresh({ requestKey: null })
      .catch(() => undefined);
  }
}

/**
 * Renews the signed-in session: now, every twelve hours, and whenever the window regains focus with a token close to
 * its expiry (a laptop that slept through its renewal). Returns the function that stops it.
 */
export function keepSessionFresh(): () => void {
  refresh();
  const interval = window.setInterval(refresh, REFRESH_INTERVAL_MS);
  const onFocus = () => {
    if (pb.authStore.token && isTokenExpired(pb.authStore.token, NEAR_EXPIRY_SECONDS)) {
      refresh();
    }
  };
  window.addEventListener('focus', onFocus);
  return () => {
    window.clearInterval(interval);
    window.removeEventListener('focus', onFocus);
  };
}
