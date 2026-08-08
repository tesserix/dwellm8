import { useCallback, useEffect, useMemo, useState } from 'react';
import { setTokenSource } from './api';
import type { Session } from './auth';

/**
 * The signed-in session, for any Dwellm8 app (ADR-0027).
 *
 * Every surface repeats the same mechanics — hold the session, restore it on a
 * cold start, hand the API a token that is not about to expire, forget it on
 * the way out — while differing entirely in what a signed-in person may then
 * do. Only the mechanics live here.
 *
 * Storage is injected rather than imported: this package has no dependencies,
 * and the surfaces do not agree on one anyway — a native app keeps the session
 * in the keychain, the web app cannot.
 */

/** SessionStorage is the little of a key-value store this needs. */
export type SessionStorage = {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  remove(key: string): Promise<void>;
};

/** The part of Identity a session needs, so a test can stand in for it. */
export type SessionIdentity = {
  signIn(email: string, password: string): Promise<Session>;
  signUp(email: string, password: string): Promise<Session>;
  resetPassword(email: string): Promise<void>;
  refresh(refreshToken: string): Promise<Session>;
};

export type AuthSession = {
  session: Session | null;
  /** True until the device has been asked what it already held. */
  restoring: boolean;
  signIn(email: string, password: string): Promise<Session>;
  signUp(email: string, password: string): Promise<Session>;
  signOut(): Promise<void>;
  resetPassword(email: string): Promise<void>;
};

// A token about to expire is a token that will expire mid-request.
const skew = 60_000;

export function useAuthSession(
  { identity, storage, key }: { identity: SessionIdentity | null; storage: SessionStorage; key: string },
): AuthSession {
  const [session, setSession] = useState<Session | null>(null);
  const [restoring, setRestoring] = useState(true);

  const remember = useCallback(async (s: Session | null) => {
    setSession(s);
    if (s) await storage.set(key, JSON.stringify(s));
    else await storage.remove(key);
  }, [storage, key]);

  useEffect(() => {
    let live = true;
    (async () => {
      try {
        const raw = await storage.get(key);
        if (live && raw) setSession(JSON.parse(raw) as Session);
      } finally {
        if (live) setRestoring(false);
      }
    })();
    return () => { live = false; };
  }, [storage, key]);

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

  return useMemo<AuthSession>(() => {
    const required = () => {
      if (!identity) throw new Error('This build has no sign-in configured');
      return identity;
    };
    return {
      session,
      restoring,
      signIn: async (email, password) => {
        const s = await required().signIn(email.trim(), password);
        await remember(s);
        return s;
      },
      signUp: async (email, password) => {
        const s = await required().signUp(email.trim(), password);
        await remember(s);
        return s;
      },
      signOut: () => remember(null),
      resetPassword: (email) => required().resetPassword(email.trim()),
    };
  }, [session, restoring, identity, remember]);
}
