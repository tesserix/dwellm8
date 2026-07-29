/**
 * Sample portfolio for the Own app.
 *
 * This is DEMONSTRATION data per requirements §9.6 — an Indian owner with two
 * managed units. Every figure is plausible for its market, every person is
 * fictional, and no side effect may ever originate from it.
 */

export const inr = (paise: number, opts: { sign?: boolean } = {}) => {
  const neg = paise < 0;
  const rupees = Math.abs(paise) / 100;
  const [w, f] = rupees.toFixed(2).split('.');
  // Indian grouping: last three, then pairs.
  const last3 = w.slice(-3);
  const rest = w.slice(0, -3);
  const grouped = rest ? rest.replace(/\B(?=(\d{2})+(?!\d))/g, ',') + ',' + last3 : last3;
  const body = `₹${grouped}.${f}`;
  if (opts.sign) return `${neg ? '-' : '+'}${body}`;
  return neg ? `-${body}` : body;
};

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
