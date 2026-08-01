/**
 * Demonstration checklists for the Ops app. ADR-0032, dwellm8#202.
 *
 * DEMONSTRATION data per requirements §9.6 — the same fictional Anchor Property
 * Care portfolio as `mock.ts`. No side effect may ever originate from this file.
 *
 * The shapes mirror the API's responses so the screens change only their source
 * when the app is wired: `blocking`, `state` and `depends_on` are the fields
 * `GET /v1/checklists/{id}` returns, and `daysOverdue` is the portfolio view's.
 */

export type TaskState = 'blocked' | 'pending' | 'done' | 'skipped';

export type ChecklistTask = {
  stepCode: string;
  title: string;
  position: number;
  blocking: boolean;
  ownerRole: string;
  assignee?: string;
  dueOn: string;
  state: TaskState;
  dependsOn: string[];
};

export type Checklist = {
  id: string;
  process: 'move_in' | 'move_out' | 'owner_onboarding' | 'manager_handover' | 'tenancy_renewal';
  templateName: string;
  unit: string;
  locality: string;
  /** The date every task's due date was computed from. */
  anchorOn: string;
  state: 'open' | 'completed' | 'abandoned';
  /** Days past the earliest outstanding task's due date. Derived, never stored. */
  daysOverdue: number;
  tasks: ChecklistTask[];
};

export const processLabel: Record<Checklist['process'], string> = {
  move_in: 'Move-in',
  move_out: 'Move-out',
  owner_onboarding: 'Owner onboarding',
  manager_handover: 'Manager handover',
  tenancy_renewal: 'Renewal',
};

export const checklists: Checklist[] = [
  {
    id: 'ck-1',
    process: 'move_out',
    templateName: 'Move-out — tower',
    unit: 'Palm Grove B-1204',
    locality: 'Whitefield',
    anchorOn: '31 Jul',
    state: 'open',
    daysOverdue: 0,
    tasks: [
      { stepCode: 'notice_acknowledged', title: 'Notice acknowledged and end date confirmed', position: 1, blocking: false, ownerRole: 'manager', dueOn: '1 Jul', state: 'done', dependsOn: [] },
      { stepCode: 'inspection_booked', title: 'Exit inspection scheduled with the tenant', position: 2, blocking: false, ownerRole: 'staff', dueOn: '24 Jul', state: 'done', dependsOn: ['notice_acknowledged'] },
      { stepCode: 'exit_inspection', title: 'Exit inspection with photographs', position: 3, blocking: true, ownerRole: 'field_agent', assignee: 'Devi R', dueOn: '31 Jul', state: 'pending', dependsOn: ['inspection_booked'] },
      { stepCode: 'meter_readings', title: 'Closing meter readings recorded', position: 4, blocking: true, ownerRole: 'field_agent', assignee: 'Devi R', dueOn: '31 Jul', state: 'blocked', dependsOn: ['exit_inspection'] },
      { stepCode: 'keys_collected', title: 'Keys and access cards collected', position: 5, blocking: true, ownerRole: 'field_agent', dueOn: '31 Jul', state: 'blocked', dependsOn: ['exit_inspection'] },
      { stepCode: 'dues_settled', title: 'Outstanding rent and charges settled', position: 6, blocking: true, ownerRole: 'accountant', dueOn: '3 Aug', state: 'blocked', dependsOn: ['meter_readings'] },
      { stepCode: 'deductions_agreed', title: 'Damage deductions itemised and agreed', position: 7, blocking: true, ownerRole: 'manager', dueOn: '5 Aug', state: 'blocked', dependsOn: ['exit_inspection'] },
      { stepCode: 'deposit_settled', title: 'Deposit returned or applied', position: 8, blocking: true, ownerRole: 'accountant', dueOn: '7 Aug', state: 'blocked', dependsOn: ['dues_settled', 'deductions_agreed'] },
      { stepCode: 'society_noc', title: 'Society no-objection obtained', position: 9, blocking: false, ownerRole: 'staff', dueOn: '3 Aug', state: 'blocked', dependsOn: ['keys_collected'] },
      { stepCode: 'unit_relisted', title: 'Unit cleaned and made available', position: 10, blocking: false, ownerRole: 'manager', dueOn: '7 Aug', state: 'blocked', dependsOn: ['keys_collected'] },
    ],
  },
  {
    // The story's edge case: nobody abandoned it and nobody finished it, so it is
    // visible as late rather than reading as a process under way.
    id: 'ck-2',
    process: 'move_in',
    templateName: 'Move-in',
    unit: 'Nest PG — Bed 14C',
    locality: 'Marathahalli',
    anchorOn: '2 Jul',
    state: 'open',
    daysOverdue: 21,
    tasks: [
      { stepCode: 'agreement_signed', title: 'Agreement signed by both parties', position: 1, blocking: true, ownerRole: 'manager', dueOn: '25 Jun', state: 'done', dependsOn: [] },
      { stepCode: 'deposit_received', title: 'Security deposit received', position: 2, blocking: true, ownerRole: 'accountant', dueOn: '1 Jul', state: 'done', dependsOn: ['agreement_signed'] },
      { stepCode: 'police_verification', title: 'Tenant police verification submitted', position: 3, blocking: false, ownerRole: 'staff', dueOn: '9 Jul', state: 'pending', dependsOn: ['agreement_signed'] },
      { stepCode: 'entry_inspection', title: 'Entry inspection with photographs', position: 4, blocking: true, ownerRole: 'warden', dueOn: '2 Jul', state: 'pending', dependsOn: [] },
      { stepCode: 'meter_baseline', title: 'Opening meter readings recorded', position: 5, blocking: true, ownerRole: 'warden', dueOn: '2 Jul', state: 'blocked', dependsOn: ['entry_inspection'] },
    ],
  },
  {
    id: 'ck-3',
    process: 'owner_onboarding',
    templateName: 'Owner onboarding',
    unit: 'Whispering Palms — 3 units',
    locality: 'Sarjapur',
    anchorOn: '20 Jul',
    state: 'open',
    daysOverdue: 4,
    tasks: [
      { stepCode: 'owner_kyc', title: 'Owner identity verified', position: 1, blocking: true, ownerRole: 'staff', dueOn: '20 Jul', state: 'done', dependsOn: [] },
      { stepCode: 'ownership_proof', title: 'Ownership document on file', position: 2, blocking: true, ownerRole: 'staff', dueOn: '22 Jul', state: 'pending', dependsOn: [] },
      { stepCode: 'mandate_signed', title: 'Management mandate signed', position: 3, blocking: true, ownerRole: 'manager', dueOn: '23 Jul', state: 'blocked', dependsOn: ['owner_kyc', 'ownership_proof'] },
      { stepCode: 'payout_account', title: 'Payout account added and verified', position: 4, blocking: true, ownerRole: 'accountant', dueOn: '23 Jul', state: 'pending', dependsOn: ['owner_kyc'] },
      { stepCode: 'portfolio_loaded', title: 'Properties and units loaded', position: 5, blocking: false, ownerRole: 'staff', dueOn: '25 Jul', state: 'blocked', dependsOn: ['mandate_signed'] },
    ],
  },
  {
    id: 'ck-4',
    process: 'tenancy_renewal',
    templateName: 'Tenancy renewal',
    unit: 'Palm Grove A-702',
    locality: 'Whitefield',
    anchorOn: '30 Sep',
    state: 'completed',
    daysOverdue: 0,
    tasks: [
      { stepCode: 'intent_confirmed', title: 'Both sides confirm the tenancy continues', position: 1, blocking: false, ownerRole: 'manager', dueOn: '16 Aug', state: 'done', dependsOn: [] },
      { stepCode: 'rent_agreed', title: 'Revised rent agreed', position: 2, blocking: true, ownerRole: 'manager', dueOn: '31 Aug', state: 'done', dependsOn: ['intent_confirmed'] },
      { stepCode: 'agreement_signed', title: 'Successor agreement signed', position: 3, blocking: true, ownerRole: 'manager', dueOn: '23 Sep', state: 'done', dependsOn: ['rent_agreed'] },
      { stepCode: 'stamp_duty', title: 'Stamp duty paid on the successor', position: 4, blocking: true, ownerRole: 'manager', dueOn: '27 Sep', state: 'done', dependsOn: ['agreement_signed'] },
      { stepCode: 'deposit_topped_up', title: 'Deposit topped up to the revised rent', position: 5, blocking: false, ownerRole: 'accountant', dueOn: '30 Sep', state: 'skipped', dependsOn: ['rent_agreed'] },
    ],
  },
];

/** What a process can be started against, for the sheet on the portfolio screen. */
export const startable = [
  { process: 'move_out' as const, unit: 'Palm Grove A-302', hint: 'Notice served 14 Jul, ends 31 Aug' },
  { process: 'move_in' as const, unit: 'Nest PG — Bed 9A', hint: 'Allotted from 4 Aug' },
  { process: 'manager_handover' as const, unit: 'Brigade Palm Grove RWA', hint: 'Mandate transfers 1 Sep' },
];

/** Blocking work not yet settled, in the order somebody would do it. */
export const outstanding = (c: Checklist): ChecklistTask[] =>
  c.state === 'open'
    ? c.tasks.filter((t) => t.blocking && t.state !== 'done' && t.state !== 'skipped')
    : [];

export const settled = (c: Checklist): number =>
  c.tasks.filter((t) => t.state === 'done' || t.state === 'skipped').length;

/**
 * The titles a task is waiting on. The API refuses an out-of-order settlement
 * naming these; the screen says the same thing before the tap rather than after.
 */
export const waitingOn = (c: Checklist, t: ChecklistTask): string[] =>
  t.dependsOn
    .map((code) => c.tasks.find((x) => x.stepCode === code))
    .filter((x): x is ChecklistTask => !!x && x.state !== 'done' && x.state !== 'skipped')
    .map((x) => x.title);
