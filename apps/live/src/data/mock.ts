/**
 * Sample tenancy for the Live app.
 *
 * DEMONSTRATION data per requirements §9.6 — a fictional Bengaluru tenant.
 * Amounts are integer paise; render them only through `inr` from
 * @rentora/mobile-shared.
 */

export const tenancy = {
  id: 'ten-4021',
  unit: 'Flat 402, Brigade Palm Grove',
  locality: 'Whitefield, Bengaluru 560066',
  agency: 'Anchor Property Care',
  manager: 'Ritika Nambiar',
  rentPaise: 4_20_00_00,
  dueDay: 5,
  paidTo: '15 Sep 2026',
  leaseExpires: '15 Apr 2027',
  depositPaise: 12_60_00_00,
  noticeDays: 60,
  autopay: false,
};

export type Charge = { label: string; paise: number; note?: string };

export const currentInvoice = {
  id: 'inv-2026-08',
  number: 'RNT/2026-27/0412',
  period: '01 Aug – 31 Aug 2026',
  dueOn: '05 Aug 2026',
  daysToDue: 6,
  lines: [
    { label: 'Rent — August 2026', paise: 4_20_00_00 },
    { label: 'Society maintenance', paise: 30_00_00 },
    { label: 'Parking', paise: 10_00_00 },
  ] as Charge[],
  paidPaise: 0,
};

export const totalDue = currentInvoice.lines.reduce((a, l) => a + l.paise, 0) - currentInvoice.paidPaise;

/**
 * Payment methods. UPI carries no instrument cost and therefore no surcharge —
 * the platform fee is borne by the manager at payout, never added to the
 * tenant's payable (requirements §5.5).
 */
export const methods = [
  { id: 'upi', label: 'UPI', hint: 'GPay, PhonePe, Paytm, any UPI app', feePaise: 0, instant: true },
  { id: 'autopay', label: 'Set up autopay', hint: 'Collected automatically each month', feePaise: 0, instant: true },
  { id: 'netbanking', label: 'Net banking', hint: 'Charges may apply', feePaise: 0, instant: false },
  { id: 'card', label: 'Credit or debit card', hint: 'Card charge applies, shown before you pay', feePaise: 4_20_00, instant: true },
  { id: 'offline', label: 'I paid by cash or transfer', hint: 'Record it for your landlord to confirm', feePaise: 0, instant: false },
];

export type Receipt = {
  id: string; n: string; date: string; period: string; paise: number; method: string;
};

export const receipts: Receipt[] = [
  { id: 'r-0411', n: 'RCT/2026-27/0411', date: '04 Jul 2026', period: 'July 2026', paise: 4_60_00_00, method: 'UPI' },
  { id: 'r-0398', n: 'RCT/2026-27/0398', date: '05 Jun 2026', period: 'June 2026', paise: 4_60_00_00, method: 'UPI' },
  { id: 'r-0377', n: 'RCT/2026-27/0377', date: '03 May 2026', period: 'May 2026', paise: 4_60_00_00, method: 'Bank transfer' },
  { id: 'r-0361', n: 'RCT/2026-27/0361', date: '05 Apr 2026', period: 'April 2026', paise: 4_60_00_00, method: 'UPI' },
];

export type Ticket = {
  id: string;
  title: string;
  category: string;
  status: 'Open' | 'Acknowledged' | 'Scheduled' | 'In progress' | 'Resolved';
  raised: string;
  liability: 'Owner' | 'Tenant' | 'Shared';
  liabilityReason: string;
  slot?: string;
  vendor?: string;
  costPaise?: number;
  timeline: { at: string; what: string }[];
};

export const tickets: Ticket[] = [
  {
    id: 't-1180',
    title: 'Geyser not heating',
    category: 'Plumbing',
    status: 'Scheduled',
    raised: '28 Jul 2026',
    liability: 'Owner',
    liabilityReason:
      'Asset defect on an owner-provided fixture, above the ₹1,000 tenant threshold. You pay nothing.',
    slot: 'Thu 31 Jul, 10:00 – 12:00',
    vendor: 'Sahyadri Facility Services',
    costPaise: 78_50_00,
    timeline: [
      { at: '28 Jul, 8:42 PM', what: 'You reported the issue with 3 photos' },
      { at: '28 Jul, 8:42 PM', what: 'Liability assessed — owner-borne' },
      { at: '29 Jul, 9:10 AM', what: 'Ritika acknowledged and requested a quote' },
      { at: '29 Jul, 4:20 PM', what: 'Quote approved within the manager’s spend authority' },
      { at: '30 Jul, 11:05 AM', what: 'Sahyadri Facility Services scheduled a visit' },
    ],
  },
  {
    id: 't-1174',
    title: 'Kitchen tap dripping',
    category: 'Plumbing',
    status: 'Resolved',
    raised: '02 Jun 2026',
    liability: 'Tenant',
    liabilityReason: 'Wear item under the ₹1,000 threshold, tenant-borne under your agreement.',
    vendor: 'Sahyadri Facility Services',
    costPaise: 6_50_00,
    timeline: [
      { at: '02 Jun', what: 'You reported the issue' },
      { at: '03 Jun', what: 'Technician attended and replaced the washer' },
      { at: '03 Jun', what: 'You confirmed completion' },
      { at: '05 Jun', what: '₹650.00 added to your July invoice' },
    ],
  },
];

export const categories = [
  'Plumbing', 'Electrical', 'Carpentry', 'Appliance', 'Pest control', 'Cleaning', 'Common area', 'Other',
];

export const notices = [
  { id: 'n1', title: 'Water supply interruption', body: 'Tank cleaning on Sat 2 Aug, 10 AM – 2 PM.', date: '28 Jul 2026' },
  { id: 'n2', title: 'Routine inspection notice', body: 'Your manager will inspect on 12 Aug at 4 PM. Notice period observed.', date: '25 Jul 2026' },
];

export const messages = [
  { id: 'm1', mine: false, at: '9:12 AM', day: '28 Jul 2026', text: 'Good morning — I have logged the geyser issue and requested a quote. I will confirm the visit slot today.' },
  { id: 'm2', mine: true, at: '9:20 AM', day: '28 Jul 2026', text: 'Thank you. Mornings work best for me.' },
  { id: 'm3', mine: false, at: '11:05 AM', day: '30 Jul 2026', text: 'Confirmed for Thursday 31 July, 10 AM to 12 PM. The technician will call before arriving.' },
];

export const documents = [
  { id: 'd1', name: 'Leave and licence agreement — executed.pdf', date: '12 April 2026' },
  { id: 'd2', name: 'Rent paid statement 2025–26 (for HRA).pdf', date: '05 April 2026' },
  { id: 'd3', name: 'Move-in condition report.pdf', date: '15 April 2026' },
  { id: 'd4', name: 'Deposit acknowledgement.pdf', date: '15 April 2026' },
];

export const profile = {
  initials: 'AV',
  name: 'Ananya Verma',
  email: 'ananya.verma@example.in',
  phone: '+91 98860 21745',
  version: '0.1.0 (1)',
};
