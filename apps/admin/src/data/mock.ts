/**
 * Demonstration data for the Admin app.
 *
 * DEMONSTRATION data per requirements §9.6 — the on-call half of the Admin
 * surface. The app carries urgency: alerts, approvals, triage and
 * intervention. Depth — rule tables, fee configuration, bulk operations,
 * reconciliation desks — is deliberately absent and lives on the web console.
 */

export const admin = {
  initials: 'KD',
  name: 'Kavya Desai',
  role: 'Platform Operations',
  team: 'dwellm8 Platform',
  email: 'kavya@dwellm8.app',
  onCall: true,
  onCallUntil: '30 Jul, 09:00',
  version: '0.1.0 (1)',
};

export const health = {
  status: 'Degraded' as 'Healthy' | 'Degraded' | 'Down',
  collectionsToday: 8_42_16_800_00,
  paymentsSuccessPct: 96.2,
  mandateSuccessPct: 88.4,
  webhookLagSec: 42,
  payoutsQueued: 61,
  activeOrgs: 214,
  activeUsers: 18_402,
};

export type Alert = {
  id: string;
  severity: 'P1' | 'P2' | 'P3';
  title: string;
  detail: string;
  service: string;
  at: string;
  state: 'Firing' | 'Acknowledged' | 'Resolved';
  runbook: string;
};

export const alerts: Alert[] = [
  {
    id: 'al-501', severity: 'P1', title: 'UPI mandate presentment failures above threshold',
    detail: 'Mandate debit failures at 11.6% against a 4% baseline for the last 40 minutes, concentrated on one sponsor bank. 214 tenancies affected across 38 organisations.',
    service: 'payments · mandates', at: '09:12, 18 min ago', state: 'Firing',
    runbook: 'Confirm with the PSP, pause automatic retries, and tell affected managers before they call.',
  },
  {
    id: 'al-498', severity: 'P2', title: 'Webhook consumer lag on settlement events',
    detail: 'Settlement webhook consumer is 42 seconds behind. Receipts are still issuing, but payout release will look stale in the manager console.',
    service: 'payments · webhooks', at: '08:47', state: 'Acknowledged',
    runbook: 'Consumer is replay-safe. Scale the consumer group; do not replay by hand.',
  },
  {
    id: 'al-492', severity: 'P3', title: 'eSign provider latency elevated',
    detail: 'Aadhaar eSign completion is taking a median 38 seconds against a 9 second baseline. No failures.',
    service: 'agreements · esign', at: '07:30', state: 'Acknowledged',
    runbook: 'Provider-side. Watch; escalate if completions start failing.',
  },
  {
    id: 'al-486', severity: 'P2', title: 'Statutory rule table review overdue',
    detail: 'Karnataka stamp duty slab has passed its review date by 6 days. Owner: compliance.',
    service: 'compliance · rules', at: 'Yesterday', state: 'Resolved',
    runbook: 'Rule tables are versioned and state-scoped. Review on the web console.',
  },
];

export type Approval = {
  id: string;
  kind: 'Organisation onboarding' | 'Payout exception' | 'Refund' | 'Fee waiver' | 'Data deletion';
  subject: string;
  requestedBy: string;
  amountPaise?: number;
  at: string;
  why: string;
  risk: 'Low' | 'Medium' | 'High';
};

export const approvals: Approval[] = [
  {
    id: 'ap-77', kind: 'Organisation onboarding', subject: 'Meridian Estates, Hyderabad',
    requestedBy: 'Sales — Rohit', at: '08:20', risk: 'Medium',
    why: 'PAN and GSTIN verified, bank penny-drop matched. Director shares a phone number with a previously suspended organisation — confirm before enabling collections.',
  },
  {
    id: 'ap-76', kind: 'Payout exception', subject: 'Anchor Property Care — payout above cap',
    requestedBy: 'Ritika Nambiar', amountPaise: 18_40_000_00, at: '07:55', risk: 'Low',
    why: 'Quarterly commercial rent settled in one payout, exceeding the ₹10,00,000 automatic cap. Source funds cleared 48 hours ago.',
  },
  {
    id: 'ap-75', kind: 'Refund', subject: 'Duplicate rent payment — Sneha Pillai',
    requestedBy: 'Support — Anil', amountPaise: 4_60_00_00, at: 'Yesterday', risk: 'Low',
    why: 'Tenant paid twice within four minutes; the second payment is unallocated on the ledger. Refund to source.',
  },
  {
    id: 'ap-74', kind: 'Data deletion', subject: 'Erasure request — former tenant',
    requestedBy: 'Privacy queue', at: 'Yesterday', risk: 'High',
    why: 'Erasure requested. Statutory retention still applies to the ledger and the executed agreement; only contact data and media may go.',
  },
];

export type Dispute = {
  id: string;
  title: string;
  org: string;
  amountPaise: number;
  raised: string;
  state: 'New' | 'Investigating' | 'With provider' | 'Resolved';
  age: string;
  summary: string;
};

export const disputes: Dispute[] = [
  {
    id: 'dp-311', title: 'Rent debited twice, one receipt', org: 'Anchor Property Care',
    amountPaise: 4_60_00_00, raised: '28 Jul', state: 'Investigating', age: '1 day',
    summary: 'Two successful debits, one ledger posting. The second is unallocated, not missing — refund to source rather than credit.',
  },
  {
    id: 'dp-309', title: 'Payout not received after release', org: 'Skyline Property Managers',
    amountPaise: 7_12_40_00, raised: '27 Jul', state: 'With provider', age: '2 days',
    summary: 'UTR issued but the beneficiary bank shows no credit. Provider has raised it with the beneficiary bank.',
  },
  {
    id: 'dp-305', title: 'Deposit deduction disputed by tenant', org: 'Nest Living LLP',
    amountPaise: 1_40_00_00, raised: '24 Jul', state: 'New', age: '5 days',
    summary: 'Tenant disputes a cleaning deduction. Move-out report and photos are on file; this is a manager decision, not a platform one.',
  },
];

export const reconciliation = {
  date: '29 July 2026',
  bankCredits: 214,
  matched: 209,
  unmatched: 5,
  unmatchedPaise: 3_18_400_00,
  suspensePaise: 92_00_00,
  items: [
    { id: 'r1', ref: 'NEFT/HDFC/883122', paise: 1_20_00_00, why: 'No matching invoice — narration has no tenancy reference' },
    { id: 'r2', ref: 'UPI/9911223344', paise: 46_00_00, why: 'Amount differs from the invoice by ₹200' },
    { id: 'r3', ref: 'IMPS/AXIS/771290', paise: 82_40_00, why: 'Organisation could not be resolved from the virtual account' },
    { id: 'r4', ref: 'NEFT/ICICI/440021', paise: 40_00_00, why: 'Duplicate of a credit already matched' },
    { id: 'r5', ref: 'UPI/8822114455', paise: 30_00_00, why: 'Received after the tenancy closed' },
  ],
};

export type Customer = {
  id: string;
  name: string;
  kind: 'Organisation' | 'Person';
  detail: string;
  state: 'Active' | 'Suspended' | 'Onboarding';
  since: string;
  gmvPaise?: number;
};

export const customers: Customer[] = [
  { id: 'c1', name: 'Anchor Property Care', kind: 'Organisation', detail: 'Bengaluru · 252 units · 3 portfolios', state: 'Active', since: 'Mar 2025', gmvPaise: 2_14_80_000_00 },
  { id: 'c2', name: 'Skyline Property Managers', kind: 'Organisation', detail: 'Mumbai · 610 units', state: 'Active', since: 'Aug 2024', gmvPaise: 6_02_10_000_00 },
  { id: 'c3', name: 'Meridian Estates', kind: 'Organisation', detail: 'Hyderabad · onboarding', state: 'Onboarding', since: 'Jul 2026' },
  { id: 'c4', name: 'Sneha Pillai', kind: 'Person', detail: 'Tenant · Flat 402, Brigade Palm Grove', state: 'Active', since: 'Apr 2026' },
  { id: 'c5', name: 'Ritika Nambiar', kind: 'Person', detail: 'Manager · Anchor Property Care', state: 'Active', since: 'Mar 2025' },
  { id: 'c6', name: 'Gupta Realty', kind: 'Organisation', detail: 'Delhi · suspended for KYC failure', state: 'Suspended', since: 'Jan 2026' },
];

export const webOnly = [
  'Fee and pricing configuration',
  'Statutory rule tables and slabs',
  'Bulk operations and data corrections',
  'Cross-record investigation and exports',
  'Moderation of listings at volume',
];
