import { Identity, identityFromEnv, AuthError } from './auth';

// Signing in is the one call the apps make to somebody other than our own API.
// What matters: the tenant travels on every request (a pool per surface is the
// user-base boundary), the refusal a person reads is a sentence rather than
// GIP's shouted constant, and a session knows when it goes stale.

function reply(body: unknown, status = 200) {
  return jest.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  });
}

const config = { apiKey: 'key-1', tenantId: 'dwellm8-ops-r8klu' };

const signedIn = {
  idToken: 'id-1', refreshToken: 'refresh-1', expiresIn: '3600',
  localId: 'uid-1', email: 'ritika@example.test',
};

describe('Identity', () => {
  beforeEach(() => jest.spyOn(Date, 'now').mockReturnValue(1_000_000));
  afterEach(() => jest.restoreAllMocks());

  it('signs in against the surface tenant and returns a session', async () => {
    const fetchMock = reply(signedIn);
    global.fetch = fetchMock as unknown as typeof fetch;

    const session = await new Identity(config).signIn('ritika@example.test', 'correct horse');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://identitytoolkit.googleapis.com/v1/accounts:signInWithPassword?key=key-1');
    expect(JSON.parse(init.body)).toEqual({
      email: 'ritika@example.test', password: 'correct horse',
      tenantId: 'dwellm8-ops-r8klu', returnSecureToken: true,
    });
    expect(session).toEqual({
      idToken: 'id-1', refreshToken: 'refresh-1', uid: 'uid-1',
      email: 'ritika@example.test', expiresAt: 1_000_000 + 3600 * 1000,
    });
  });

  it('signs a new manager up in the same tenant', async () => {
    const fetchMock = reply(signedIn);
    global.fetch = fetchMock as unknown as typeof fetch;

    await new Identity(config).signUp('ritika@example.test', 'correct horse');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toContain('accounts:signUp');
    expect(JSON.parse(init.body).tenantId).toBe('dwellm8-ops-r8klu');
  });

  it('turns a refusal into a sentence a person can act on', async () => {
    global.fetch = reply({ error: { message: 'INVALID_LOGIN_CREDENTIALS' } }, 400) as unknown as typeof fetch;

    await expect(new Identity(config).signIn('a@b.test', 'wrong'))
      .rejects.toThrow('That email and password do not match');
  });

  it('says so plainly when the email is already somebody', async () => {
    global.fetch = reply({ error: { message: 'EMAIL_EXISTS' } }, 400) as unknown as typeof fetch;

    await expect(new Identity(config).signUp('a@b.test', 'secret123'))
      .rejects.toThrow('That email already has an account — sign in instead');
  });

  it('carries the reason on the error for a caller that wants to branch', async () => {
    global.fetch = reply({ error: { message: 'WEAK_PASSWORD : Password should be at least 6 characters' } }, 400) as unknown as typeof fetch;

    await expect(new Identity(config).signUp('a@b.test', 'x'))
      .rejects.toMatchObject({ reason: 'WEAK_PASSWORD' } as Partial<AuthError>);
  });

  it('shows what the provider actually said when the refusal is not one we know', async () => {
    // A referrer-restricted API key answers this, and "could not be completed"
    // sent the reader looking at their password for an hour.
    global.fetch = reply({ error: { message: 'Requests from referer <empty> are blocked.' } }, 403) as unknown as typeof fetch;

    await expect(new Identity(config).signUp('a@b.test', 'secret123'))
      .rejects.toThrow('Requests from referer <empty> are blocked.');
  });

  it('sends a reset link against the surface tenant', async () => {
    const fetchMock = reply({ email: 'ritika@example.test' });
    global.fetch = fetchMock as unknown as typeof fetch;

    await new Identity(config).resetPassword('ritika@example.test');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://identitytoolkit.googleapis.com/v1/accounts:sendOobCode?key=key-1');
    expect(JSON.parse(init.body)).toEqual({
      requestType: 'PASSWORD_RESET', email: 'ritika@example.test',
      tenantId: 'dwellm8-ops-r8klu',
    });
  });

  it('does not say whether an address has an account', async () => {
    global.fetch = reply({ error: { message: 'EMAIL_NOT_FOUND' } }, 400) as unknown as typeof fetch;

    await expect(new Identity(config).resetPassword('nobody@example.test')).resolves.toBeUndefined();
  });

  it('still refuses a reset the provider would not accept', async () => {
    global.fetch = reply({ error: { message: 'INVALID_EMAIL' } }, 400) as unknown as typeof fetch;

    await expect(new Identity(config).resetPassword('not-an-address'))
      .rejects.toThrow('That does not look like an email address');
  });

  it('exchanges a refresh token for a fresh session', async () => {
    const fetchMock = reply({
      id_token: 'id-2', refresh_token: 'refresh-2', expires_in: '3600', user_id: 'uid-1',
    });
    global.fetch = fetchMock as unknown as typeof fetch;

    const session = await new Identity(config).refresh('refresh-1');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://securetoken.googleapis.com/v1/token?key=key-1');
    expect(init.body).toBe('grant_type=refresh_token&refresh_token=refresh-1');
    expect(session.idToken).toBe('id-2');
    expect(session.expiresAt).toBe(1_000_000 + 3600 * 1000);
  });
});

describe('identityFromEnv', () => {
  afterEach(() => {
    delete process.env.EXPO_PUBLIC_GIP_API_KEY;
    delete process.env.EXPO_PUBLIC_GIP_TENANT_ID;
  });

  it('is null when the app was not given a pool to sign into', () => {
    delete process.env.EXPO_PUBLIC_GIP_API_KEY;
    delete process.env.EXPO_PUBLIC_GIP_TENANT_ID;
    expect(identityFromEnv()).toBeNull();
  });

  it('builds the client from the environment', () => {
    process.env.EXPO_PUBLIC_GIP_API_KEY = 'key-1';
    process.env.EXPO_PUBLIC_GIP_TENANT_ID = 'dwellm8-ops-r8klu';
    expect(identityFromEnv()).toBeInstanceOf(Identity);
  });
});
