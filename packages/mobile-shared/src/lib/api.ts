/**
 * The one HTTP client every Dwellm8 app talks through.
 *
 * Typed against the API's wire shapes (services/api, internal/surface/*) —
 * money is `*_minor` integers end to end (ADR-0007), dates are strings the
 * screen formats, and nothing here invents a field the server did not send.
 *
 * Configuration is environment, not code: EXPO_PUBLIC_API_URL names the API
 * and its absence means the app runs on its demonstration data (requirements
 * §9.6) — the same build serves both, and the switch is visible in one place.
 */

/** One property as the owner surface presents it (GET /v1/owner/properties). */
export type OwnerProperty = {
  id: string;
  code: string;
  name: string;
  kind: string;
  address_line1: string;
  address_line2?: string;
  locality: string;
  city: string;
  state_code: string;
  pin: string;
  unit_count: number;
  agency?: string;
  occupied: boolean;
  lease_id?: string;
  lease_expires_on?: string;
  billed_through_on?: string;
};

/** One line of a monthly owner statement. */
export type OwnerStatementLine = {
  account: string;
  label: string;
  amount_minor: number;
};

/** One property's calendar month, net (GET .../statement?month=YYYY-MM). */
export type OwnerStatement = {
  property_id: string;
  from: string;
  to: string;
  net_amount_minor: number;
  income_amount_minor: number;
  expense_amount_minor: number;
  lines: OwnerStatementLine[];
};

/** One activity feed entry (GET /v1/owner/activity). */
export type ActivityEntry = {
  kind: string;
  occurred_at: string;
  body?: string;
};

/** One document on file (GET .../documents). */
export type OwnerDocument = {
  id: string;
  filename: string;
  content_type: string;
  created_at: string;
};

/** What an upload-URL request answers with. */
export type UploadGrant = {
  upload_url: string;
  object_path: string;
};

/** One property on the firm's portfolio (GET /v1/ops/properties). */
export type OpsProperty = {
  id: string;
  code: string;
  name: string;
  kind: string;
  address_line1: string;
  locality: string;
  city: string;
  unit_count: number;
};

/** One live tenancy and what it owes today (GET /v1/ops/arrears). */
export type OpsArrear = {
  lease_id: string;
  property: string;
  unit: string;
  locality: string;
  phone?: string;
  email?: string;
  rent_amount_minor: number;
  due_amount_minor: number;
  as_of: string;
};

/** The collection roster's headline numbers (GET /v1/ops/today). */
export type OpsToday = {
  as_of: string;
  active_tenancies: number;
  rent_roll_amount_minor: number;
  outstanding_amount_minor: number;
  tenancies_in_arrears: number;
};

export type ApiConfig = {
  /** e.g. https://api.dwellm8.com — no trailing slash. */
  baseUrl: string;
  /** Returns the signed-in user's bearer token, null when signed out. */
  getToken?: () => Promise<string | null>;
};

/** ApiError carries the status and the server's own message, which is written
 * to be shown to a person rather than parsed. */
export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export class DwellmApi {
  private cfg: ApiConfig;
  constructor(cfg: ApiConfig) {
    this.cfg = { ...cfg, baseUrl: cfg.baseUrl.replace(/\/+$/, '') };
  }

  private async request<T>(method: string, path: string, body?: unknown): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json' };
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const token = this.cfg.getToken ? await this.cfg.getToken() : null;
    if (token) headers.Authorization = `Bearer ${token}`;

    const res = await fetch(this.cfg.baseUrl + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) {
      let message = `request failed (${res.status})`;
      try {
        const parsed = JSON.parse(text) as { error?: string };
        if (parsed.error) message = parsed.error;
      } catch {
        /* the body was not JSON; the status alone will have to explain */
      }
      throw new ApiError(res.status, message);
    }
    return (text ? JSON.parse(text) : {}) as T;
  }

  /* ------------------------------------------------------- owner surface */

  async ownerProperties(): Promise<OwnerProperty[]> {
    const out = await this.request<{ properties: OwnerProperty[] }>('GET', '/v1/owner/properties');
    return out.properties ?? [];
  }

  ownerProperty(id: string): Promise<OwnerProperty> {
    return this.request('GET', `/v1/owner/properties/${id}`);
  }

  /** month as YYYY-MM; omitted means the current one. */
  ownerStatement(propertyId: string, month?: string): Promise<OwnerStatement> {
    const q = month ? `?month=${month}` : '';
    return this.request('GET', `/v1/owner/properties/${propertyId}/statement${q}`);
  }

  async ownerActivity(): Promise<ActivityEntry[]> {
    const out = await this.request<{ entries: ActivityEntry[] }>('GET', '/v1/owner/activity');
    return out.entries ?? [];
  }

  async ownerDocuments(propertyId: string): Promise<OwnerDocument[]> {
    const out = await this.request<{ documents: OwnerDocument[] }>(
      'GET', `/v1/owner/properties/${propertyId}/documents`);
    return out.documents ?? [];
  }

  ownerUploadUrl(propertyId: string, filename: string, contentType: string): Promise<UploadGrant> {
    return this.request('POST', `/v1/owner/properties/${propertyId}/documents/upload-url`, {
      filename, content_type: contentType,
    });
  }

  /* --------------------------------------------------------- ops surface */

  async opsProperties(): Promise<OpsProperty[]> {
    const out = await this.request<{ properties: OpsProperty[] }>('GET', '/v1/ops/properties');
    return out.properties ?? [];
  }

  async opsArrears(): Promise<OpsArrear[]> {
    const out = await this.request<{ arrears: OpsArrear[] }>('GET', '/v1/ops/arrears');
    return out.arrears ?? [];
  }

  opsToday(): Promise<OpsToday> {
    return this.request('GET', '/v1/ops/today');
  }

  async opsActivity(): Promise<ActivityEntry[]> {
    const out = await this.request<{ entries: ActivityEntry[] }>('GET', '/v1/ops/activity');
    return out.entries ?? [];
  }
}

/**
 * apiFromEnv builds the client the environment names, or null — and null is a
 * mode, not a failure: the demonstration data renders instead, clearly
 * labelled by the caller. EXPO_PUBLIC_* is inlined at build time by Expo.
 */
export function apiFromEnv(getToken?: () => Promise<string | null>): DwellmApi | null {
  const base = process.env.EXPO_PUBLIC_API_URL;
  if (!base) return null;
  return new DwellmApi({ baseUrl: base, getToken });
}
