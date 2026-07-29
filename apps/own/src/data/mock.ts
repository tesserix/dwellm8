/**
 * Sample portfolio for the Own app.
 *
 * This is DEMONSTRATION data per requirements §9.6 — an Indian owner with two
 * managed units. Every figure is plausible for its market, every person is
 * fictional, and no side effect may ever originate from it.
 */

/**
 * Money is formatted in exactly one place for every app — the local copy this
 * file used to carry could drift from it, so it is gone.
 */
export { inr, inrShort, PLATFORM_FEE_PCT } from '@dwellm8/mobile-shared';

export type Property = {
  id: string;
  address: string;
  locality: string;
  agency: string;
  manager: string;
  managerRole: string;
  paidTo: string;
  leaseExpires: string;
  beds: number;
  baths: number;
  parking: number;
  rentPaise: number;
};

export const properties: Property[] = [
  {
    id: 'blr-402',
    address: 'Flat 402, Brigade Palm Grove',
    locality: 'Whitefield, Bengaluru 560066',
    agency: 'Anchor Property Care',
    manager: 'Ritika Nambiar',
    managerRole: 'Property Manager',
    paidTo: '15 Sep 2026',
    leaseExpires: '15 Apr 2027',
    beds: 3,
    baths: 2,
    parking: 1,
    rentPaise: 4_20_00_00,
  },
  {
    id: 'pun-b12',
    address: 'B-12, Kumar Prithvi',
    locality: 'Baner, Pune 411045',
    agency: 'Anchor Property Care',
    manager: 'Ritika Nambiar',
    managerRole: 'Property Manager',
    paidTo: '05 Aug 2026',
    leaseExpires: '31 Jan 2027',
    beds: 2,
    baths: 2,
    parking: 1,
    rentPaise: 2_75_00_00,
  },
];

export const balance = {
  net: 0,
  opening: 0,
  moneyIn: 0,
  moneyOut: 0,
  uncleared: 0,
  unpaidBills: 0,
};

export const chart = [
  { label: 'Feb', income: 4_20_00_00, expense: 22_00_00 },
  { label: 'Mar', income: 4_20_00_00, expense: 18_50_00 },
  { label: 'Apr', income: 4_20_00_00, expense: 96_00_00 },
  { label: 'May', income: 4_20_00_00, expense: 21_00_00 },
  { label: 'Jun', income: 8_40_00_00, expense: 2_74_00_00 },
  { label: 'Jul', income: 4_20_00_00, expense: 31_40_00 },
];
export const chartMax = 10_00_00_00;

export const expenseBreakdown = [
  { label: 'Management fee', paise: 2_01_60_00, tint: '#6B6BD6' },
  { label: 'Compliance checks', paise: 1_18_00_00, tint: '#E48A55' },
  { label: 'General maintenance', paise: 88_40_00, tint: '#4E6A94' },
  { label: 'Plumbing', paise: 27_50_00, tint: '#B9C0EC' },
  { label: 'Platform fee', paise: 12_55_80, tint: '#57B0CF' },
  { label: 'Society dues', paise: 9_00_00, tint: '#E8B44A' },
];

export type StatementLine = { label: string; paise: number };
export type Statement = {
  id: string;
  n: number;
  date: string;
  month: string;
  propertyId: string;
  netPaise: number;
  incomePaise: number;
  expensePaise: number;
  lines: StatementLine[];
};

export const statements: Statement[] = [
  {
    id: 'st-29', n: 29, date: '15 Jul 2026', month: 'July', propertyId: 'blr-402',
    netPaise: 3_75_88_00, incomePaise: 4_20_00_00, expensePaise: 44_12_00,
    lines: [
      { label: 'Rent collected', paise: 4_20_00_00 },
      { label: 'Management fee (8%)', paise: -33_60_00 },
      { label: 'Platform fee (2.99%)', paise: -12_55_80 },
      { label: 'Annual statement preparation', paise: -3_00_00 },
    ],
  },
  {
    id: 'st-28', n: 28, date: '30 Jun 2026', month: 'June', propertyId: 'blr-402',
    netPaise: 2_92_29_00, incomePaise: 8_40_00_00, expensePaise: 5_47_71_00,
    lines: [
      { label: 'Rent collected', paise: 8_40_00_00 },
      { label: 'General maintenance', paise: -88_40_00 },
      { label: 'Management fee (8%)', paise: -67_20_00 },
      { label: 'Owner compliance checks', paise: -1_12_00_00 },
      { label: 'Plumbing', paise: -27_50_00 },
      { label: 'Platform fee (2.99%)', paise: -25_11_60 },
    ],
  },
  {
    id: 'st-27', n: 27, date: '15 Jun 2026', month: 'June', propertyId: 'blr-402',
    netPaise: 3_38_44_00, incomePaise: 4_20_00_00, expensePaise: 81_56_00,
    lines: [
      { label: 'Rent collected', paise: 4_20_00_00 },
      { label: 'Management fee (8%)', paise: -33_60_00 },
      { label: 'Society dues', paise: -9_00_00 },
      { label: 'Platform fee (2.99%)', paise: -12_55_80 },
    ],
  },
  {
    id: 'st-26', n: 26, date: '15 May 2026', month: 'May', propertyId: 'blr-402',
    netPaise: 3_71_44_00, incomePaise: 4_20_00_00, expensePaise: 48_56_00,
    lines: [
      { label: 'Rent collected', paise: 4_20_00_00 },
      { label: 'Management fee (8%)', paise: -33_60_00 },
      { label: 'Platform fee (2.99%)', paise: -12_55_80 },
    ],
  },
];

export type Activity = {
  id: string;
  kind: 'maintenance' | 'inspection' | 'statement';
  title: string;
  date: string;
  propertyId: string;
  sub?: string;
};

export const activities: Activity[] = [
  { id: 'j-69160', kind: 'maintenance', title: 'Geyser not heating', date: '01 Jul 2026', propertyId: 'blr-402' },
  { id: 'j-69121', kind: 'maintenance', title: 'Gas safety — further works', date: '30 Apr 2026', propertyId: 'blr-402' },
  { id: 'i-2201', kind: 'inspection', title: 'Routine inspection', date: '21 Apr 2026', propertyId: 'blr-402' },
  { id: 'j-68990', kind: 'maintenance', title: 'Fire safety NOC renewal 2026–27', date: '06 Feb 2026', propertyId: 'blr-402' },
  { id: 'j-68830', kind: 'maintenance', title: 'Lift AMC renewal 2026', date: '23 Dec 2025', propertyId: 'blr-402' },
  { id: 'j-68702', kind: 'maintenance', title: 'Electrical safety certificate 2025', date: '08 Sep 2025', propertyId: 'blr-402' },
];

export const upNext = {
  status: 'IN PROGRESS',
  property: 'B-12, Kumar Prithvi, Baner',
  title: 'Front gate latch replacement',
  reported: '18 Jun 2026',
  vendor: 'Sahyadri Facility Services',
};

export const job = {
  id: '69160',
  title: 'Geyser not heating',
  propertyId: 'blr-402',
  completedOn: '31 May 2026',
  description: 'Geyser not heating — bathroom 2',
  detail:
    'Storage water heater fails to reach temperature and trips the RCB after approximately ten minutes. Thermostat tested and found faulty; the element shows scaling consistent with hard water. Recommend thermostat and element replacement, plus a descaling flush. The inlet valve is also seeping and should be replaced while the unit is drained.',
  tradie: 'Sahyadri Facility Services',
  quote: { ref: '16446', vendor: 'Sahyadri Facility Services', paise: 78_50_00, file: 'Geyser_quote_16446.pdf' },
};

export const documents = [
  { id: 'd1', name: 'Statement #29 — OWN11718.pdf', date: '15 July 2026' },
  { id: 'd2', name: 'OWN11718 — Financial summary 1 Jul 2026.pdf', date: '01 July 2026' },
  { id: 'd3', name: 'Statement #28 — OWN11718.pdf', date: '30 June 2026' },
  { id: 'd4', name: 'Statement #27 — OWN11718.pdf', date: '15 June 2026' },
  { id: 'd5', name: 'Leave and licence agreement — executed.pdf', date: '12 April 2026' },
  { id: 'd6', name: 'Fire safety NOC 2026–27.pdf', date: '06 February 2026' },
];

export const threads = [
  { id: 'anchor', agency: 'Anchor Property Care', preview: 'Good morning,' },
  { id: 'greenline', agency: 'Greenline Estates', preview: '' },
];

export const messages = [
  { id: 'm1', mine: true, at: '10:08 PM', day: '30 Jun 2026', text: 'Hi' },
  {
    id: 'm2', mine: true, at: '10:09 PM', day: '30 Jun 2026',
    text: 'For this month why did I receive less than the actual rental? The tenant seems to have paid three months of rent, but I was only paid ₹29,229.00.',
  },
  {
    id: 'm3', mine: false, at: '9:31 AM', day: '01 Jul 2026',
    text: 'Good morning,\n\nAs you are on fortnightly disbursements you would normally receive approximately two weeks of rent per payout. The tenant paid in advance on 15 June, so you received approximately one month of rent less fees, and then a further month on 30 June less fees and maintenance.\n\nKind regards',
  },
];

export const profile = {
  initials: 'SR',
  name: 'Samyak Rout',
  email: 'samyak.rout@gmail.com',
  phone: '+91 98450 44601',
  version: '0.1.0 (1)',
};

/* ------------------------------------------------------------- approvals */

export type Approval = {
  id: string;
  kind: 'spend' | 'renewal' | 'offer';
  title: string;
  propertyId: string;
  vendor: string;
  quotePaise: number;
  raised: string;
  urgency: 'Emergency' | 'Urgent' | 'Routine';
  managerNote: string;
  photos: number;
  liability: 'Owner' | 'Tenant' | 'Shared';
  alternatives?: { vendor: string; paise: number; note: string }[];
};

export const approvals: Approval[] = [
  {
    id: 'ap-offer-1',
    kind: 'offer',
    title: 'Offer on B-12 — ₹29,000 from Tanvi Desai',
    propertyId: 'pun-b12',
    vendor: 'Tanvi Desai',
    quotePaise: 2_90_00_00,
    raised: '28 Jul 2026',
    urgency: 'Urgent',
    liability: 'Owner',
    photos: 0,
    managerNote:
      'Above your asking rent, wants to move in on 5 August for 11 months. Employed at a listed IT firm, no pets, screened and referenced. The flat has been empty 18 days at ₹916 a day.',
  },
  {
    id: 'ap-renewal-1',
    kind: 'renewal',
    title: 'Renewal — Flat 402, Sneha Pillai',
    propertyId: 'blr-402',
    vendor: 'Sneha Pillai',
    quotePaise: 4_41_00_00,
    raised: '25 Jul 2026',
    urgency: 'Routine',
    liability: 'Owner',
    photos: 0,
    managerNote:
      'Fifteen rents paid on time, three small repairs, no disputes. Market is ₹45,500, but re-letting costs about ₹38,000 in vacancy and commission. I recommend +5% to ₹44,100.',
  },
  {
    id: 'ap-2209',
    kind: 'spend',
    title: 'Geyser element and thermostat replacement',
    propertyId: 'blr-402',
    vendor: 'Sahyadri Facility Services',
    quotePaise: 18_40_00,
    raised: '29 Jul 2026',
    urgency: 'Routine',
    liability: 'Owner',
    photos: 3,
    managerNote:
      'Thermostat tested faulty and the element is scaled from hard water. The quote covers thermostat, element, a descaling flush and the seeping inlet valve. Above my ₹10,000 authority, so it needs you.',
    alternatives: [
      { vendor: 'Kiran Electricals', paise: 21_60_00, note: 'Off panel, no AMC history with us' },
      { vendor: 'Replace the unit', paise: 42_00_00, note: 'New 25L geyser, 5 year warranty' },
    ],
  },
  {
    id: 'ap-2196',
    kind: 'spend',
    title: 'Bathroom waterproofing — second coat',
    propertyId: 'pun-b12',
    vendor: 'Nayak Waterproofing',
    quotePaise: 14_00_00,
    raised: '28 Jul 2026',
    urgency: 'Urgent',
    liability: 'Owner',
    photos: 4,
    managerNote:
      'Membrane laid last week has cured. A second coat before tiling protects the flat below and is cheaper than doing this twice.',
  },
];

/* --------------------------------------------------------------- payouts */

export type Payout = {
  id: string;
  date: string;
  propertyId: string;
  grossPaise: number;
  managementPaise: number;
  platformPaise: number;
  repairsPaise: number;
  netPaise: number;
  state: 'Paid' | 'On the way' | 'Scheduled' | 'Held';
  utr?: string;
  note?: string;
};

export const payouts: Payout[] = [
  {
    id: 'po-881', date: '06 Aug 2026', propertyId: 'blr-402',
    grossPaise: 4_50_00_00, managementPaise: 3_60_00, platformPaise: 1_34_55, repairsPaise: 18_40_00,
    netPaise: 4_26_65_45, state: 'Scheduled',
    note: 'Releases once the geyser invoice is settled.',
  },
  {
    id: 'po-874', date: '22 Jul 2026', propertyId: 'blr-402',
    grossPaise: 4_50_00_00, managementPaise: 3_60_00, platformPaise: 1_34_55, repairsPaise: 0,
    netPaise: 4_45_05_45, state: 'Paid', utr: 'HDFCN52026072200418',
  },
  {
    id: 'po-869', date: '06 Jul 2026', propertyId: 'pun-b12',
    grossPaise: 2_75_00_00, managementPaise: 2_20_00, platformPaise: 82_22, repairsPaise: 6_50_00,
    netPaise: 2_65_47_78, state: 'Paid', utr: 'HDFCN52026070600377',
  },
  {
    id: 'po-861', date: '22 Jun 2026', propertyId: 'blr-402',
    grossPaise: 4_50_00_00, managementPaise: 3_60_00, platformPaise: 1_34_55, repairsPaise: 88_40_00,
    netPaise: 3_56_65_45, state: 'Paid', utr: 'HDFCN52026062200290',
  },
];

/* -------------------------------------------------------------- vacancy */

export const vacancy = {
  propertyId: 'pun-b12',
  unit: 'B-12, Kumar Prithvi',
  since: '12 Jul 2026',
  daysVacant: 18,
  askingPaise: 2_90_00_00,
  lastRentPaise: 2_75_00_00,
  dailyCostPaise: 9_16_67,
  enquiries: 14,
  viewings: 6,
  applications: 2,
  portals: ['Housing.com', 'MagicBricks', '99acres'],
  offers: [
    { id: 'of-1', name: 'Tanvi Desai', offerPaise: 2_90_00_00, from: '05 Aug 2026', term: '11 months', note: 'Employed at a listed IT firm, no pets, wants a 3-year intent' },
    { id: 'of-2', name: 'Ankit Raina', offerPaise: 2_80_00_00, from: '15 Aug 2026', term: '11 months', note: 'Asks for the flat to be painted before move-in' },
  ],
};

/* -------------------------------------------------------------- renewal */

export const renewal = {
  propertyId: 'blr-402',
  tenant: 'Sneha Pillai',
  since: '15 Apr 2026',
  expires: '15 Apr 2027',
  decisionBy: '15 Jan 2027',
  currentPaise: 4_20_00_00,
  proposedPaise: 4_41_00_00,
  marketPaise: 4_55_00_00,
  onTimePayments: '15 of 15',
  ticketsRaised: 3,
  note:
    'Market supports ₹45,500 but re-letting costs about ₹38,000 in vacancy and commission. A 5% rise keeps a paying tenant in place.',
};

/* ------------------------------------------------------------- tax pack */

export const taxPack = {
  year: '2025–26',
  rentReceivedPaise: 50_40_00_00,
  municipalTaxPaise: 1_20_00_00,
  standardDeductionPaise: 14_76_00_00,
  interestPaise: 0,
  netIncomePaise: 34_44_00_00,
  tdsCreditPaise: 5_04_00_00,
  documents: [
    { id: 't1', name: 'Annual rent statement 2025–26.pdf', date: '05 Apr 2026' },
    { id: 't2', name: 'Form 16C — TDS on rent.pdf', date: '30 Apr 2026' },
    { id: 't3', name: 'Municipal tax receipt.pdf', date: '18 Jun 2025' },
    { id: 't4', name: 'Interest certificate — home loan.pdf', date: '12 Apr 2026' },
  ],
};

/* -------------------------------------------------------- notifications */

export const notifications = [
  { id: 'n1', kind: 'approval', title: 'Quote needs your approval', body: 'Geyser repair, ₹18,400 — Flat 402', at: '29 Jul', unread: true },
  { id: 'n2', kind: 'money', title: 'Rent received', body: '₹42,000 collected from Sneha Pillai by UPI Autopay', at: '04 Jul', unread: true },
  { id: 'n3', kind: 'vacancy', title: 'Two offers on B-12', body: 'Highest ₹29,000, above your asking rent', at: '28 Jul', unread: false },
  { id: 'n4', kind: 'inspection', title: 'Inspection report filed', body: 'Routine inspection at Flat 402 — two findings', at: '26 Jul', unread: false },
  { id: 'n5', kind: 'money', title: 'Payout released', body: '₹44,505.45 to HDFC ••4471, UTR issued', at: '22 Jul', unread: false },
];
