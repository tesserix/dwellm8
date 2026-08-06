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
/** One maintenance ticket as the manager sees it (GET /v1/ops/tickets). */
export type OpsTicket = {
  ticket_id: string;
  lease_id: string;
  unit?: string;
  property?: string;
  category: string;
  title: string;
  body?: string;
  status: string;
  liability?: string;
  liability_reason?: string;
  slot?: string;
  vendor?: string;
  cost_minor?: number;
  raised_at: string;
  timeline?: { at: string; actor: string; body: string }[];
};

/** One tenancy conversation in the manager's inbox (GET /v1/ops/threads). */
export type OpsThread = {
  lease_id: string;
  unit: string;
  property: string;
  last_body: string;
  last_sender: string;
  last_at: string;
  messages: number;
};

export type OpsMessage = {
  message_id: string;
  sender: string;
  body: string;
  sent_at: string;
};

export type OpsChecklistTask = {
  id: string; step_code: string; title: string; position: number; blocking: boolean;
  owner_role: string; assignee_party_id?: string; due_on: string; state: string;
  depends_on?: string[];
};

export type OpsChecklist = {
  id: string; process: string; property_id: string; unit_id?: string; lease_id?: string;
  anchor_on: string; state: string;
  blocking_outstanding?: OpsChecklistTask[]; tasks: OpsChecklistTask[];
};

/** One row of GET /v1/checklists — progress, the late ones first. */
export type OpsChecklistProgress = {
  id: string; process: string; property_id: string; unit_id?: string; lease_id?: string;
  state: string; tasks: number; settled: number; outstanding: number;
  blocking_outstanding: number; next_due_on?: string; days_overdue?: number;
};

export type OpsAutomation = {
  key: string; name: string; purpose: string; trigger: string; on?: string;
  enabled: boolean; enabled_by_default: boolean; approval_ceiling_minor: number;
  params: { name: string; purpose: string; unit: string; value: number; default: number; min: number; max: number }[];
  overridden?: string[];
  runs: number; acted: number; awaiting_approval: number; failed: number; last_run_at?: string;
};

export type OpsApproval = {
  id: string; automation: string; subject_kind: string; subject_id: string;
  action: string; amount_minor: number; ceiling_minor: number; requested_at: string;
};

export type OpsEnquiry = {
  id: string; listing_id: string; headline?: string; kind: string; state: string;
  message?: string; contact_masked?: string; scheduled_for?: string; created_at: string;
};

export type OpsInspection = {
  id?: string; starts_at: string; duration_mins?: number; remaining?: number;
  assigned_to?: string; meeting_point?: string; state?: string;
  listing_id?: string; outcome?: string; note?: string;
};

/** One owner's books this firm manages (GET /v1/ops/portfolios). */
export type OpsPortfolio = {
  grant_id: string;
  owner_org_id: string;
  owner_name: string;
  permissions: string[];
  since: string;
  property_count: number;
  /** The manager's own property — held rather than managed for somebody (#268). */
  self_managed?: boolean;
};

/** One gate pass on the org worklist (GET /v1/ops/passes). */
export type OpsPass = {
  pass_id: string;
  lease_id: string;
  unit?: string;
  property?: string;
  name: string;
  kind: string;
  code: string;
  state: string;
  valid_from: string;
  valid_to?: string;
};

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
  notice_served_on?: string;
  notice_move_out_on?: string;
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

/** One maintenance request as the tenant sees it (GET .../tickets). */
export type ResidentTicket = {
  ticket_id: string;
  category: string;
  title: string;
  body?: string;
  status: string;
  liability?: string;
  liability_reason?: string;
  slot?: string;
  vendor?: string;
  cost_minor?: number;
  raised_at: string;
  timeline?: ResidentTicketEvent[];
};

export type ResidentTicketEvent = {
  at: string;
  actor: string;
  body: string;
};

/** One line of the tenancy conversation (GET .../messages). */
export type ResidentMessage = {
  message_id: string;
  sender: string;
  mine: boolean;
  body: string;
  sent_at: string;
};

/** One expected visitor (GET .../passes). */
export type ResidentPass = {
  pass_id: string;
  name: string;
  kind: string;
  code: string;
  state: string;
  valid_from: string;
  valid_to?: string;
  created_at: string;
};

/** The signed-in renter (GET /v1/resident/me). */
export type ResidentMe = {
  party_id: string;
  phone?: string;
  email?: string;
  display_name?: string;
  tenancies: { lease_id: string; organisation: string; state: string }[];
};

/** Any verified sign-in's own profile (GET /v1/me — the Own app's "me"). */
export type Me = {
  party_id: string;
  phone?: string;
  email?: string;
  display_name?: string;
};

/** Whether the address a manager signed up with reaches them (#282). */
export type EmailVerification = {
  email?: string;
  verified: boolean;
  /** How many seconds the resend button stays dead. */
  resend_after_seconds?: number;
};

/** One line of the statutory checklist. */
export type FirmRequirement = {
  kind: string;
  /** of_the_firm for a body corporate's own PAN, of_the_person for a proprietor's. */
  subject: string;
  label: string;
  why: string;
  expires?: boolean;
};

export type FirmAuthority = {
  id: string;
  authority: string;
  state_code: string;
  number: string;
  valid_from: string;
  valid_to: string;
};

/** The firm's own registration and what is still missing from it (#282). */
export type FirmRegistration = {
  legal_name: string;
  trade_name?: string;
  constitution: string;
  pan_masked?: string;
  tan?: string;
  gstin?: string;
  registrar_id?: string;
  address_line1?: string;
  address_line2?: string;
  locality?: string;
  city?: string;
  state_code?: string;
  pin_code?: string;
  contact_email?: string;
  contact_phone?: string;
  state: string;
  authorities: FirmAuthority[];
  required: FirmRequirement[];
  outstanding: FirmRequirement[];
  /** Which fields this constitution must fill in — registrar_id only for a body corporate. */
  fields: string[];
  may_manage: boolean;
};

function fillRegistration(r: FirmRegistration): FirmRegistration {
  return {
    ...r,
    authorities: r.authorities ?? [], required: r.required ?? [],
    outstanding: r.outstanding ?? [], fields: r.fields ?? [],
  };
}

/** What a first sign-in produced (POST /v1/onboarding). */
export type Onboarded = {
  organisation_id: string;
  party_id: string;
  role: string;
  created: boolean;
};

/** What manager-led owner onboarding produced (POST /v1/ops/onboardings). */
export type OwnerOnboarded = {
  owner_org_id: string;
  owner_party_id: string;
  grant_id: string;
  created_organisation: boolean;
  property_id?: string;
  unit_ids?: string[];
  lease_id?: string;
  lease_state?: string;
  lease_note?: string;
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

/** Who else moves in: a co-applicant, a dependant, or a guarantor. */
export type ApplicantPerson = {
  id?: string;
  role: 'co_applicant' | 'dependant' | 'guarantor';
  full_name: string;
  relationship?: string;
  date_of_birth?: string; // YYYY-MM-DD
  phone?: string; // E.164, ADR-0029
  email?: string;
};

/** One place the applicant lived, by month — nobody remembers the day. */
export type ApplicantAddress = {
  id?: string;
  kind: 'rented' | 'owned' | 'family' | 'employer_provided' | 'hostel';
  line1: string;
  locality?: string;
  city: string;
  state_code?: string;
  pin?: string;
  country?: string;
  from: string; // YYYY-MM
  to?: string; // absent is the present address
  landlord_name?: string;
  landlord_phone?: string;
};

/** The five years, with the holes named rather than left to be spotted. */
export type AddressHistory = {
  addresses: ApplicantAddress[];
  gaps: { from: string; to: string }[];
  complete: boolean;
};

export type ApplicantPack = {
  id: string;
  application_id: string;
  full_name: string;
  date_of_birth?: string;
  nationality: string;
  tax_residency: string; // resident | non_resident
  occupants: number;
  pets: boolean;
  pets_note?: string;
  state: string; // draft | submitted
  submitted_at?: string;
  corrects?: string;
  people: ApplicantPerson[];
  address_history_complete?: boolean;
  address_history_gaps?: number;
};

/** The manager's own account with an aggregator (#269). The number is never
 * returned — only the mask the provider left us with. */
export type MerchantAccount = {
  provider: string;
  business_name: string;
  state: 'unconnected' | 'submitted' | 'verified' | 'suspended';
  reason?: string;
  settlement_masked: string;
  settlement_ifsc?: string;
  settlement_currency: string;
  may_collect: boolean;
  /** The sentence the screen shows instead of a state name. */
  next_action: string;
};

export type Settlement = {
  id: string;
  payment_id: string;
  lease_id?: string;
  currency: string;
  gross_amount_minor: number;
  platform_amount_minor: number;
  management_amount_minor: number;
  tds_amount_minor: number;
  owner_amount_minor: number;
  state: 'pending' | 'instructed' | 'settled' | 'failed';
  provider?: string;
  transfer_ref?: string;
  expected_on?: string;
  settled_on?: string;
  reason?: string;
  /** Money the schedule promised by a date that has passed. */
  overdue: boolean;
};

export type ConnectMerchant = {
  provider: string;
  business_name: string;
  business_type: 'individual' | 'proprietorship' | 'partnership' | 'llp' | 'company' | 'trust';
  country?: string;
  email?: string;
  phone?: string;
  pan: string;
  gstin?: string;
  account_number: string;
  account_holder: string;
  ifsc: string;
  currency?: string;
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
  /** Seconds until the same call is worth making again, when the server said
   * so — a screen counting down should not have to read English to do it. */
  retryAfterSeconds?: number;
  constructor(status: number, message: string, retryAfterSeconds?: number) {
    super(message);
    this.status = status;
    this.retryAfterSeconds = retryAfterSeconds;
  }
}

/** The mandate every Ops request acts under (ADR-0005), app-wide: null is
 * the firm's own books; a grant id opens the grantor's rows and nothing
 * else. Module-level because apiFromEnv() builds a client per hook. */
let actingGrant: string | null = null;

export function setActingGrant(grantId: string | null): void {
  actingGrant = grantId;
}

export function getActingGrant(): string | null {
  return actingGrant;
}

/** Who is signed in, app-wide. Module-level for the same reason the grant is:
 * apiFromEnv() builds a client per hook, and a screen must not have to know
 * where the token comes from to be authenticated. */
let tokenSource: (() => Promise<string | null>) | null = null;

export function setTokenSource(fn: (() => Promise<string | null>) | null): void {
  tokenSource = fn;
}

export class DwellmApi {
  private cfg: ApiConfig;
  constructor(cfg: ApiConfig) {
    this.cfg = { ...cfg, baseUrl: cfg.baseUrl.replace(/\/+$/, '') };
  }

  private async request<T>(method: string, path: string, body?: unknown,
    extra?: Record<string, string>): Promise<T> {
    const headers: Record<string, string> = { Accept: 'application/json', ...extra };
    if (actingGrant && path.startsWith('/v1/ops/')) headers['X-Dwellm8-Grant'] = actingGrant;
    if (body !== undefined) headers['Content-Type'] = 'application/json';
    const getToken = this.cfg.getToken ?? tokenSource;
    const token = getToken ? await getToken() : null;
    if (token) headers.Authorization = `Bearer ${token}`;

    const res = await fetch(this.cfg.baseUrl + path, {
      method,
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const text = await res.text();
    if (!res.ok) {
      let message = `request failed (${res.status})`;
      const header = Number(res.headers?.get?.('Retry-After'));
      let retryAfter = Number.isFinite(header) && header > 0 ? header : undefined;
      try {
        const parsed = JSON.parse(text) as { error?: string; resend_after_seconds?: number };
        if (parsed.error) message = parsed.error;
        if (retryAfter === undefined && parsed.resend_after_seconds) {
          retryAfter = parsed.resend_after_seconds;
        }
      } catch {
        /* the body was not JSON; the status alone will have to explain */
      }
      throw new ApiError(res.status, message, retryAfter);
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

  async opsTickets(settled = false): Promise<OpsTicket[]> {
    const out = await this.request<{ tickets?: OpsTicket[] }>(
      'GET', `/v1/ops/tickets${settled ? '?settled=true' : ''}`);
    return out.tickets ?? [];
  }

  opsTicket(id: string): Promise<OpsTicket> {
    return this.request('GET', `/v1/ops/tickets/${id}`);
  }

  /** One manager action on a ticket: acknowledge, schedule, assess, start,
   * resolve or cancel — extra fields per action. */
  opsAdvanceTicket(id: string, req: {
    action: string; slot?: string; vendor?: string;
    liability?: string; liability_reason?: string; cost_minor?: number; note?: string;
  }): Promise<OpsTicket> {
    return this.request('POST', `/v1/ops/tickets/${id}/advance`, {
      action: req.action, slot: req.slot ?? '', vendor: req.vendor ?? '',
      liability: req.liability ?? '', liability_reason: req.liability_reason ?? '',
      cost_minor: req.cost_minor, note: req.note ?? '',
    });
  }

  async opsThreads(): Promise<OpsThread[]> {
    const out = await this.request<{ threads?: OpsThread[] }>('GET', '/v1/ops/threads');
    return out.threads ?? [];
  }

  async opsMessages(leaseId: string): Promise<OpsMessage[]> {
    const out = await this.request<{ messages?: OpsMessage[] }>(
      'GET', `/v1/ops/tenancies/${leaseId}/messages`);
    return out.messages ?? [];
  }

  opsSendMessage(leaseId: string, body: string): Promise<OpsMessage> {
    return this.request('POST', `/v1/ops/tenancies/${leaseId}/messages`, { body });
  }

  async opsPasses(): Promise<OpsPass[]> {
    const out = await this.request<{ passes?: OpsPass[] }>('GET', '/v1/ops/passes');
    return out.passes ?? [];
  }

  opsSetPassState(id: string, state: 'arrived' | 'inside' | 'left' | 'denied'): Promise<OpsPass> {
    return this.request('POST', `/v1/ops/passes/${id}/state`, { state });
  }

  async opsPortfolios(): Promise<OpsPortfolio[]> {
    const out = await this.request<{ portfolios?: OpsPortfolio[] }>('GET', '/v1/ops/portfolios');
    return out.portfolios ?? [];
  }

  async opsChecklists(state?: string): Promise<OpsChecklistProgress[]> {
    const out = await this.request<{ checklists?: OpsChecklistProgress[] }>(
      'GET', `/v1/checklists${state ? `?state=${state}` : ''}`);
    return out.checklists ?? [];
  }

  opsChecklist(id: string): Promise<OpsChecklist> {
    return this.request('GET', `/v1/checklists/${id}`);
  }

  opsChecklistComplete(id: string, step: string): Promise<OpsChecklist> {
    return this.request('POST', `/v1/checklists/${id}/steps/${step}/complete`, {});
  }

  opsChecklistSkip(id: string, step: string, reason: string): Promise<OpsChecklist> {
    return this.request('POST', `/v1/checklists/${id}/steps/${step}/skip`, { reason });
  }

  opsChecklistFinish(id: string): Promise<OpsChecklist> {
    return this.request('POST', `/v1/checklists/${id}/finish`, {});
  }

  async opsAutomations(): Promise<OpsAutomation[]> {
    const out = await this.request<{ automations?: OpsAutomation[] }>('GET', '/v1/automations');
    return out.automations ?? [];
  }

  opsSetAutomation(key: string, patch: { enabled?: boolean; params?: Record<string, number>; approval_ceiling_minor?: number }): Promise<void> {
    return this.request('PUT', `/v1/automations/${key}`, {
      enabled: patch.enabled ?? null, params: patch.params ?? null,
      approval_ceiling_minor: patch.approval_ceiling_minor ?? null,
    });
  }

  async opsApprovals(): Promise<OpsApproval[]> {
    const out = await this.request<{ approvals?: OpsApproval[] }>('GET', '/v1/automations/approvals');
    return out.approvals ?? [];
  }

  opsDecideApproval(id: string, decision: 'approve' | 'decline', reason?: string): Promise<void> {
    return this.request('POST', `/v1/automations/approvals/${id}`, { decision, reason: reason ?? '' });
  }

  async opsEnquiries(): Promise<OpsEnquiry[]> {
    const out = await this.request<{ enquiries?: OpsEnquiry[] }>('GET', '/v1/enquiries');
    return out.enquiries ?? [];
  }

  /* ------------------------------- the applicant pack (#258, #259) */
  // Reached under the mandate, so the firm managing the property collects the
  // pack rather than the owner. Gaps in the five years come back named.

  async opsApplications(state?: string): Promise<RentalApplication[]> {
    const out = await this.request<{ applications?: RentalApplication[] }>(
      'GET', `/v1/ops/applications${state ? `?state=${state}` : ''}`);
    return out.applications ?? [];
  }

  /** The manager's own merchant accounts, #269. */
  async opsMerchants(): Promise<MerchantAccount[]> {
    const out = await this.request<{ accounts?: MerchantAccount[] }>('GET', '/v1/ops/merchant');
    return out.accounts ?? [];
  }

  opsConnectMerchant(a: ConnectMerchant): Promise<MerchantAccount> {
    return this.request('POST', '/v1/ops/merchant', a);
  }

  opsRefreshMerchant(provider: string): Promise<MerchantAccount> {
    return this.request('POST', `/v1/ops/merchant/${provider}/refresh`);
  }

  async opsSettlements(): Promise<Settlement[]> {
    const out = await this.request<{ settlements?: Settlement[] }>('GET', '/v1/ops/settlements');
    return out.settlements ?? [];
  }

  opsReleaseSettlement(id: string, beneficiaryRef: string): Promise<Settlement> {
    return this.request('POST', `/v1/ops/settlements/${id}/release`, { beneficiary_ref: beneficiaryRef });
  }

  opsApplicantPack(applicationId: string): Promise<ApplicantPack> {
    return this.request('GET', `/v1/ops/applications/${applicationId}/profile`);
  }

  opsSaveApplicantPack(applicationId: string, p: {
    full_name: string; date_of_birth?: string; nationality?: string;
    tax_residency?: 'resident' | 'non_resident'; occupants?: number;
    pets?: boolean; pets_note?: string; people?: ApplicantPerson[];
  }): Promise<ApplicantPack> {
    return this.request('PUT', `/v1/ops/applications/${applicationId}/profile`, p);
  }

  opsSaveHousehold(applicationId: string, people: ApplicantPerson[]): Promise<ApplicantPack> {
    return this.request('PUT', `/v1/ops/applications/${applicationId}/profile/people`, { people });
  }

  opsAddressHistory(applicationId: string): Promise<AddressHistory> {
    return this.request('GET', `/v1/ops/applications/${applicationId}/addresses`);
  }

  opsSaveAddressHistory(applicationId: string, addresses: ApplicantAddress[]): Promise<AddressHistory> {
    return this.request('PUT', `/v1/ops/applications/${applicationId}/addresses`, { addresses });
  }

  opsSubmitApplicantPack(applicationId: string): Promise<{ id: string; state: string }> {
    return this.request('POST', `/v1/ops/applications/${applicationId}/profile/submit`, {});
  }

  async opsInspections(on?: string): Promise<OpsInspection[]> {
    const out = await this.request<{ inspections?: OpsInspection[] }>(
      'GET', `/v1/inspections${on ? `?on=${on}` : ''}`);
    return out.inspections ?? [];
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

  async residentTickets(leaseId: string): Promise<ResidentTicket[]> {
    const out = await this.request<{ tickets?: ResidentTicket[] }>(
      'GET', `/v1/resident/tenancies/${leaseId}/tickets`);
    return out.tickets ?? [];
  }

  residentTicket(leaseId: string, ticketId: string): Promise<ResidentTicket> {
    return this.request('GET', `/v1/resident/tenancies/${leaseId}/tickets/${ticketId}`);
  }

  residentRaiseTicket(leaseId: string,
    req: { category: string; title: string; body?: string }): Promise<ResidentTicket> {
    return this.request('POST', `/v1/resident/tenancies/${leaseId}/tickets`,
      { category: req.category, title: req.title, body: req.body ?? '' });
  }

  residentMe(): Promise<ResidentMe> {
    return this.request('GET', '/v1/resident/me');
  }

  /** Fill in self-served PI (#240). The verified phone never moves. */
  residentUpdateMe(patch: { display_name?: string; email?: string }): Promise<ResidentMe> {
    return this.request('PATCH', '/v1/resident/me',
      { display_name: patch.display_name ?? '', email: patch.email ?? '' });
  }

  /** The verified sign-in's own profile — the Own app's "me" (#240). Null is
   * the first sign-in: verified, but with no account behind it yet. */
  async me(): Promise<Me | null> {
    try {
      return await this.request<Me>('GET', '/v1/me');
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) return null;
      throw err;
    }
  }

  /** Whether the address this sign-in used has been proved (#282). */
  emailVerification(): Promise<EmailVerification> {
    return this.request('GET', '/v1/verification/email');
  }

  /** Mail a fresh code. 429 carries resend_after_seconds. */
  sendEmailCode(): Promise<EmailVerification> {
    return this.request('POST', '/v1/verification/email', {});
  }

  confirmEmailCode(code: string): Promise<EmailVerification> {
    return this.request('POST', '/v1/verification/email/confirm', { code });
  }

  /** What the firm must hold to manage somebody else's property (#282). */
  async opsRegistration(): Promise<FirmRegistration> {
    return fillRegistration(await this.request<FirmRegistration>('GET', '/v1/ops/registration'));
  }

  /** The PAN travels whole and is masked server-side; nothing keeps it here. */
  async opsSaveRegistration(req: {
    legal_name: string; trade_name?: string; constitution: string;
    pan?: string; tan?: string; gstin?: string; registrar_id?: string;
    address_line1?: string; address_line2?: string; locality?: string; city?: string;
    state_code?: string; pin_code?: string; contact_email?: string; contact_phone?: string;
  }): Promise<FirmRegistration> {
    return fillRegistration(await this.request<FirmRegistration>('PUT', '/v1/ops/registration', {
      legal_name: req.legal_name, trade_name: req.trade_name ?? '',
      constitution: req.constitution, pan: req.pan ?? '', tan: req.tan ?? '',
      gstin: req.gstin ?? '', registrar_id: req.registrar_id ?? '',
      address_line1: req.address_line1 ?? '', address_line2: req.address_line2 ?? '',
      locality: req.locality ?? '', city: req.city ?? '',
      state_code: req.state_code ?? '', pin_code: req.pin_code ?? '',
      contact_email: req.contact_email ?? '', contact_phone: req.contact_phone ?? '',
    }));
  }

  /** One state's agent registration. RERA s.9 is per state and expires. */
  async opsFileRegistration(req: {
    authority?: string; state_code: string; number: string; valid_from: string; valid_to: string;
  }): Promise<FirmRegistration> {
    return fillRegistration(await this.request<FirmRegistration>(
      'POST', '/v1/ops/registration/authorities', {
        authority: req.authority ?? 'rera', state_code: req.state_code, number: req.number,
        valid_from: req.valid_from, valid_to: req.valid_to,
      }));
  }

  async opsRecordDocument(req: {
    kind: string; object_path: string; filename: string; content_type: string; expires_on?: string;
  }): Promise<FirmRegistration> {
    return fillRegistration(await this.request<FirmRegistration>(
      'POST', '/v1/ops/registration/documents', {
        kind: req.kind, object_path: req.object_path, filename: req.filename,
        content_type: req.content_type, expires_on: req.expires_on ?? '',
      }));
  }

  /** The first sign-in naming the firm it is creating (#31). */
  onboard(organisationName: string): Promise<Onboarded> {
    return this.request('POST', '/v1/onboarding', { organisation_name: organisationName });
  }

  updateMe(patch: { display_name?: string; email?: string }): Promise<Me> {
    return this.request('PATCH', '/v1/me',
      { display_name: patch.display_name ?? '', email: patch.email ?? '' });
  }

  /** Manager-led owner onboarding (#240): the owner's identity reserved
   * against their phone, their organisation, the firm's mandate, and the
   * first property with its units. Idempotent per phone. */
  opsOnboardOwner(req: {
    /** org_id names an owner the firm already acts for — a second property
     * joins their books rather than minting a second set. */
    /** self is the solo manager onboarding a property they own themselves —
     * their own books, no mandate (#268). */
    owner: { org_id?: string; name?: string; phone?: string; email?: string; self?: boolean };
    organisation_name?: string;
    property?: {
      code: string; name: string; kind: string;
      address_line1?: string; address_line2?: string;
      locality?: string; city?: string; district?: string;
      state_code?: string; pin?: string;
    };
    units?: { code: string; kind: string; floor?: number; carpet_area_sqft?: number }[];
    tenancy?: {
      unit_code: string;
      tenant: { name: string; phone: string; email?: string };
      start_on: string; end_on?: string;
      rent_amount_minor: number; deposit_amount_minor?: number;
      due_day: number; notice_days?: number; lock_in_until?: string;
      deductor_class?: string; landlord_residency?: string;
    };
  }): Promise<OwnerOnboarded> {
    return this.request('POST', '/v1/ops/onboardings', {
      owner: {
        org_id: req.owner.org_id ?? '', name: req.owner.name ?? '',
        phone: req.owner.phone ?? '', email: req.owner.email ?? '',
        self: req.owner.self ?? false,
      },
      organisation_name: req.organisation_name ?? '',
      property: {
        code: req.property?.code ?? '', name: req.property?.name ?? '',
        kind: req.property?.kind ?? '', address_line1: req.property?.address_line1 ?? '',
        address_line2: req.property?.address_line2 ?? '', locality: req.property?.locality ?? '',
        city: req.property?.city ?? '', district: req.property?.district ?? '',
        state_code: req.property?.state_code ?? '', pin: req.property?.pin ?? '',
      },
      units: (req.units ?? []).map((u) => ({
        code: u.code, kind: u.kind, floor: u.floor ?? null, carpet_area_sqft: u.carpet_area_sqft ?? 0,
      })),
      tenancy: req.tenancy ? {
        unit_code: req.tenancy.unit_code,
        tenant: {
          name: req.tenancy.tenant.name, phone: req.tenancy.tenant.phone,
          email: req.tenancy.tenant.email ?? '',
        },
        start_on: req.tenancy.start_on, end_on: req.tenancy.end_on ?? '',
        rent_amount_minor: req.tenancy.rent_amount_minor,
        deposit_amount_minor: req.tenancy.deposit_amount_minor ?? 0,
        due_day: req.tenancy.due_day, notice_days: req.tenancy.notice_days ?? 0,
        lock_in_until: req.tenancy.lock_in_until ?? '',
        deductor_class: req.tenancy.deductor_class ?? '',
        landlord_residency: req.tenancy.landlord_residency ?? '',
      } : null,
    });
  }

  async residentMessages(leaseId: string): Promise<ResidentMessage[]> {
    const out = await this.request<{ messages?: ResidentMessage[] }>(
      'GET', `/v1/resident/tenancies/${leaseId}/messages`);
    return out.messages ?? [];
  }

  residentSendMessage(leaseId: string, body: string): Promise<ResidentMessage> {
    return this.request('POST', `/v1/resident/tenancies/${leaseId}/messages`, { body });
  }

  async residentPasses(leaseId: string): Promise<ResidentPass[]> {
    const out = await this.request<{ passes?: ResidentPass[] }>(
      'GET', `/v1/resident/tenancies/${leaseId}/passes`);
    return out.passes ?? [];
  }

  residentCreatePass(leaseId: string,
    req: { name: string; kind: string; valid_hours?: number }): Promise<ResidentPass> {
    return this.request('POST', `/v1/resident/tenancies/${leaseId}/passes`,
      { name: req.name, kind: req.kind, valid_hours: req.valid_hours ?? 0 });
  }

  residentCancelPass(leaseId: string, passId: string): Promise<ResidentPass> {
    return this.request('POST', `/v1/resident/tenancies/${leaseId}/passes/${passId}/cancel`, {});
  }

  /** Serve notice to vacate. moveOutOn is ISO YYYY-MM-DD; served the moment
   * it posts — the review step lives in the app. */
  residentServeNotice(leaseId: string, moveOutOn: string): Promise<ResidentTenancy> {
    return this.request('POST', `/v1/resident/tenancies/${leaseId}/notice`, { move_out_on: moveOutOn });
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
