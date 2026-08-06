/**
 * Signing in (ADR-0027).
 *
 * Identity Platform keeps a user pool per surface, so the tenant id is not
 * decoration: it is why a tenant's password cannot open the manager's app. It
 * travels on every call here, and the API checks the same boundary again on
 * the way in — a token verified for one pool is refused by another's routes.
 *
 * Nothing in this file decides what anybody may do. It says who signed in; the
 * organisation they act for is a database lookup the API owns.
 */

/** Session is one signed-in person, and when their token stops being one. */
export type Session = {
  idToken: string;
  refreshToken: string;
  uid: string;
  email: string;
  /** Epoch milliseconds. Compare against Date.now() before using idToken. */
  expiresAt: number;
};

export type IdentityConfig = {
  apiKey: string;
  tenantId: string;
};

/** AuthError carries the sentence to show and the constant to branch on. */
export class AuthError extends Error {
  reason: string;
  constructor(reason: string, message: string) {
    super(message);
    this.reason = reason;
  }
}

// GIP answers with a shouted constant. A person reads the right-hand side.
const refusals: Record<string, string> = {
  INVALID_LOGIN_CREDENTIALS: 'That email and password do not match',
  EMAIL_NOT_FOUND: 'That email and password do not match',
  INVALID_PASSWORD: 'That email and password do not match',
  USER_DISABLED: 'That account has been disabled — ask your administrator',
  EMAIL_EXISTS: 'That email already has an account — sign in instead',
  WEAK_PASSWORD: 'Choose a password of at least six characters',
  INVALID_EMAIL: 'That does not look like an email address',
  TOO_MANY_ATTEMPTS_TRY_LATER: 'Too many attempts — wait a few minutes and try again',
  TOKEN_EXPIRED: 'Your sign-in has expired — sign in again',
};

const identityToolkit = 'https://identitytoolkit.googleapis.com/v1/accounts';
const secureToken = 'https://securetoken.googleapis.com/v1/token';

export class Identity {
  private cfg: IdentityConfig;
  constructor(cfg: IdentityConfig) {
    this.cfg = cfg;
  }

  signIn(email: string, password: string): Promise<Session> {
    return this.password('signInWithPassword', email, password);
  }

  signUp(email: string, password: string): Promise<Session> {
    return this.password('signUp', email, password);
  }

  /** refresh trades a refresh token for a fresh id token. The refresh token
   * outlives the id token by design — that is the whole point of it. */
  async refresh(refreshToken: string): Promise<Session> {
    const res = await fetch(`${secureToken}?key=${this.cfg.apiKey}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: `grant_type=refresh_token&refresh_token=${encodeURIComponent(refreshToken)}`,
    });
    const body = parse(await res.text());
    if (!res.ok) throw refusal(body);
    return {
      idToken: String(body.id_token ?? ''),
      refreshToken: String(body.refresh_token ?? refreshToken),
      uid: String(body.user_id ?? ''),
      email: String(body.email ?? ''),
      expiresAt: Date.now() + Number(body.expires_in ?? 3600) * 1000,
    };
  }

  private async password(op: string, email: string, password: string): Promise<Session> {
    const res = await fetch(`${identityToolkit}:${op}?key=${this.cfg.apiKey}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        email, password, tenantId: this.cfg.tenantId, returnSecureToken: true,
      }),
    });
    const body = parse(await res.text());
    if (!res.ok) throw refusal(body);
    return {
      idToken: String(body.idToken ?? ''),
      refreshToken: String(body.refreshToken ?? ''),
      uid: String(body.localId ?? ''),
      email: String(body.email ?? email),
      expiresAt: Date.now() + Number(body.expiresIn ?? 3600) * 1000,
    };
  }
}

function parse(text: string): Record<string, unknown> {
  try {
    return text ? (JSON.parse(text) as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

function refusal(body: Record<string, unknown>): AuthError {
  const err = body.error as { message?: string } | undefined;
  // GIP appends its own explanation after a space-colon-space; the constant is
  // the part before it.
  const reason = (err?.message ?? 'SIGN_IN_FAILED').split(' :')[0].trim();
  return new AuthError(reason, refusals[reason] ?? 'That sign-in could not be completed');
}

/**
 * identityFromEnv builds the client the environment names, or null — and null
 * is a mode, not a failure: an app without a pool runs on its demonstration
 * data, exactly as apiFromEnv does.
 */
export function identityFromEnv(): Identity | null {
  const apiKey = process.env.EXPO_PUBLIC_GIP_API_KEY;
  const tenantId = process.env.EXPO_PUBLIC_GIP_TENANT_ID;
  if (!apiKey || !tenantId) return null;
  return new Identity({ apiKey, tenantId });
}
