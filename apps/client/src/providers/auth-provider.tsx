import type { RecordModel } from 'pocketbase';
import { createContext, type ReactNode, useCallback, useContext, useEffect, useState } from 'react';
import { pb } from '@/lib/pocketbase';
import { clearRemoteSession } from '@/lib/server';
import { keepSessionFresh } from '@/lib/session';

interface AuthContextValue {
  user: RecordModel | null;
  isAuthenticated: boolean;
  isAdmin: boolean;
  adminReady: boolean;
  adminError: boolean;
  retryAdmin: () => void;
  login: (email: string, password: string) => Promise<void>;
  register: (email: string, password: string, passwordConfirm: string) => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<RecordModel | null>(pb.authStore.isValid ? pb.authStore.record : null);
  const [adminAccess, setAdminAccess] = useState<{ userId: string; allowed: boolean; error: boolean } | null>(null);
  const [adminAttempt, setAdminAttempt] = useState(0);
  const retryAdmin = useCallback(() => setAdminAttempt((attempt) => attempt + 1), []);

  useEffect(() => {
    setAdminAccess(null);
    if (!user) {
      return;
    }
    const controller = new AbortController();
    void pb
      .send<{ isAdmin: boolean }>('/api/account', { method: 'GET', signal: controller.signal, requestKey: null })
      .then((capabilities) => {
        if (!controller.signal.aborted) {
          setAdminAccess({ userId: user.id, allowed: capabilities.isAdmin === true, error: false });
        }
      })
      .catch(() => {
        if (!controller.signal.aborted) {
          setAdminAccess({ userId: user.id, allowed: false, error: true });
        }
      });
    return () => controller.abort();
  }, [user, adminAttempt]);

  useEffect(() => {
    return pb.authStore.onChange((_token, record) => {
      // A renewed token carries the same record: keep the object, so nothing keyed on the user reloads.
      setUser((current) => {
        const next = pb.authStore.isValid ? record : null;
        return current && next && current.id === next.id && current.updated === next.updated ? current : next;
      });
    }, true);
  }, []);

  // The token is renewed at boot, every twelve hours and when the window regains focus near its expiry; a refusal ends the session (see `installSessionHandlers`).
  const signedIn = !!user;
  useEffect(() => (signedIn ? keepSessionFresh() : undefined), [signedIn]);

  const login = useCallback(async (email: string, password: string) => {
    await pb.collection('users').authWithPassword(email, password);
  }, []);

  const register = useCallback(async (email: string, password: string, passwordConfirm: string) => {
    await pb.collection('users').create({ email, password, passwordConfirm });
    await pb.collection('users').authWithPassword(email, password);
  }, []);

  const logout = useCallback(() => {
    // Also forgets the resume positions this device kept for the account.
    clearRemoteSession();
    window.location.assign('/login');
  }, []);

  const access = user && adminAccess?.userId === user.id ? adminAccess : null;
  return (
    <AuthContext.Provider
      value={{
        user,
        isAuthenticated: !!user,
        isAdmin: access?.allowed ?? false,
        adminReady: !user || !!access,
        adminError: access?.error ?? false,
        retryAdmin,
        login,
        register,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const context = useContext(AuthContext);
  if (!context) {
    throw new Error('useAuth must be used within AuthProvider');
  }
  return context;
}
