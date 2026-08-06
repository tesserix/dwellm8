import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import * as SecureStore from 'expo-secure-store';
import { Identity, identityFromEnv, setTokenSource, type Session } from '@dwellm8/mobile-shared';

/**
 * Who is signed into the manager's app.
 *
 * The id token lives in the keychain rather than in memory alone, because a
 * manager who reopens the app on a site visit should not have to sign in
 * again; it is refreshed a minute before it expires rather than after a 401,
 * so a payout release never fails on a stale token halfway through.
 */

const key = 'dwellm8.pm.session';
// A token about to expire is a token that will expire mid-request.
const skew = 60_000;

type SessionState = {
  session: Session | null;
  identity: Identity | null;
  restoring: boolean;
  signIn: (email: string, password: string) => Promise<Session>;
  signUp: (email: string, password: string) => Promise<Session>;
  signOut: () => Promise<void>;
};

const Ctx = createContext<SessionState | null>(null);

export function SessionProvider({ children }: { children: React.ReactNode }) {
  const identity = useMemo(() => identityFromEnv(), []);
  const [session, setSession] = useState<Session | null>(null);
  const [restoring, setRestoring] = useState(true);

  const remember = useCallback(async (s: Session | null) => {
    setSession(s);
    if (s) await SecureStore.setItemAsync(key, JSON.stringify(s));
    else await SecureStore.deleteItemAsync(key);
  }, []);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const raw = await SecureStore.getItemAsync(key);
        if (live && raw) setSession(JSON.parse(raw) as Session);
      } finally {
        if (live) setRestoring(false);
      }
    })();
    return () => { live = false; };
  }, []);

  // The client asks for a token per request; this is where it comes from.
  useEffect(() => {
    if (!identity) return;
    setTokenSource(async () => {
      if (!session) return null;
      if (session.expiresAt - skew > Date.now()) return session.idToken;
      try {
        const fresh = await identity.refresh(session.refreshToken);
        await remember(fresh);
        return fresh.idToken;
      } catch {
        await remember(null);
        return null;
      }
    });
    return () => setTokenSource(null);
  }, [identity, session, remember]);

  const value = useMemo<SessionState>(() => ({
    session,
    identity,
    restoring,
    signIn: async (email, password) => {
      if (!identity) throw new Error('This build has no sign-in configured');
      const s = await identity.signIn(email.trim(), password);
      await remember(s);
      return s;
    },
    signUp: async (email, password) => {
      if (!identity) throw new Error('This build has no sign-in configured');
      const s = await identity.signUp(email.trim(), password);
      await remember(s);
      return s;
    },
    signOut: () => remember(null),
  }), [identity, session, restoring, remember]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSession(): SessionState {
  const v = useContext(Ctx);
  if (!v) throw new Error('useSession outside SessionProvider');
  return v;
}
