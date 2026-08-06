/**
 * Demonstration portfolio for the Ops app.
 *
 * DEMONSTRATION data per requirements §9.6 — one fictional Bengaluru managing
 * agency, Anchor Property Care, with a residential portfolio, a PG hostel and
 * a society under management. Every amount is integer paise; render only
 * through `inr` from @dwellm8/mobile-shared. No side effect may ever originate
 * from this file.
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

/* ------------------------------------------------------------ collections */

export type Arrear = {
  id: string;
  tenant: string;
  initials: string;
  unit: string;
  phone: string;
  duePaise: number;
  dueSince: string;
  daysLate: number;
  mandate: 'Active' | 'Paused' | 'None' | 'Failed';
  lastContact: string;
  promiseToPay?: string;
  stage: 'Reminder' | 'Follow-up' | 'Notice' | 'Escalated';
};

export const arrears: Arrear[] = [
  {
    id: 'ten-7041', tenant: 'Vikram Shetty', initials: 'VS', unit: 'A-704, Sobha Dream Acres',
    phone: '+91 99012 88451', duePaise: 5_20_00_00, dueSince: '05 Jul 2026', daysLate: 24,
    mandate: 'Failed', lastContact: 'WhatsApp, 26 Jul', stage: 'Notice',
  },
  {
    id: 'ten-3382', tenant: 'Meera Iyer', initials: 'MI', unit: 'D-1102, Purva Skywood',
    phone: '+91 98807 41120', duePaise: 3_80_00_00, dueSince: '05 Jul 2026', daysLate: 24,
    mandate: 'None', lastContact: 'Call, 28 Jul', promiseToPay: '31 Jul 2026', stage: 'Follow-up',
  },
  {
    id: 'ten-5510', tenant: 'Arjun Rao', initials: 'AR', unit: 'Bed 09A, Nest PG',
    phone: '+91 91084 77220', duePaise: 1_20_00_00, dueSince: '10 Jul 2026', daysLate: 19,
    mandate: 'Paused', lastContact: 'SMS, 22 Jul', stage: 'Escalated',
  },
  {
    id: 'ten-2011', tenant: 'Fatima Sheikh', initials: 'FS', unit: 'C-201, Prestige Lakeside',
    phone: '+91 90350 66019', duePaise: 4_50_00_00, dueSince: '20 Jul 2026', daysLate: 9,
    mandate: 'Active', lastContact: 'Auto reminder, 21 Jul', stage: 'Reminder',
  },
  {
    id: 'ten-8890', tenant: 'Deepak Kulkarni', initials: 'DK', unit: 'Shop 3, Anchor Arcade',
    phone: '+91 98452 30071', duePaise: 8_00_00_00, dueSince: '01 Jul 2026', daysLate: 28,
    mandate: 'None', lastContact: 'Visit, 24 Jul', stage: 'Escalated',
  },
  {
    id: 'ten-4102', tenant: 'Sneha Pillai', initials: 'SP', unit: 'Flat 402, Brigade Palm Grove',
    phone: '+91 98860 21745', duePaise: 60_00_00, dueSince: '25 Jul 2026', daysLate: 4,
    mandate: 'Active', lastContact: 'Auto reminder, 26 Jul', stage: 'Reminder',
  },
  {
    id: 'ten-6633', tenant: 'Rahul Menon', initials: 'RM', unit: 'B-12, Kumar Prithvi',
    phone: '+91 97400 55182', duePaise: 2_75_00_00, dueSince: '05 Jul 2026', daysLate: 24,
    mandate: 'Failed', lastContact: 'WhatsApp, 27 Jul', stage: 'Notice',
  },
];

export const collectionActions = [
  { id: 'a1', label: 'Send WhatsApp reminder', hint: 'Approved template, delivery tracked' },
  { id: 'a2', label: 'Log a call outcome', hint: 'Records a promise to pay against the tenancy' },
  { id: 'a3', label: 'Record cash or transfer', hint: 'Receipt issued, ledger posted, owner notified' },
  { id: 'a4', label: 'Retry the mandate', hint: 'Re-presents the UPI autopay debit' },
  { id: 'a5', label: 'Escalate to notice', hint: 'Generates the statutory notice for the manager to serve' },
];

export const receiptMethods = ['Cash', 'UPI to office', 'Bank transfer', 'Cheque'];

/* ---------------------------------------------------------------- tickets */

export type TicketStatus = 'New' | 'Triaged' | 'Quoted' | 'Scheduled' | 'In progress' | 'Awaiting owner' | 'Resolved';

export type OpsTicket = {
  id: string;
  title: string;
  category: string;
  unit: string;
  tenant: string;
  raised: string;
  status: TicketStatus;
  priority: 'Emergency' | 'Urgent' | 'Routine';
  slaHours: number;
  slaLeft: string;
  liability: 'Owner' | 'Tenant' | 'Shared';
  vendor?: string;
  quotePaise?: number;
  photos: number;
  detail: string;
  timeline: { at: string; what: string }[];
};

export const tickets: OpsTicket[] = [
  {
    id: 't-2214', title: 'No water supply in the whole flat', category: 'Plumbing',
    unit: 'Flat 402, Brigade Palm Grove', tenant: 'Sneha Pillai', raised: '29 Jul, 7:10 AM',
    status: 'Triaged', priority: 'Emergency', slaHours: 4, slaLeft: '2h 10m left',
    liability: 'Owner', photos: 2,
    detail: 'No supply at any outlet since last night. Neighbouring flats unaffected, so the riser is likely fine and the fault sits at the flat inlet or the overhead pump connection.',
    timeline: [
      { at: '29 Jul, 7:10 AM', what: 'Tenant reported with 2 photos' },
      { at: '29 Jul, 7:14 AM', what: 'Auto-triaged as emergency — 4 hour SLA' },
      { at: '29 Jul, 7:40 AM', what: 'You acknowledged and called the tenant' },
    ],
  },
  {
    id: 't-2209', title: 'Geyser element and thermostat replacement', category: 'Appliance',
    unit: 'Flat 402, Brigade Palm Grove', tenant: 'Sneha Pillai', raised: '28 Jul',
    status: 'Awaiting owner', priority: 'Routine', slaHours: 72, slaLeft: '1d 4h left',
    liability: 'Owner', vendor: 'Sahyadri Facility Services', quotePaise: 18_40_00, photos: 3,
    detail: 'Thermostat tested faulty, element scaled. Quote covers thermostat, element, descaling flush and inlet valve. Above your ₹10,000 authority, so it needs the owner.',
    timeline: [
      { at: '28 Jul, 8:42 PM', what: 'Tenant reported with 3 photos' },
      { at: '29 Jul, 9:10 AM', what: 'You requested a quote from Sahyadri' },
      { at: '29 Jul, 4:20 PM', what: 'Quote ₹18,400 received — above your authority' },
      { at: '29 Jul, 4:25 PM', what: 'Sent to owner for approval' },
    ],
  },
  {
    id: 't-2201', title: 'Lift stopping between floors', category: 'Common area',
    unit: 'Tower B, Brigade Palm Grove', tenant: 'RWA committee', raised: '27 Jul',
    status: 'Scheduled', priority: 'Urgent', slaHours: 24, slaLeft: 'On time',
    liability: 'Shared', vendor: 'Vertex Elevators AMC', photos: 1,
    detail: 'Intermittent stop between the 6th and 7th floor. Under AMC — no charge expected, engineer visit booked.',
    timeline: [
      { at: '27 Jul', what: 'Warden reported' },
      { at: '27 Jul', what: 'Raised against the AMC with Vertex' },
      { at: '28 Jul', what: 'Engineer visit booked for 30 Jul, 10:00' },
    ],
  },
  {
    id: 't-2196', title: 'Bathroom seepage into the flat below', category: 'Plumbing',
    unit: 'D-1102, Purva Skywood', tenant: 'Meera Iyer', raised: '25 Jul',
    status: 'In progress', priority: 'Urgent', slaHours: 24, slaLeft: 'Breached by 6h',
    liability: 'Owner', vendor: 'Nayak Waterproofing', quotePaise: 42_00_00, photos: 4,
    detail: 'Seepage traced to failed waterproofing at the shower trap. Chipping done, membrane laid, curing for 48 hours before tiling.',
    timeline: [
      { at: '25 Jul', what: 'Tenant below reported the leak' },
      { at: '26 Jul', what: 'Quote ₹42,000 approved by the owner' },
      { at: '28 Jul', what: 'Nayak started work' },
    ],
  },
  {
    id: 't-2188', title: 'Wi-Fi down in the common room', category: 'Internet',
    unit: 'Nest PG, Marathahalli', tenant: 'Warden', raised: '24 Jul',
    status: 'New', priority: 'Routine', slaHours: 72, slaLeft: '2d left',
    liability: 'Owner', photos: 0,
    detail: 'Residents report the common room access point has been offline since Friday. Router restarted with no change.',
    timeline: [{ at: '24 Jul', what: 'Warden reported' }],
  },
  {
    id: 't-2170', title: 'Kitchen chimney servicing', category: 'Appliance',
    unit: 'C-201, Prestige Lakeside', tenant: 'Fatima Sheikh', raised: '20 Jul',
    status: 'Resolved', priority: 'Routine', slaHours: 72, slaLeft: 'Closed',
    liability: 'Tenant', vendor: 'CleanEdge Services', quotePaise: 9_00_00, photos: 2,
    detail: 'Annual chimney degrease. Tenant-borne under the agreement; ₹900 recharged to the August invoice.',
    timeline: [
      { at: '20 Jul', what: 'Tenant requested' },
      { at: '22 Jul', what: 'CleanEdge attended' },
      { at: '22 Jul', what: '₹900 recharged to August invoice' },
    ],
  },
];

export type Vendor = {
  id: string;
  name: string;
  trade: string;
  rating: number;
  jobs: number;
  responseMins: number;
  onPanel: boolean;
  ratePaise: number;
};

export const vendors: Vendor[] = [
  { id: 'v1', name: 'Sahyadri Facility Services', trade: 'Plumbing, appliance', rating: 4.6, jobs: 142, responseMins: 38, onPanel: true, ratePaise: 6_50_00 },
  { id: 'v2', name: 'Nayak Waterproofing', trade: 'Waterproofing, civil', rating: 4.4, jobs: 61, responseMins: 95, onPanel: true, ratePaise: 12_00_00 },
  { id: 'v3', name: 'Vertex Elevators AMC', trade: 'Lifts (under AMC)', rating: 4.1, jobs: 24, responseMins: 180, onPanel: true, ratePaise: 0 },
  { id: 'v4', name: 'CleanEdge Services', trade: 'Cleaning, chimney, pest', rating: 4.7, jobs: 208, responseMins: 45, onPanel: true, ratePaise: 4_50_00 },
  { id: 'v5', name: 'Kiran Electricals', trade: 'Electrical', rating: 4.2, jobs: 77, responseMins: 55, onPanel: false, ratePaise: 5_00_00 },
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

/* -------------------------------------------------------------- portfolio */

export type Unit = {
  id: string;
  label: string;
  tenant?: string;
  rentPaise: number;
  status: 'Occupied' | 'Vacant' | 'Notice' | 'Under repair';
  paidTo?: string;
  leaseEnds?: string;
};

export type OpsProperty = {
  id: string;
  name: string;
  locality: string;
  owner: string;
  ownerPhone: string;
  kind: 'Residential' | 'Hostel' | 'Commercial' | 'Society';
  units: Unit[];
  monthlyRentPaise: number;
  openTickets: number;
};

export const propertiesOps: OpsProperty[] = [
  {
    id: 'pr-bpg', name: 'Brigade Palm Grove', locality: 'Whitefield, Bengaluru 560066',
    owner: 'Samyak Rout', ownerPhone: '+91 98450 44601', kind: 'Residential', monthlyRentPaise: 12_60_00_00, openTickets: 3,
    units: [
      { id: 'u-402', label: 'Flat 402', tenant: 'Sneha Pillai', rentPaise: 4_20_00_00, status: 'Occupied', paidTo: '31 Jul 2026', leaseEnds: '15 Apr 2027' },
      { id: 'u-403', label: 'Flat 403', tenant: 'Kabir Anand', rentPaise: 4_40_00_00, status: 'Occupied', paidTo: '31 Aug 2026', leaseEnds: '01 Dec 2026' },
      { id: 'u-501', label: 'Flat 501', rentPaise: 4_00_00_00, status: 'Vacant' },
    ],
  },
  {
    id: 'pr-sda', name: 'Sobha Dream Acres', locality: 'Panathur, Bengaluru 560087',
    owner: 'Nandini Gupta (NRI)', ownerPhone: '+61 412 880 199', kind: 'Residential', monthlyRentPaise: 5_20_00_00, openTickets: 1,
    units: [
      { id: 'u-704', label: 'A-704', tenant: 'Vikram Shetty', rentPaise: 5_20_00_00, status: 'Occupied', paidTo: '30 Jun 2026', leaseEnds: '31 Mar 2027' },
    ],
  },
  {
    id: 'pr-nest', name: 'Nest PG, Marathahalli', locality: 'Marathahalli, Bengaluru 560037',
    owner: 'Nest Living LLP', ownerPhone: '+91 80 4123 7788', kind: 'Hostel', monthlyRentPaise: 6_72_00_00, openTickets: 2,
    units: [
      { id: 'u-09a', label: 'Bed 09A', tenant: 'Arjun Rao', rentPaise: 1_20_00_00, status: 'Occupied', paidTo: '30 Jun 2026' },
      { id: 'u-14b', label: 'Bed 14B', tenant: 'Imran Qureshi', rentPaise: 1_40_00_00, status: 'Notice', paidTo: '31 Jul 2026', leaseEnds: '31 Jul 2026' },
      { id: 'u-15a', label: 'Bed 15A', rentPaise: 1_40_00_00, status: 'Vacant' },
    ],
  },
  {
    id: 'pr-arc', name: 'Anchor Arcade', locality: 'Indiranagar, Bengaluru 560038',
    owner: 'Anchor Holdings', ownerPhone: '+91 80 4009 1200', kind: 'Commercial', monthlyRentPaise: 8_00_00_00, openTickets: 0,
    units: [
      { id: 'u-s3', label: 'Shop 3', tenant: 'Deepak Kulkarni', rentPaise: 8_00_00_00, status: 'Occupied', paidTo: '30 Jun 2026', leaseEnds: '31 Aug 2028' },
    ],
  },
];

/* ------------------------------------------------------------------- beds */

export type Bed = {
  id: string;
  label: string;
  room: string;
  floor: number;
  sharing: 2 | 3 | 4;
  resident?: string;
  status: 'Occupied' | 'Vacant' | 'Reserved' | 'Notice';
  rentPaise: number;
  dueState?: 'Paid' | 'Due' | 'Late';
};

export const beds: Bed[] = [
  { id: 'b-09a', label: '09A', room: '09', floor: 1, sharing: 2, resident: 'Arjun Rao', status: 'Occupied', rentPaise: 1_20_00_00, dueState: 'Late' },
  { id: 'b-09b', label: '09B', room: '09', floor: 1, sharing: 2, resident: 'Nikhil Bose', status: 'Occupied', rentPaise: 1_20_00_00, dueState: 'Paid' },
  { id: 'b-10a', label: '10A', room: '10', floor: 1, sharing: 3, resident: 'Sameer Khan', status: 'Occupied', rentPaise: 1_00_00_00, dueState: 'Paid' },
  { id: 'b-10b', label: '10B', room: '10', floor: 1, sharing: 3, status: 'Vacant', rentPaise: 1_00_00_00 },
  { id: 'b-10c', label: '10C', room: '10', floor: 1, sharing: 3, resident: 'Yash Patel', status: 'Occupied', rentPaise: 1_00_00_00, dueState: 'Due' },
  { id: 'b-14a', label: '14A', room: '14', floor: 2, sharing: 2, resident: 'Rohan Das', status: 'Occupied', rentPaise: 1_40_00_00, dueState: 'Paid' },
  { id: 'b-14b', label: '14B', room: '14', floor: 2, sharing: 2, resident: 'Imran Qureshi', status: 'Notice', rentPaise: 1_40_00_00, dueState: 'Paid' },
  { id: 'b-15a', label: '15A', room: '15', floor: 2, sharing: 2, status: 'Reserved', rentPaise: 1_40_00_00 },
  { id: 'b-15b', label: '15B', room: '15', floor: 2, sharing: 2, status: 'Vacant', rentPaise: 1_40_00_00 },
];

export const bedWaitlist = [
  { id: 'wl1', name: 'Pranav Joshi', preference: 'Twin sharing, 2nd floor', from: '01 Aug 2026', phone: '+91 90080 12345' },
  { id: 'wl2', name: 'Aditya Sharma', preference: 'Any bed, AC room', from: '05 Aug 2026', phone: '+91 90080 98765' },
];

/* ------------------------------------------------------------------- gate */

export type GateEntry = {
  id: string;
  kind: 'Visitor' | 'Delivery' | 'Cab' | 'Staff';
  who: string;
  detail: string;
  unit: string;
  at: string;
  state: 'Pending' | 'Approved' | 'Denied' | 'Inside' | 'Left';
};

export const gateLog: GateEntry[] = [
  { id: 'g1', kind: 'Visitor', who: 'Priya Menon', detail: 'Guest of the resident', unit: 'Flat 402', at: '09:42', state: 'Pending' },
  { id: 'g2', kind: 'Delivery', who: 'Blinkit', detail: 'Grocery, OTP required', unit: 'Flat 403', at: '09:36', state: 'Pending' },
  { id: 'g3', kind: 'Cab', who: 'KA 05 MJ 8821', detail: 'Airport pickup', unit: 'Flat 501', at: '09:20', state: 'Approved' },
  { id: 'g4', kind: 'Visitor', who: 'Ashok Verma', detail: 'Plumber — Sahyadri', unit: 'Flat 402', at: '08:55', state: 'Inside' },
  { id: 'g5', kind: 'Delivery', who: 'Amazon', detail: 'Parcel left at the gate desk', unit: 'Flat 403', at: '08:31', state: 'Approved' },
  { id: 'g6', kind: 'Visitor', who: 'Neha Bhat', detail: 'Guest of the resident', unit: 'Flat 501', at: '20:10 yesterday', state: 'Left' },
];

export const domesticStaff = [
  { id: 'ds1', name: 'Lakshmi R', role: 'Housekeeping', units: 'Flats 402, 403', in: '07:10', out: '—', present: true },
  { id: 'ds2', name: 'Ramesh N', role: 'Cook', units: 'Flat 501', in: '06:40', out: '09:05', present: false },
  { id: 'ds3', name: 'Salma B', role: 'Housekeeping', units: 'Flat 704', in: '—', out: '—', present: false },
];

/* ------------------------------------------------------------------ leads */

export type Lead = {
  id: string;
  name: string;
  initials: string;
  interest: string;
  budgetPaise: number;
  source: 'Portal' | 'Website' | 'Referral' | 'Walk-in';
  stage: 'New' | 'Contacted' | 'Viewing booked' | 'Application' | 'Offer';
  since: string;
  phone: string;
};

export const leads: Lead[] = [
  { id: 'l1', name: 'Ankit Raina', initials: 'AR', interest: 'Flat 501, Brigade Palm Grove', budgetPaise: 4_00_00_00, source: 'Portal', stage: 'Viewing booked', since: '28 Jul', phone: '+91 98111 20034' },
  { id: 'l2', name: 'Divya Nair', initials: 'DN', interest: '2BHK, Whitefield', budgetPaise: 3_50_00_00, source: 'Website', stage: 'New', since: '29 Jul', phone: '+91 98450 71122' },
  { id: 'l3', name: 'Pranav Joshi', initials: 'PJ', interest: 'Bed, Nest PG', budgetPaise: 1_40_00_00, source: 'Referral', stage: 'Application', since: '26 Jul', phone: '+91 90080 12345' },
  { id: 'l4', name: 'Tanvi Desai', initials: 'TD', interest: 'Flat 501, Brigade Palm Grove', budgetPaise: 4_20_00_00, source: 'Portal', stage: 'Offer', since: '22 Jul', phone: '+91 99001 45566' },
];

/* --------------------------------------------------------------- payouts */

export const payouts = [
  { id: 'po-881', owner: 'Samyak Rout', propertyName: 'Brigade Palm Grove', grossPaise: 8_60_00_00, feePaise: 25_71_40, netPaise: 7_58_28_60, state: 'Ready to release', due: 'Today' },
  { id: 'po-880', owner: 'Nandini Gupta', propertyName: 'Sobha Dream Acres', grossPaise: 0, feePaise: 0, netPaise: 0, state: 'Blocked — rent not collected', due: 'Today' },
  { id: 'po-879', owner: 'Nest Living LLP', propertyName: 'Nest PG', grossPaise: 6_12_00_00, feePaise: 18_29_88, netPaise: 5_44_70_12, state: 'Ready to release', due: 'Tomorrow' },
  { id: 'po-878', owner: 'Anchor Holdings', propertyName: 'Anchor Arcade', grossPaise: 0, feePaise: 0, netPaise: 0, state: 'Blocked — TDS certificate pending', due: '31 Jul' },
];

/* ---------------------------------------------------------------- inbox */

export const threads = [
  { id: 'th1', who: 'Sneha Pillai', initials: 'SP', unit: 'Flat 402', preview: 'The plumber has not arrived yet…', at: '10:02', unread: 2 },
  { id: 'th2', who: 'Samyak Rout', initials: 'SR', unit: 'Owner — Brigade Palm Grove', preview: 'Approved. Please go ahead with the quote.', at: '09:14', unread: 0 },
  { id: 'th3', who: 'Vikram Shetty', initials: 'VS', unit: 'A-704', preview: 'I will clear it by Friday, salary is delayed.', at: 'Yesterday', unread: 1 },
  { id: 'th4', who: 'Nest PG warden', initials: 'NW', unit: 'Nest PG', preview: 'Bed 14B keys collected.', at: 'Yesterday', unread: 0 },
];

export const messages = [
  { id: 'm1', mine: false, at: '7:12 AM', day: '29 Jul 2026', text: 'Good morning ma’am, there is no water in the flat since last night. I have attached photos of the taps.' },
  { id: 'm2', mine: true, at: '7:40 AM', day: '29 Jul 2026', text: 'Thank you for reporting. I have logged it as an emergency and Sahyadri has been dispatched. Someone will be there before 11 AM.' },
  { id: 'm3', mine: false, at: '10:02 AM', day: '29 Jul 2026', text: 'The plumber has not arrived yet — should I wait at home?' },
];

/* -------------------------------------------------------------- approvals */

export const approvals = [
  { id: 'ap1', title: 'Geyser repair quote', ref: 't-2209', amountPaise: 18_40_00, who: 'Samyak Rout', property: 'Flat 402, Brigade Palm Grove', state: 'Sent 29 Jul, 4:25 PM' },
  { id: 'ap2', title: 'Waterproofing — second coat', ref: 't-2196', amountPaise: 14_00_00, who: 'Nandini Gupta', property: 'D-1102, Purva Skywood', state: 'Sent 28 Jul' },
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
