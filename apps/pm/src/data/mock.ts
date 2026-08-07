/**
 * Demonstration data for the four PM screens that have no endpoint yet —
 * inspection (#298), society (#300) and compliance (#301) — plus
 * the Today figures `source.ts` falls back to with no EXPO_PUBLIC_API_URL.
 *
 * One fictional Bengaluru managing agency, Anchor Property Care. Every amount
 * is integer paise; render only through `inr` from @dwellm8/mobile-shared. No
 * side effect may ever originate from this file, and nothing beyond those five
 * consumers may import it — every other screen reads the live API.
 */

export const org = {
  id: 'org-anchor',
  name: 'Anchor Property Care',
  city: 'Bengaluru',
  portfolios: [
    { id: 'p-res', name: 'Residential — South & East', units: 84 },
    { id: 'p-pg', name: 'Nest PG, Marathahalli', units: 48 },
    { id: 'p-soc', name: 'Brigade Palm Grove RWA', units: 120 },
  ],
};

export const staff = {
  initials: 'RN',
  name: 'Ritika Nambiar',
  role: 'Property Manager',
  email: 'ritika@anchorpropertycare.in',
  phone: '+91 98450 11902',
  spendAuthorityPaise: 1_00_00_00, // ₹10,000 without owner approval
  version: '0.1.0 (1)',
};

/* ------------------------------------------------------------------ today */

export const today = {
  date: 'Wednesday, 29 July 2026',
  collectedPaise: 18_46_500_00,
  targetPaise: 21_07_000_00,
  arrearsPaise: 2_60_500_00,
  arrearsCount: 7,
  openTickets: 11,
  breachingSla: 2,
  inspectionsToday: 3,
  visitsDone: 1,
  payoutsPending: 4,
  payoutsPaise: 12_84_600_00,
  occupancyPct: 94,
  vacantUnits: 5,
};

export type Task = {
  id: string;
  kind: 'collect' | 'ticket' | 'inspection' | 'approval' | 'lease' | 'gate';
  title: string;
  where: string;
  at: string;
  urgent?: boolean;
  ref: string;
};

export const worklist: Task[] = [
  { id: 'w1', kind: 'collect', title: 'Arrears call — 12 days late', where: 'A-704, Sobha Dream Acres', at: 'Overdue', urgent: true, ref: 'ten-7041' },
  { id: 'w2', kind: 'ticket', title: 'No water supply — SLA breach in 2h', where: 'Flat 402, Brigade Palm Grove', at: '11:00', urgent: true, ref: 't-2214' },
  { id: 'w3', kind: 'inspection', title: 'Routine inspection', where: 'B-12, Kumar Prithvi, Baner', at: '12:30', ref: 'i-5501' },
  { id: 'w4', kind: 'approval', title: 'Quote ₹18,400 needs owner approval', where: 'Flat 402, Brigade Palm Grove', at: 'Today', ref: 't-2209' },
  { id: 'w5', kind: 'inspection', title: 'Move-out condition report', where: 'Bed 14B, Nest PG', at: '15:00', ref: 'i-5504' },
  { id: 'w6', kind: 'lease', title: 'Renewal offer expires in 3 days', where: 'C-201, Prestige Lakeside', at: 'Today', ref: 'ten-2011' },
  { id: 'w7', kind: 'gate', title: 'Approve 4 pending visitor passes', where: 'Brigade Palm Grove RWA', at: 'Now', ref: 'gate' },
];

/* ------------------------------------------------------------ inspections */

export type Inspection = {
  id: string;
  kind: 'Routine' | 'Move-in' | 'Move-out' | 'Handover';
  unit: string;
  locality: string;
  tenant: string;
  at: string;
  window: string;
  status: 'Scheduled' | 'In progress' | 'Submitted';
  noticeServed: string;
  distanceKm: number;
};

export const inspections: Inspection[] = [
  { id: 'i-5501', kind: 'Routine', unit: 'B-12, Kumar Prithvi', locality: 'Baner, Pune', tenant: 'Rahul Menon', at: 'Today', window: '12:30 – 13:30', status: 'Scheduled', noticeServed: '24 Jul (5 days notice)', distanceKm: 3.2 },
  { id: 'i-5504', kind: 'Move-out', unit: 'Bed 14B, Nest PG', locality: 'Marathahalli', tenant: 'Imran Qureshi', at: 'Today', window: '15:00 – 16:00', status: 'Scheduled', noticeServed: 'Not required', distanceKm: 6.8 },
  { id: 'i-5505', kind: 'Move-in', unit: 'C-201, Prestige Lakeside', locality: 'Whitefield', tenant: 'Fatima Sheikh', at: 'Today', window: '17:30 – 18:30', status: 'Scheduled', noticeServed: 'Not required', distanceKm: 9.1 },
  { id: 'i-5498', kind: 'Routine', unit: 'A-704, Sobha Dream Acres', locality: 'Panathur', tenant: 'Vikram Shetty', at: 'Tomorrow', window: '10:00 – 11:00', status: 'Scheduled', noticeServed: '25 Jul (7 days notice)', distanceKm: 5.4 },
  { id: 'i-5490', kind: 'Routine', unit: 'D-1102, Purva Skywood', locality: 'Hebbal', tenant: 'Meera Iyer', at: '26 Jul', window: 'Completed', status: 'Submitted', noticeServed: '19 Jul', distanceKm: 12.6 },
];

export const inspectionRooms = [
  { id: 'r1', name: 'Entrance and hallway', items: ['Door and locks', 'Walls and ceiling', 'Flooring', 'Lighting'] },
  { id: 'r2', name: 'Living room', items: ['Walls and ceiling', 'Windows', 'Fan and lights', 'Flooring'] },
  { id: 'r3', name: 'Kitchen', items: ['Counters and sink', 'Chimney', 'Taps and drainage', 'Cabinets'] },
  { id: 'r4', name: 'Bathrooms', items: ['Geyser', 'Taps and shower', 'Drainage', 'Seepage and tiles'] },
  { id: 'r5', name: 'Bedrooms', items: ['Walls and ceiling', 'Wardrobes', 'Fan and lights', 'Windows'] },
  { id: 'r6', name: 'Balcony and utility', items: ['Railings', 'Drainage', 'Washing machine point'] },
  { id: 'r7', name: 'Meters and safety', items: ['Electricity meter', 'Water meter', 'Smoke alarm', 'Fire extinguisher'] },
];

/* -------------------------------------------------------------- society */

export const society = {
  name: 'Brigade Palm Grove RWA',
  flats: 120,
  committee: 'Elected Mar 2026 · 7 members',
  corpusPaise: 42_80_000_00,
  monthlyDuePaise: 3_000_00,
  collectedPct: 87,
  defaulters: 9,
  arrearsPaise: 2_16_000_00,
};

export type Due = {
  id: string;
  flat: string;
  resident: string;
  duePaise: number;
  months: number;
  state: 'Paid' | 'Due' | 'Late';
};

export const societyDues: Due[] = [
  { id: 'd1', flat: 'A-204', resident: 'Kiran Prabhu', duePaise: 9_000_00, months: 3, state: 'Late' },
  { id: 'd2', flat: 'B-701', resident: 'Meenal Joshi', duePaise: 6_000_00, months: 2, state: 'Late' },
  { id: 'd3', flat: 'C-105', resident: 'Farhan Ali', duePaise: 3_000_00, months: 1, state: 'Due' },
  { id: 'd4', flat: 'A-402', resident: 'Sneha Pillai', duePaise: 0, months: 0, state: 'Paid' },
  { id: 'd5', flat: 'D-808', resident: 'Ravi Chandran', duePaise: 3_000_00, months: 1, state: 'Due' },
];

export const societyNotices = [
  { id: 'sn1', title: 'Water tank cleaning', body: 'Saturday 2 August, 10:00 – 14:00. Supply off in all towers.', at: '28 Jul', audience: 'All residents' },
  { id: 'sn2', title: 'AGM — 17 August', body: 'Accounts for 2025–26, corpus utilisation and the lift AMC renewal.', at: '25 Jul', audience: 'Owners' },
  { id: 'sn3', title: 'Diwali decoration committee', body: 'Volunteers wanted. Budget approved: ₹40,000.', at: '20 Jul', audience: 'All residents' },
];

export const amenities = [
  { id: 'am1', name: 'Clubhouse', bookings: 4, next: 'Sat 2 Aug, 18:00 — A-204 birthday', ratePaise: 2_500_00 },
  { id: 'am2', name: 'Swimming pool', bookings: 0, next: 'Open 06:00 – 09:00, 16:00 – 20:00', ratePaise: 0 },
  { id: 'am3', name: 'Guest suite', bookings: 2, next: 'Wed 6 Aug — C-105, two nights', ratePaise: 1_200_00 },
  { id: 'am4', name: 'Gym', bookings: 0, next: 'Open 05:00 – 22:00', ratePaise: 0 },
];

/* ------------------------------------------------------------ compliance */

export type Compliance = {
  id: string;
  item: string;
  property: string;
  authority: string;
  expires: string;
  daysLeft: number;
  owner: 'Owner' | 'Society' | 'Manager';
  costPaise: number;
  state: 'Current' | 'Due soon' | 'Expired';
};

export const compliance: Compliance[] = [
  { id: 'cm1', item: 'Lift AMC', property: 'Brigade Palm Grove', authority: 'Vertex Elevators', expires: '23 Dec 2026', daysLeft: 146, owner: 'Society', costPaise: 1_80_000_00, state: 'Current' },
  { id: 'cm2', item: 'Fire safety NOC', property: 'Brigade Palm Grove', authority: 'Karnataka Fire & Emergency Services', expires: '05 Feb 2027', daysLeft: 190, owner: 'Society', costPaise: 45_000_00, state: 'Current' },
  { id: 'cm3', item: 'Electrical safety certificate', property: 'Sobha Dream Acres', authority: 'Licensed electrical inspector', expires: '08 Sep 2026', daysLeft: 40, owner: 'Owner', costPaise: 12_000_00, state: 'Due soon' },
  { id: 'cm4', item: 'Trade licence — PG', property: 'Nest PG, Marathahalli', authority: 'BBMP', expires: '31 Aug 2026', daysLeft: 32, owner: 'Manager', costPaise: 25_000_00, state: 'Due soon' },
  { id: 'cm5', item: 'Police verification — residents', property: 'Nest PG, Marathahalli', authority: 'Marathahalli PS', expires: '14 Jul 2026', daysLeft: -16, owner: 'Manager', costPaise: 0, state: 'Expired' },
  { id: 'cm6', item: 'Property tax receipt', property: 'Anchor Arcade', authority: 'BBMP', expires: '31 Mar 2027', daysLeft: 244, owner: 'Owner', costPaise: 88_000_00, state: 'Current' },
];
