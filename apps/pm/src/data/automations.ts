/**
 * Demonstration automations for the Ops app. ADR-0033, dwellm8#200.
 *
 * DEMONSTRATION data per requirements §9.6 — the same fictional Anchor Property
 * Care portfolio as `mock.ts`. No side effect may ever originate from this file.
 *
 * The shapes mirror `GET /v1/automations`: a `value` beside a `default` on every
 * parameter, and `overridden` naming what this organisation changed — so the
 * screen can show what is customised without comparing numbers itself.
 */

export type AutomationParam = {
  name: string;
  purpose: string;
  unit: string;
  value: number;
  default: number;
  min: number;
  max: number;
};

export type Automation = {
  key: string;
  name: string;
  purpose: string;
  trigger: 'schedule' | 'event';
  on?: string;
  enabled: boolean;
  enabledByDefault: boolean;
  approvalCeilingMinor: number;
  params: AutomationParam[];
  overridden: string[];
  /** What it has actually done. A toggle with nothing beside it asks to be believed. */
  runs: number;
  acted: number;
  awaitingApproval: number;
  failed: number;
  lastRunAt?: string;
};

export const automations: Automation[] = [
  {
    key: 'arrears_ladder',
    name: 'Arrears follow-up',
    purpose: 'Chases a tenancy that has fallen behind, once at each step of the ladder.',
    trigger: 'schedule',
    enabled: true,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [
      { name: 'first_reminder_after', purpose: 'Days overdue before the first reminder', unit: 'days', value: 5, default: 3, min: 1, max: 30 },
      { name: 'second_reminder_after', purpose: 'Days overdue before the second', unit: 'days', value: 7, default: 7, min: 2, max: 60 },
      { name: 'final_reminder_after', purpose: 'Days overdue before the final reminder', unit: 'days', value: 14, default: 14, min: 3, max: 120 },
      { name: 'minimum_arrears_minor', purpose: 'Below this, nobody is chased', unit: 'paise', value: 10000, default: 10000, min: 0, max: 10000000 },
    ],
    overridden: ['first_reminder_after'],
    runs: 46, acted: 31, awaitingApproval: 1, failed: 0, lastRunAt: 'Today, 06:00',
  },
  {
    key: 'lease_expiry_reminder',
    name: 'Lease expiry reminder',
    purpose: 'Tells the owner a tenancy is ending while there is still time to act.',
    trigger: 'schedule',
    enabled: true,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [
      { name: 'remind_within', purpose: 'How far ahead to look', unit: 'days', value: 60, default: 60, min: 7, max: 180 },
    ],
    overridden: [],
    runs: 12, acted: 9, awaitingApproval: 0, failed: 0, lastRunAt: 'Today, 06:00',
  },
  {
    key: 'renewal_kickoff',
    name: 'Renewal checklist',
    purpose: 'Starts the renewal process for a tenancy approaching its end.',
    trigger: 'schedule',
    enabled: true,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [
      { name: 'start_within', purpose: 'How close to the end the process starts', unit: 'days', value: 45, default: 45, min: 14, max: 120 },
    ],
    overridden: [],
    runs: 6, acted: 4, awaitingApproval: 0, failed: 0, lastRunAt: 'Today, 06:00',
  },
  {
    key: 'inspection_scheduling',
    name: 'Routine inspections',
    purpose: 'Schedules a periodic inspection with the notice the tenant is owed.',
    trigger: 'schedule',
    enabled: false,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [
      { name: 'every', purpose: 'How often a tenancy is inspected', unit: 'days', value: 180, default: 180, min: 30, max: 730 },
      { name: 'notice_days', purpose: 'Notice given before the visit', unit: 'days', value: 14, default: 14, min: 1, max: 90 },
    ],
    overridden: ['enabled'],
    runs: 8, acted: 8, awaitingApproval: 0, failed: 0, lastRunAt: '18 Jul, 06:00',
  },
  {
    key: 'compliance_renewal',
    name: 'Compliance renewal',
    purpose: 'Raises a statutory certificate that is about to lapse, while it can still be renewed.',
    trigger: 'schedule',
    enabled: true,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [
      { name: 'remind_within', purpose: 'How far ahead to look', unit: 'days', value: 45, default: 45, min: 7, max: 180 },
    ],
    overridden: [],
    runs: 3, acted: 2, awaitingApproval: 0, failed: 1, lastRunAt: 'Today, 06:00',
  },
  {
    key: 'move_in_checklist',
    name: 'Move-in checklist',
    purpose: 'Starts the move-in process when a tenancy goes live.',
    trigger: 'event',
    on: 'lease.tenancy.started',
    enabled: true,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [],
    overridden: [],
    runs: 5, acted: 5, awaitingApproval: 0, failed: 0, lastRunAt: '29 Jul, 11:42',
  },
  {
    key: 'move_out_checklist',
    name: 'Move-out checklist',
    purpose: 'Starts the move-out process when notice is served, so the exit steps exist before the tenancy ends.',
    trigger: 'event',
    on: 'lease.notice.served',
    enabled: true,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [],
    overridden: [],
    runs: 4, acted: 4, awaitingApproval: 0, failed: 0, lastRunAt: '27 Jul, 09:15',
  },
  {
    key: 'owner_onboarding_checklist',
    name: 'Owner onboarding',
    purpose: 'Starts the onboarding process when an organisation is created.',
    trigger: 'event',
    on: 'identity.organisation.created',
    enabled: true,
    enabledByDefault: true,
    approvalCeilingMinor: 0,
    params: [],
    overridden: [],
    runs: 1, acted: 1, awaitingApproval: 0, failed: 0, lastRunAt: '2 Jul, 14:03',
  },
];

/** What an automation stopped for, rather than acting beyond its ceiling. */
export type Approval = {
  id: string;
  automation: string;
  automationName: string;
  subject: string;
  action: string;
  amountMinor: number;
  ceilingMinor: number;
  requestedAt: string;
};

export const approvals: Approval[] = [
  {
    id: 'ap-1',
    automation: 'arrears_ladder',
    automationName: 'Arrears follow-up',
    subject: 'Palm Grove A-302 — Meera Iyer',
    action: 'Waive the late fee at the final reminder',
    amountMinor: 2_50_000,
    ceilingMinor: 1_00_000,
    requestedAt: 'Yesterday, 06:00',
  },
];

/** What was automated on one record. `GET /v1/automations/history/{kind}/{id}`. */
export type HistoryLine = {
  id: string;
  automation: string;
  automationName: string;
  outcome: 'acted' | 'skipped' | 'awaiting_approval' | 'failed';
  action: string;
  detail?: string;
  occurredAt: string;
};

export const historyByLease: Record<string, HistoryLine[]> = {
  'lease-a302': [
    { id: 'r-4', automation: 'arrears_ladder', automationName: 'Arrears follow-up', outcome: 'awaiting_approval', action: 'Waive the late fee', detail: '₹2,500 is over the ceiling of ₹1,000', occurredAt: 'Yesterday, 06:00' },
    { id: 'r-3', automation: 'arrears_ladder', automationName: 'Arrears follow-up', outcome: 'acted', action: 'Final reminder sent', detail: '₹28,400 outstanding, 14 days since the last charge', occurredAt: '25 Jul, 06:00' },
    { id: 'r-2', automation: 'arrears_ladder', automationName: 'Arrears follow-up', outcome: 'acted', action: 'Second reminder sent', detail: '₹28,400 outstanding, 7 days since the last charge', occurredAt: '18 Jul, 06:00' },
    { id: 'r-1', automation: 'lease_expiry_reminder', automationName: 'Lease expiry reminder', outcome: 'skipped', action: 'Expiry reminder', detail: 'ends 31 Mar, outside the 60-day window', occurredAt: '18 Jul, 06:00' },
  ],
};

export const enabledCount = (list: Automation[]) => list.filter((a) => a.enabled).length;
export const customisedCount = (list: Automation[]) => list.filter((a) => a.overridden.length > 0).length;
