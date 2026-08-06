import React, { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';
import * as SecureStore from 'expo-secure-store';
import {
  apiFromEnv, Identity, identityFromEnv, setTokenSource, type Session,
} from '@dwellm8/mobile-shared';

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

// Remembered per sign-in, because verification is a fact about the address and
// never comes undone: a manager who proved it on this device must not be sent
// back to the code screen by a cold start on a site with no signal.
const verifiedKey = (uid: string) => `dwellm8.pm.verified.${uid}`;

async function rememberedVerified(uid: string): Promise<true | null> {
  return (await SecureStore.getItemAsync(verifiedKey(uid))) === '1' ? true : null;
}

type SessionState = {
  session: Session | null;
  identity: Identity | null;
  restoring: boolean;
  /** null while we are still asking. A verified sign-in is not yet a firm, and
   * the difference decides which screen the app opens on. */
  hasFirm: boolean | null;
  refreshFirm: () => Promise<void>;
  /** Whether the address this sign-in used has been proved (#282). null while
   * we are still asking; true for a phone sign-in, which arrived by OTP. */
  verified: boolean | null;
  refreshVerification: () => Promise<void>;
  /** Whether the firm has filed its statutory details at all (#282). */
  registered: boolean | null;
  refreshRegistration: () => Promise<void>;
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

  const [hasFirm, setHasFirm] = useState<boolean | null>(null);

  const refreshFirm = useCallback(async () => {
    if (!session) { setHasFirm(null); return; }
    const api = apiFromEnv();
    if (!api) { setHasFirm(true); return; }
    try {
      setHasFirm(!!(await api.me()));
    } catch {
      // A network failure is not "no firm" — it must not send a working
      // manager back through onboarding.
      setHasFirm(null);
    }
  }, [session]);

  useEffect(() => { void refreshFirm(); }, [refreshFirm]);

  const [verified, setVerified] = useState<boolean | null>(null);

  const refreshVerification = useCallback(async () => {
    if (!session) { setVerified(null); return; }
    const api = apiFromEnv();
    if (!api) { setVerified(true); return; }
    try {
      const ok = (await api.emailVerification()).verified;
      setVerified(ok);
      if (ok) await SecureStore.setItemAsync(verifiedKey(session.uid), '1');
    } catch {
      setVerified(await rememberedVerified(session.uid));
    }
  }, [session]);

  useEffect(() => { void refreshVerification(); }, [refreshVerification]);

  // What this device already knows, while the answer above is in flight — so
  // the gate does not flash the code screen at somebody who passed it weeks ago.
  useEffect(() => {
    if (!session) return;
    let live = true;
    void rememberedVerified(session.uid).then((ok) => {
      if (live && ok) setVerified((v) => (v === null ? true : v));
    });
    return () => { live = false; };
  }, [session]);

  const [registered, setRegistered] = useState<boolean | null>(null);

  const refreshRegistration = useCallback(async () => {
    if (!session || !hasFirm) { setRegistered(null); return; }
    const api = apiFromEnv();
    if (!api) { setRegistered(true); return; }
    try {
      // Draft means nothing has been filed yet. A firm that has filed and still
      // has gaps is let through — the checklist stays reachable from the tabs.
      setRegistered((await api.opsRegistration()).state !== 'draft');
    } catch {
      setRegistered(null);
    }
  }, [session, hasFirm]);

  useEffect(() => { void refreshRegistration(); }, [refreshRegistration]);

  const value = useMemo<SessionState>(() => ({
    session,
    identity,
    restoring,
    hasFirm,
    refreshFirm,
    verified,
    refreshVerification,
    registered,
    refreshRegistration,
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
    signOut: async () => {
      setHasFirm(null);
      setVerified(null);
      setRegistered(null);
      await remember(null);
    },
  }), [identity, session, restoring, hasFirm, refreshFirm, verified, refreshVerification,
    registered, refreshRegistration, remember]);

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSession(): SessionState {
  const v = useContext(Ctx);
  if (!v) throw new Error('useSession outside SessionProvider');
  return v;
}
