import { renderHook, act, waitFor } from '@testing-library/react-native';
import { useAuthSession, type SessionStorage } from './session';
import { setTokenSource } from './api';
import type { Session } from './auth';

// The mechanics every Dwellm8 app repeats: keep the session, hand the API a
// token that is not about to expire, and forget everything on the way out.
// What an app does with a signed-in person is the app's own business.

const stored: Session = {
  idToken: 'id-1', refreshToken: 'refresh-1', uid: 'uid-1',
  email: 'ritika@firm.in', expiresAt: 2_000_000,
};

function memoryStorage(seed: Record<string, string> = {}): SessionStorage & { data: Record<string, string> } {
  const data = { ...seed };
  return {
    data,
    get: async (k) => data[k] ?? null,
    set: async (k, v) => { data[k] = v; },
    remove: async (k) => { delete data[k]; },
  };
}

function identityDouble() {
  return {
    signIn: jest.fn().mockResolvedValue(stored),
    signUp: jest.fn().mockResolvedValue(stored),
    resetPassword: jest.fn().mockResolvedValue(undefined),
    refresh: jest.fn().mockResolvedValue({ ...stored, idToken: 'id-2', expiresAt: 9_000_000 }),
  };
}

const key = 'dwellm8.test.session';

describe('useAuthSession', () => {
  beforeEach(() => jest.spyOn(Date, 'now').mockReturnValue(1_000_000));
  afterEach(() => { setTokenSource(null); jest.restoreAllMocks(); });

  it('restores a session the device already held', async () => {
    const storage = memoryStorage({ [key]: JSON.stringify(stored) });
    const { result } = await renderHook(() => useAuthSession({ identity: identityDouble(), storage, key }));

    await waitFor(() => expect(result.current.restoring).toBe(false));
    expect(result.current.session?.email).toBe('ritika@firm.in');
  });

  it('is not restoring forever when the device held nothing', async () => {
    const { result } = await renderHook(() => useAuthSession({ identity: identityDouble(), storage: memoryStorage(), key }));

    await waitFor(() => expect(result.current.restoring).toBe(false));
    expect(result.current.session).toBeNull();
  });

  it('keeps the session it signed in with', async () => {
    const storage = memoryStorage();
    const { result } = await renderHook(() => useAuthSession({ identity: identityDouble(), storage, key }));
    await waitFor(() => expect(result.current.restoring).toBe(false));

    await act(async () => { await result.current.signIn('ritika@firm.in', 'correct horse'); });

    expect(JSON.parse(storage.data[key])).toEqual(stored);
    expect(result.current.session?.uid).toBe('uid-1');
  });

  it('forgets everything on the way out', async () => {
    const storage = memoryStorage({ [key]: JSON.stringify(stored) });
    const { result } = await renderHook(() => useAuthSession({ identity: identityDouble(), storage, key }));
    await waitFor(() => expect(result.current.session).not.toBeNull());

    await act(async () => { await result.current.signOut(); });

    expect(storage.data[key]).toBeUndefined();
    expect(result.current.session).toBeNull();
  });

  it('asks for a reset link without holding a session', async () => {
    const identity = identityDouble();
    const { result } = await renderHook(() => useAuthSession({ identity, storage: memoryStorage(), key }));
    await waitFor(() => expect(result.current.restoring).toBe(false));

    await act(async () => { await result.current.resetPassword(' ritika@firm.in ') });

    expect(identity.resetPassword).toHaveBeenCalledWith('ritika@firm.in');
    expect(result.current.session).toBeNull();
  });
});
