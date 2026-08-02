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

/** One public listing card (GET /v1/public/listings), the true cost stated. */
export type PublicListing = {
  id: string;
  headline: string;
  locality: string;
  city: string;
  state_code: string;
  bedrooms?: number;
  carpet_area_sqft?: number;
  available_from?: string;
  rent_minor: number;
  maintenance_minor: number;
  parking_minor: number;
  other_monthly_minor: number;
  deposit_minor: number;
  one_time_minor: number;
  total_monthly_minor: number;
  total_one_time_minor: number;
  currency: string;
  published_at: string;
  /** Short-lived signed photo URLs, cover first — present on detail only (#136). */
  photos?: string[];
};

export type PublicSearch = {
  city?: string;
  locality?: string;
  maxRentMinor?: number;
  bedrooms?: number;
  availableBy?: string; // YYYY-MM-DD
  after?: string; // cursor from the previous page
  limit?: number;
};

/** One bookable viewing time (GET /v1/public/listings/{id}/slots). */
export type PublicSlot = {
  id: string;
  starts_at: string;
  duration_mins: number;
  remaining: number;
};

/** A confirmed booking — the meeting point arrives only here. */
export type BookedInspection = {
  id: string;
  state: string;
  starts_at: string;
  meeting_point?: string;
};

/** One of the prospect's own enquiries (GET /v1/public/enquiries). */
export type ProspectEnquiry = {
  id: string;
  listing_id: string;
  headline?: string;
  kind: string;
  state: string;
  message?: string;
  scheduled_for?: string;
  created_at: string;
};

export type ShortlistItem = {
  listing_id: string;
  headline: string;
  locality: string;
  city: string;
  rent_minor: number;
  state: string;
};

/** One residency as the tenant sees it (GET /v1/resident/tenancies). */
export type ResidentTenancy = {
  lease_id: string;
  state: string;
  live: boolean;
  organisation: string;
  property: string;
  unit: string;
  locality: string;
  city: string;
  start_on: string;
  end_on?: string;
  ended_on?: string;
  notice_days: number;
  lock_in_until?: string;
  rent_amount_minor: number;
  due_day: number;
  currency: string;
  dues?: ResidentDues;
};

export type ResidentDues = {
  due_amount_minor: number;
  rent_amount_minor: number;
  late_fee_amount_minor: number;
  adjustment_amount_minor: number;
  paid_amount_minor: number;
  advance_amount_minor: number;
  deposit_amount_minor: number;
  as_of: string;
};

export type ResidentHistoryEntry = {
  entry_id: string;
  kind: string;
  occurred_on: string;
  amount_minor: number;
  memo?: string;
  reversed?: boolean;
};

export type ResidentPayment = {
  payment_id: string;
  amount_minor: number;
  currency: string;
  method: string;
  status: string;
  created_at: string;
  received_at?: string;
  failure_code?: string;
  receipt_number?: string;
};

/** What starting a payment answers with (POST .../payments). */
export type SavedSearchRow = {
  id: string;
  city: string;
  locality?: string;
  max_rent_minor?: number;
  bedrooms?: number;
  alerts_enabled: boolean;
  new_matches: number;
  created_at: string;
};

export type RentalApplication = {
  id: string;
  listing_id: string;
  state: string; // submitted | under_review | accepted | declined | withdrawn
  move_in: string; // YYYY-MM-DD
  term_months: number;
  created_at: string;
  headline?: string;
  offer_minor?: number;
  note?: string;
  decline_reason?: string;
  lease_id?: string;
};

export type FlaggedListing = {
  id: string;
  headline: string;
  state: string;
  report_count: number;
  reported_at?: string;
  suspended_reason?: string;
};

export type ReportedMedia = {
  id: string;
  listing_id: string;
  headline: string;
  reported_at: string;
  content_type: string;
  url?: string;
};

export type PaymentStarted = {
  payment_id: string;
  status: string;
  pay_url?: string;
  pay_token?: string;
  provider_order_id?: string;
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

  private async request<T>(method: string, path: string, body?: unknown,
    extra?: Record<string, string>): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json', ...extra };
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

  /* ------------------------------------------ inventory registration (#32) */

  /** Register a building — the row every listing, lease and ledger entry
   * points back to. Requires can_administer on the organisation. */
  registerProperty(p: {
    code: string; name: string; kind: string;
    address_line1: string; address_line2?: string;
    locality: string; city: string; district?: string;
    state_code: string; pin: string;
  }): Promise<{ id: string; code: string }> {
    return this.request('POST', '/v1/properties', p);
  }

  /** Add a lettable unit, or the parking that attaches to one. */
  addUnit(propertyId: string, u: {
    code: string; unit_kind: string; floor?: number;
    carpet_area_sqft?: number; parent_unit_id?: string;
  }): Promise<{ id: string; code: string }> {
    return this.request('POST', `/v1/properties/${propertyId}/units`, u);
  }

  /* ---------------------------------------------- public discovery (Find) */
  // Anonymous by design (ADR-0019): browsing needs no account, making contact
  // needs a verified phone. The prospect token is a browsing key, sent as its
  // own header so a future sign-in can coexist with it.

  async publicSearch(q: PublicSearch): Promise<{ listings: PublicListing[]; nextAfter?: string }> {
    const p = new URLSearchParams();
    if (q.city) p.set('city', q.city);
    if (q.locality) p.set('locality', q.locality);
    if (q.maxRentMinor) p.set('max_rent_minor', String(q.maxRentMinor));
    if (q.bedrooms) p.set('bedrooms', String(q.bedrooms));
    if (q.availableBy) p.set('available_by', q.availableBy);
    if (q.after) p.set('after', q.after);
    if (q.limit) p.set('limit', String(q.limit));
    const qs = p.toString();
    const out = await this.request<{ listings: PublicListing[]; next_after?: string }>(
      'GET', `/v1/public/listings${qs ? `?${qs}` : ''}`);
    return { listings: out.listings ?? [], nextAfter: out.next_after };
  }

  publicListing(id: string): Promise<PublicListing> {
    return this.request('GET', `/v1/public/listings/${id}`);
  }

  async prospectStart(): Promise<string> {
    const out = await this.request<{ token: string }>('POST', '/v1/public/prospects', {});
    return out.token;
  }

  prospectVerify(token: string, phone: string): Promise<void> {
    return this.request('POST', '/v1/public/prospects/verify', { phone },
      { 'X-Dwellm8-Prospect': token });
  }

  prospectConfirm(token: string, phone: string, code: string): Promise<void> {
    return this.request('POST', '/v1/public/prospects/confirm', { phone, code },
      { 'X-Dwellm8-Prospect': token });
  }

  async shortlist(token: string): Promise<ShortlistItem[]> {
    const out = await this.request<{ shortlist: ShortlistItem[] }>(
      'GET', '/v1/public/shortlist', undefined, { 'X-Dwellm8-Prospect': token });
    return out.shortlist ?? [];
  }

  shortlistAdd(token: string, listingId: string): Promise<void> {
    return this.request('POST', '/v1/public/shortlist', { listing_id: listingId },
      { 'X-Dwellm8-Prospect': token });
  }

  shortlistRemove(token: string, listingId: string): Promise<void> {
    return this.request('DELETE', `/v1/public/shortlist/${listingId}`, undefined,
      { 'X-Dwellm8-Prospect': token });
  }

  enquire(token: string, listingId: string, message: string): Promise<{ id: string; state: string }> {
    return this.request('POST', '/v1/public/enquiries',
      { listing_id: listingId, kind: 'enquiry', message },
      { 'X-Dwellm8-Prospect': token });
  }

  async myEnquiries(token: string): Promise<ProspectEnquiry[]> {
    const out = await this.request<{ enquiries: ProspectEnquiry[] }>(
      'GET', '/v1/public/enquiries', undefined, { 'X-Dwellm8-Prospect': token });
    return out.enquiries ?? [];
  }

  async listingSlots(listingId: string): Promise<PublicSlot[]> {
    const out = await this.request<{ slots: PublicSlot[] }>(
      'GET', `/v1/public/listings/${listingId}/slots`);
    return out.slots ?? [];
  }

  /** A full slot answers 409 with `alternatives` — surface them, never silence. */
  bookInspection(token: string, listingId: string, slotId: string): Promise<BookedInspection> {
    return this.request('POST', '/v1/public/inspections',
      { listing_id: listingId, slot_id: slotId },
      { 'X-Dwellm8-Prospect': token });
  }

  cancelInspection(token: string, enquiryId: string): Promise<void> {
    return this.request('POST', `/v1/public/inspections/${enquiryId}/cancel`, {},
      { 'X-Dwellm8-Prospect': token });
  }

  /* ------------------------------------------------ saved searches (#144) */

  /** The same criteria saved twice is the same search. */
  async saveSearch(token: string, q: {
    city: string; locality?: string; maxRentMinor?: number; bedrooms?: number;
  }): Promise<string> {
    const out = await this.request<{ id: string }>('POST', '/v1/public/searches', {
      city: q.city, locality: q.locality,
      max_rent_minor: q.maxRentMinor, bedrooms: q.bedrooms,
    }, { 'X-Dwellm8-Prospect': token });
    return out.id;
  }

  async mySearches(token: string): Promise<SavedSearchRow[]> {
    const out = await this.request<{ searches: SavedSearchRow[] }>(
      'GET', '/v1/public/searches', undefined, { 'X-Dwellm8-Prospect': token });
    return out.searches ?? [];
  }

  /** Advances the no-resend watermark: current matches stop being news. */
  searchSeen(token: string, id: string): Promise<void> {
    return this.request('POST', `/v1/public/searches/${id}/seen`, {},
      { 'X-Dwellm8-Prospect': token });
  }

  /** Opting out retains the search; only the alerting stops. */
  setSearchAlerts(token: string, id: string, enabled: boolean): Promise<void> {
    return this.request('POST', `/v1/public/searches/${id}/alerts`, { enabled },
      { 'X-Dwellm8-Prospect': token });
  }

  deleteSearch(token: string, id: string): Promise<void> {
    return this.request('DELETE', `/v1/public/searches/${id}`, undefined,
      { 'X-Dwellm8-Prospect': token });
  }

  /* ---------------------------------------------------- push tokens (#126) */

  registerPushToken(token: string, expoToken: string, platform: 'ios' | 'android'): Promise<void> {
    return this.request('POST', '/v1/public/push/token',
      { token: expoToken, platform }, { 'X-Dwellm8-Prospect': token });
  }

  dropPushToken(token: string, expoToken: string): Promise<void> {
    return this.request('POST', '/v1/public/push/token/drop',
      { token: expoToken }, { 'X-Dwellm8-Prospect': token });
  }

  /* -------------------------------------------- rental applications (#142) */
  // The formal step between enquiry and lease. Applying needs the same
  // verified prospect token the enquiry did; the owner side needs a signed-in
  // operator.

  /** moveIn as YYYY-MM-DD. Applying twice for one listing is the same
   * application, returned again. */
  applyToListing(token: string, listingId: string, a: {
    moveIn: string; termMonths?: number; offerMinor?: number; note?: string;
  }): Promise<RentalApplication> {
    return this.request('POST', `/v1/public/listings/${listingId}/applications`, {
      move_in: a.moveIn, term_months: a.termMonths,
      offer_minor: a.offerMinor, note: a.note,
    }, { 'X-Dwellm8-Prospect': token });
  }

  async myApplications(token: string): Promise<RentalApplication[]> {
    const out = await this.request<{ applications: RentalApplication[] }>(
      'GET', '/v1/public/applications', undefined, { 'X-Dwellm8-Prospect': token });
    return out.applications ?? [];
  }

  withdrawApplication(token: string, id: string): Promise<void> {
    return this.request('POST', `/v1/public/applications/${id}/withdraw`, {},
      { 'X-Dwellm8-Prospect': token });
  }

  /** Owner's review queue; state filters (submitted, under_review, …). */
  async applicationsQueue(state?: string): Promise<RentalApplication[]> {
    const q = state ? `?state=${state}` : '';
    const out = await this.request<{ applications: RentalApplication[] }>(
      'GET', `/v1/applications${q}`);
    return out.applications ?? [];
  }

  reviewApplication(id: string): Promise<void> {
    return this.request('POST', `/v1/applications/${id}/review`, {});
  }

  /** Accepting drafts the tenancy with the application's terms carried over
   * and pauses the listing; rentMinor overrides a negotiated number. */
  acceptApplication(id: string, tenant: {
    name: string; phone: string; email?: string; rentMinor?: number;
  }): Promise<{ id: string; lease_id: string; state: string }> {
    return this.request('POST', `/v1/applications/${id}/accept`, {
      tenant_name: tenant.name, tenant_phone: tenant.phone,
      tenant_email: tenant.email, rent_minor: tenant.rentMinor,
    });
  }

  declineApplication(id: string, reason: string): Promise<void> {
    return this.request('POST', `/v1/applications/${id}/decline`, { reason });
  }

  /* ------------------------------------------------- moderation (#143) */

  /** Anyone may report a live listing; the reviewer judges. */
  reportListing(id: string, reason: 'fraud' | 'discrimination' | 'incorrect' | 'other'): Promise<void> {
    return this.request('POST', `/v1/public/listings/${id}/report`, { reason });
  }

  async moderationListings(): Promise<FlaggedListing[]> {
    const out = await this.request<{ queue: FlaggedListing[] }>('GET', '/v1/moderation/listings');
    return out.queue ?? [];
  }

  suspendListing(id: string, reason: string): Promise<void> {
    return this.request('POST', `/v1/listings/${id}/suspend`, { reason });
  }

  /** The middle judgement: live, but the lister is on notice. */
  warnListing(id: string, reason: string): Promise<void> {
    return this.request('POST', `/v1/listings/${id}/warn`, { reason });
  }

  reinstateListing(id: string): Promise<void> {
    return this.request('POST', `/v1/listings/${id}/reinstate`, {});
  }

  dismissListingReports(id: string): Promise<void> {
    return this.request('POST', `/v1/listings/${id}/reports/dismiss`, {});
  }

  async moderationMedia(): Promise<ReportedMedia[]> {
    const out = await this.request<{ queue: ReportedMedia[] }>('GET', '/v1/moderation/media');
    return out.queue ?? [];
  }

  takedownMedia(listingId: string, mediaId: string, reason: string): Promise<void> {
    return this.request('POST', `/v1/listings/${listingId}/media/${mediaId}/takedown`, { reason });
  }

  clearMediaReport(listingId: string, mediaId: string): Promise<void> {
    return this.request('POST', `/v1/listings/${listingId}/media/${mediaId}/clear`, {});
  }

  /* --------------------------------------------- resident surface (Live) */

  async residentTenancies(): Promise<ResidentTenancy[]> {
    const out = await this.request<{ tenancies: ResidentTenancy[] }>(
      'GET', '/v1/resident/tenancies');
    return out.tenancies ?? [];
  }

  residentTenancy(leaseId: string): Promise<ResidentTenancy> {
    return this.request('GET', `/v1/resident/tenancies/${leaseId}`);
  }

  async residentHistory(leaseId: string): Promise<{ charges: ResidentHistoryEntry[]; payments: ResidentPayment[] }> {
    const out = await this.request<{ charges?: ResidentHistoryEntry[]; payments?: ResidentPayment[] }>(
      'GET', `/v1/resident/tenancies/${leaseId}/history`);
    return { charges: out.charges ?? [], payments: out.payments ?? [] };
  }

  residentPay(leaseId: string, amountMinor: number, method: string,
    idempotencyKey: string): Promise<PaymentStarted> {
    return this.request('POST', `/v1/resident/tenancies/${leaseId}/payments`,
      { amount_minor: amountMinor, method, idempotency_key: idempotencyKey });
  }

  async residentActivity(leaseId: string): Promise<ActivityEntry[]> {
    const out = await this.request<{ entries: ActivityEntry[] }>(
      'GET', `/v1/resident/tenancies/${leaseId}/activity`);
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
