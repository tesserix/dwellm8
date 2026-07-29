/**
 * Demonstration data for the Pro app.
 *
 * DEMONSTRATION data per requirements §9.6 — one fictional technician working
 * a Bengaluru day for a panel vendor. Integer paise everywhere; render only
 * through `inr` from @dwellm8/mobile-shared.
 */

export const tech = {
  initials: 'AV',
  name: 'Ashok Verma',
  trade: 'Plumbing and appliances',
  firm: 'Sahyadri Facility Services',
  rating: 4.7,
  jobsDone: 412,
  onTimePct: 94,
  phone: '+91 98863 40021',
  version: '0.1.0 (1)',
};

export type JobState = 'Offered' | 'Accepted' | 'Travelling' | 'On site' | 'Awaiting parts' | 'Completed' | 'Paid';

export type Job = {
  id: string;
  title: string;
  category: string;
  unit: string;
  locality: string;
  contact: string;
  contactPhone: string;
  window: string;
  state: JobState;
  priority: 'Emergency' | 'Urgent' | 'Routine';
  distanceKm: number;
  payPaise: number;
  underAmc: boolean;
  brief: string;
  access: string;
  photosBefore: number;
  photosAfter: number;
  startCode: string;
  timeline: { at: string; what: string; done?: boolean }[];
};

export const jobs: Job[] = [
  {
    id: 'j-9041', title: 'No water supply in the whole flat', category: 'Plumbing',
    unit: 'Flat 402, Brigade Palm Grove', locality: 'Whitefield', contact: 'Sneha Pillai',
    contactPhone: '+91 98860 21745', window: 'Today, 09:00 – 11:00', state: 'Travelling',
    priority: 'Emergency', distanceKm: 2.4, payPaise: 6_50_00, underAmc: false,
    brief: 'No supply at any outlet since last night. Neighbours unaffected — check the flat inlet valve and the overhead pump connection before anything else.',
    access: 'Tenant is home. Gate pass is under your name, the warden has been told.',
    photosBefore: 2, photosAfter: 0, startCode: '4471',
    timeline: [
      { at: '07:52', what: 'Job offered by Anchor Property Care' },
      { at: '07:55', what: 'You accepted' },
      { at: '08:31', what: 'You started travelling' },
      { at: '—', what: 'Enter the tenant OTP to start work', done: false },
    ],
  },
  {
    id: 'j-9037', title: 'Geyser element and thermostat replacement', category: 'Appliance',
    unit: 'Flat 402, Brigade Palm Grove', locality: 'Whitefield', contact: 'Sneha Pillai',
    contactPhone: '+91 98860 21745', window: 'Today, 14:00 – 16:00', state: 'Accepted',
    priority: 'Routine', distanceKm: 2.4, payPaise: 18_40_00, underAmc: false,
    brief: 'Quote approved by the owner. Replace thermostat and element, descale the tank, replace the seeping inlet valve. Carry a 2 kW element and a 15 A thermostat.',
    access: 'Tenant is home after 14:00. Water shutoff is in the utility balcony.',
    photosBefore: 3, photosAfter: 0, startCode: '8130',
    timeline: [
      { at: 'Yesterday', what: 'Quote submitted — ₹18,400' },
      { at: 'Today, 09:14', what: 'Owner approved the quote' },
      { at: 'Today, 09:20', what: 'You accepted the slot' },
    ],
  },
  {
    id: 'j-9052', title: 'Kitchen sink blocked', category: 'Plumbing',
    unit: 'D-1102, Purva Skywood', locality: 'Hebbal', contact: 'Meera Iyer',
    contactPhone: '+91 98807 41120', window: 'Today, 17:00 – 19:00', state: 'Offered',
    priority: 'Urgent', distanceKm: 11.8, payPaise: 9_00_00, underAmc: false,
    brief: 'Standing water in the sink, drains very slowly. Tenant has already tried a plunger and a drain cleaner.',
    access: 'Tenant works from home. Building requires a visitor pass at the gate.',
    photosBefore: 1, photosAfter: 0, startCode: '2265',
    timeline: [{ at: '09:40', what: 'Offered to you — expires in 12 minutes', done: false }],
  },
  {
    id: 'j-9029', title: 'Lift door sensor fault', category: 'Common area',
    unit: 'Tower B, Brigade Palm Grove', locality: 'Whitefield', contact: 'RWA warden',
    contactPhone: '+91 80 4123 9000', window: 'Tomorrow, 10:00 – 12:00', state: 'Accepted',
    priority: 'Urgent', distanceKm: 2.4, payPaise: 0, underAmc: true,
    brief: 'Door reopens intermittently between the 6th and 7th floor. Under AMC — no charge, log parts against the contract.',
    access: 'Warden has the machine room key.',
    photosBefore: 1, photosAfter: 0, startCode: '5590',
    timeline: [{ at: 'Yesterday', what: 'Assigned under the AMC' }],
  },
  {
    id: 'j-8998', title: 'Bathroom tap washer replacement', category: 'Plumbing',
    unit: 'C-201, Prestige Lakeside', locality: 'Whitefield', contact: 'Fatima Sheikh',
    contactPhone: '+91 90350 66019', window: 'Yesterday, 11:00', state: 'Completed',
    priority: 'Routine', distanceKm: 5.2, payPaise: 4_50_00, underAmc: false,
    brief: 'Dripping tap in the guest bathroom. Washer replaced, seat reseated.',
    access: 'Completed.',
    photosBefore: 2, photosAfter: 2, startCode: '7712',
    timeline: [
      { at: 'Yesterday, 11:04', what: 'Started with tenant OTP' },
      { at: 'Yesterday, 11:38', what: 'Completed, 2 photos uploaded' },
      { at: 'Yesterday, 11:39', what: 'Tenant signed off' },
      { at: 'Settles 05 Aug', what: 'Payment scheduled', done: false },
    ],
  },
  {
    id: 'j-8981', title: 'Chimney degrease', category: 'Appliance',
    unit: 'C-201, Prestige Lakeside', locality: 'Whitefield', contact: 'Fatima Sheikh',
    contactPhone: '+91 90350 66019', window: '22 Jul', state: 'Paid',
    priority: 'Routine', distanceKm: 5.2, payPaise: 9_00_00, underAmc: false,
    brief: 'Annual degrease, filters soaked and refitted.',
    access: 'Completed.',
    photosBefore: 2, photosAfter: 2, startCode: '3308',
    timeline: [
      { at: '22 Jul', what: 'Completed' },
      { at: '25 Jul', what: 'Paid — UTR HDFC0092281' },
    ],
  },
];

export const quotes = [
  { id: 'q-441', job: 'Bathroom seepage — D-1102', amountPaise: 42_00_00, state: 'Approved', at: '26 Jul' },
  { id: 'q-448', job: 'Geyser overhaul — Flat 402', amountPaise: 18_40_00, state: 'Approved', at: '29 Jul' },
  { id: 'q-452', job: 'Balcony railing rework — B-12', amountPaise: 26_00_00, state: 'Awaiting owner', at: '29 Jul' },
];

export const parts = [
  { id: 'p1', name: 'Thermostat, 15 A', pricePaise: 4_20_00 },
  { id: 'p2', name: 'Heating element, 2 kW', pricePaise: 6_80_00 },
  { id: 'p3', name: 'Inlet valve, 15 mm', pricePaise: 1_90_00 },
  { id: 'p4', name: 'Descaling flush', pricePaise: 2_50_00 },
  { id: 'p5', name: 'Tap washer set', pricePaise: 60_00 },
];

export const earnings = {
  weekPaise: 34_60_00,
  monthPaise: 1_48_20_00,
  pendingPaise: 27_40_00,
  nextSettlement: '05 Aug 2026',
  jobsThisWeek: 9,
  ledger: [
    { id: 'e1', label: 'Chimney degrease — C-201', at: '25 Jul', paise: 9_00_00, state: 'Paid' },
    { id: 'e2', label: 'Tap washer — C-201', at: 'Yesterday', paise: 4_50_00, state: 'Pending' },
    { id: 'e3', label: 'Waterproofing (part 1) — D-1102', at: '28 Jul', paise: 22_90_00, state: 'Pending' },
    { id: 'e4', label: 'Pest control — Nest PG', at: '21 Jul', paise: 12_00_00, state: 'Paid' },
    { id: 'e5', label: 'TDS deducted (194C, 1%)', at: '25 Jul', paise: -21_00, state: 'Paid' },
  ],
};
