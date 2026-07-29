/**
 * Demonstration listings for Dwellm8 Find.
 *
 * DEMONSTRATION data per requirements §9.6. The marketplace rules this data
 * encodes are the product's, not decoration:
 *
 *  - listing for 90 days, then it expires and must be re-published
 *  - free to list, for owners and agencies alike
 *  - nothing goes live until the ownership and the address are verified
 *  - a boost buys position, never a verification badge
 *  - inspection visitors check in with a QR, so an owner sees who really came
 */

export const LISTING_DAYS = 90;
export const BOOST_PAISE = 9_99_00;

export const photos = {
  a: require('../../assets/listings/a.jpg'),
  b: require('../../assets/listings/b.jpg'),
  c: require('../../assets/listings/c.jpg'),
  d: require('../../assets/listings/d.jpg'),
  e: require('../../assets/listings/e.jpg'),
};

export type Verification = {
  ownership: boolean;   // khata, sale deed or the management agreement
  address: boolean;     // geocoded and checked against the document
  identity: boolean;    // lister's ID, matched to the ownership document
  photosOwn: boolean;   // photographs taken on site, not lifted from a portal
};

export type Listing = {
  id: string;
  title: string;
  locality: string;
  city: string;
  rentPaise: number;
  depositPaise: number;
  bhk: number;
  baths: number;
  sqft: number;
  floor: string;
  furnishing: 'Unfurnished' | 'Semi-furnished' | 'Fully furnished';
  facing: string;
  availableFrom: string;
  preferred: string;
  listedBy: 'Owner' | 'Agency';
  lister: string;
  managed: boolean;
  verification: Verification;
  postedOn: string;
  daysLeft: number;
  boosted: boolean;
  views: number;
  enquiries: number;
  photo: keyof typeof photos;
  amenities: string[];
  about: string;
  inspections: { id: string; when: string; registered: number; attended?: number }[];
};

export const listings: Listing[] = [
  {
    id: 'l-501', title: '3 BHK in Brigade Palm Grove', locality: 'Whitefield', city: 'Bengaluru',
    rentPaise: 4_00_00_00, depositPaise: 12_00_00_00, bhk: 3, baths: 2, sqft: 1450, floor: '5 of 7',
    furnishing: 'Semi-furnished', facing: 'East', availableFrom: '10 Aug 2026',
    preferred: 'Family or working professionals', listedBy: 'Agency', lister: 'Anchor Property Care',
    managed: true,
    verification: { ownership: true, address: true, identity: true, photosOwn: true },
    postedOn: '12 Jul 2026', daysLeft: 72, boosted: true, views: 1840, enquiries: 14, photo: 'a',
    amenities: ['Lift', 'Power backup', 'Covered parking', 'Gym', 'Swimming pool', 'Security', 'Play area'],
    about:
      'Corner flat with a wide balcony off the living room and no building overlooking it. Two covered car parks, a working lift with an AMC in place, and a society that actually maintains the pool. Managed by Anchor, so rent, repairs and the deposit all run through Dwellm8.',
    inspections: [
      { id: 'i1', when: 'Sat 2 Aug, 11:00 – 13:00', registered: 9, attended: 7 },
      { id: 'i2', when: 'Sun 3 Aug, 16:00 – 18:00', registered: 4 },
    ],
  },
  {
    id: 'l-488', title: '2 BHK with two balconies', locality: 'Baner', city: 'Pune',
    rentPaise: 2_90_00_00, depositPaise: 5_80_00_00, bhk: 2, baths: 2, sqft: 1080, floor: '3 of 11',
    furnishing: 'Fully furnished', facing: 'North-east', availableFrom: '05 Aug 2026',
    preferred: 'Anyone', listedBy: 'Owner', lister: 'Nandini Gupta',
    managed: false,
    verification: { ownership: true, address: true, identity: true, photosOwn: false },
    postedOn: '18 Jul 2026', daysLeft: 78, boosted: false, views: 720, enquiries: 6, photo: 'b',
    amenities: ['Lift', 'Power backup', 'Parking', 'Security', 'Piped gas'],
    about:
      'Owner-listed. Fully furnished down to the washing machine, two balconies, and a quiet internal-facing bedroom. The owner lives in Melbourne and would prefer a tenant who is comfortable dealing over video.',
    inspections: [{ id: 'i1', when: 'Sat 2 Aug, 10:00 – 12:00', registered: 3 }],
  },
  {
    id: 'l-475', title: 'Studio near the tech park', locality: 'Marathahalli', city: 'Bengaluru',
    rentPaise: 1_60_00_00, depositPaise: 3_20_00_00, bhk: 1, baths: 1, sqft: 520, floor: '2 of 4',
    furnishing: 'Fully furnished', facing: 'West', availableFrom: 'Immediately',
    preferred: 'Working professionals', listedBy: 'Agency', lister: 'Anchor Property Care',
    managed: true,
    verification: { ownership: true, address: true, identity: true, photosOwn: true },
    postedOn: '02 Jul 2026', daysLeft: 62, boosted: false, views: 2110, enquiries: 22, photo: 'c',
    amenities: ['Lift', 'Power backup', 'Housekeeping', 'Security', 'Wi-Fi'],
    about:
      'Walking distance to two tech parks. Rent includes housekeeping twice a week and building Wi-Fi. Small, but the layout works — the bed is out of sight of the door.',
    inspections: [{ id: 'i1', when: 'Any weekday, 18:00 – 20:00', registered: 12, attended: 9 }],
  },
  {
    id: 'l-462', title: '2 BHK in an older building', locality: 'Indiranagar', city: 'Bengaluru',
    rentPaise: 3_40_00_00, depositPaise: 6_80_00_00, bhk: 2, baths: 2, sqft: 1150, floor: '1 of 3',
    furnishing: 'Unfurnished', facing: 'South', availableFrom: '01 Sep 2026',
    preferred: 'Family', listedBy: 'Owner', lister: 'Deepak Kulkarni',
    managed: false,
    verification: { ownership: true, address: true, identity: false, photosOwn: true },
    postedOn: '24 Jul 2026', daysLeft: 84, boosted: false, views: 410, enquiries: 3, photo: 'd',
    amenities: ['Parking', 'Security', 'Park nearby'],
    about:
      'A 1990s building with proper cross ventilation and thick walls — no lift, first floor. Quiet lane off 12th Main. Owner will not allow structural changes but is relaxed about painting.',
    inspections: [],
  },
  {
    id: 'l-449', title: 'PG bed, twin sharing', locality: 'Marathahalli', city: 'Bengaluru',
    rentPaise: 1_40_00_00, depositPaise: 1_40_00_00, bhk: 1, baths: 1, sqft: 180, floor: '2 of 3',
    furnishing: 'Fully furnished', facing: 'East', availableFrom: '01 Aug 2026',
    preferred: 'Working professionals', listedBy: 'Agency', lister: 'Nest Living LLP',
    managed: true,
    verification: { ownership: true, address: true, identity: true, photosOwn: true },
    postedOn: '20 Jul 2026', daysLeft: 76, boosted: true, views: 1320, enquiries: 18, photo: 'e',
    amenities: ['Meals included', 'Housekeeping', 'Wi-Fi', 'Laundry', 'Power backup'],
    about:
      'Twin sharing with an attached bathroom, three meals a day and laundry twice a week. Warden on site. Rent is per bed and includes everything except electricity above the cap.',
    inspections: [{ id: 'i1', when: 'Walk in any day, 10:00 – 19:00', registered: 6, attended: 5 }],
  },
];

/* ------------------------------------------------------------- my saves */

export const savedSearches = [
  { id: 's1', name: '2–3 BHK, Whitefield', filter: 'Under ₹45,000 · semi or fully furnished', newCount: 4 },
  { id: 's2', name: 'PG beds, Marathahalli', filter: 'Under ₹15,000 · meals included', newCount: 1 },
];

/* ----------------------------------------------------------- enquiries */

export type Enquiry = {
  id: string;
  listingId: string;
  state: 'Enquiry sent' | 'Inspection booked' | 'Attended' | 'Applied' | 'Offer made' | 'Not proceeding';
  at: string;
  detail: string;
};

export const enquiries: Enquiry[] = [
  { id: 'e1', listingId: 'l-501', state: 'Inspection booked', at: 'Sat 2 Aug, 11:00', detail: 'Show the QR at the gate to check in' },
  { id: 'e2', listingId: 'l-475', state: 'Applied', at: '28 Jul', detail: 'Application with the manager, decision expected in 2 days' },
  { id: 'e3', listingId: 'l-462', state: 'Enquiry sent', at: '27 Jul', detail: 'Owner has not replied — we will nudge them after 48 hours' },
];

/* ------------------------------------------------------- my own listing */

export const myListing = {
  id: 'l-488',
  title: '2 BHK with two balconies, Baner',
  postedOn: '18 Jul 2026',
  daysLeft: 78,
  views: 720,
  shortlists: 31,
  enquiries: 6,
  inspectionRegistered: 3,
  inspectionAttended: 0,
  boosted: false,
  boostUplift: '2.4× views, 3× enquiries on comparable Baner listings',
};

export const seeker = {
  initials: 'AR',
  name: 'Ankit Raina',
  phone: '+91 98111 20034',
  budgetPaise: 4_00_00_00,
  city: 'Bengaluru',
  version: '0.1.0 (1)',
};
