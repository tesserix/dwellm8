/**
 * The TDS decision matrix, in the words a tenant can answer.
 *
 * Presentation only. The API decides the section and the rate — ADR-0024 and
 * ADR-0025 — and nothing here is authoritative; what it does is let the lease
 * builder tell the user what their answers mean *while they are answering*,
 * instead of after a round trip.
 *
 * The reason it exists at all is that the Act's own vocabulary is unanswerable at
 * a form. "Are you liable to audit under section 44AB" is a real question with a
 * real answer that most tenants cannot map onto themselves, and a dropdown of
 * four statutory classes gets guessed at. So the question asked here is about the
 * tenant's situation, and the class is derived from it.
 */

export type DeductorClass =
  | 'individual_no_audit'
  | 'individual_audited'
  | 'business'
  | 'government';

export type Residency = 'resident' | 'non_resident';

export type Section = '194i' | '194ib' | '195';

/** How the deductor classes are asked about, in order of how common they are. */
export const deductorOptions: {
  k: DeductorClass;
  label: string;
  hint: string;
}[] = [
  {
    k: 'individual_no_audit',
    label: 'A person renting a home',
    hint: 'Salaried, self-employed or retired, and your accounts are not audited under section 44AB',
  },
  {
    k: 'business',
    label: 'A company, firm or LLP',
    hint: 'The rent is paid by a business, whatever it is used for',
  },
  {
    k: 'individual_audited',
    label: 'A person whose accounts are audited',
    hint: 'A proprietor or professional above the section 44AB turnover limits — this puts you on the same footing as a company',
  },
  {
    k: 'government',
    label: 'A government body or local authority',
    hint: 'Tax is adjusted by book entry and reported on Form 24G rather than paid by challan',
  },
];

export const residencyOptions: { k: Residency; label: string; hint: string }[] = [
  {
    k: 'resident',
    label: 'Resident in India',
    hint: 'They were in India for at least 182 days in the tax year, or meet the other tests in section 6',
  },
  {
    k: 'non_resident',
    label: 'Non-resident (NRI)',
    hint: 'Including an Indian citizen living abroad. This changes the obligation substantially — see below',
  },
];

export type Path = {
  section: Section;
  /** "194-I", as a human writes it. */
  name: string;
  /** One line: when tax is deducted and on what. */
  when: string;
  /** The threshold in words, or the sentence that says there is none. */
  threshold: string;
  needsTAN: boolean;
  /** Forms in the order they are produced. */
  artefacts: string[];
  /** True only for section 195. */
  needsAcknowledgement: boolean;
};

const paths: Record<Section, Path> = {
  '194i': {
    section: '194i',
    name: '194-I',
    when: 'Every time rent is paid or credited, deposited by the 7th of the next month',
    threshold: 'Applies once the year’s rent to this landlord passes the annual threshold',
    needsTAN: true,
    artefacts: ['Challan', 'Quarterly return 26Q', 'Form 16A to the landlord'],
    needsAcknowledgement: false,
  },
  '194ib': {
    section: '194ib',
    name: '194-IB',
    when: 'Once a year — in March, or in the last month of the tenancy — on the whole period’s rent',
    threshold: 'Applies where the monthly rent passes the section threshold',
    needsTAN: false,
    artefacts: ['Form 26QC', 'Form 16C to the landlord'],
    needsAcknowledgement: false,
  },
  '195': {
    section: '195',
    name: '195',
    when: 'Every time rent is paid or credited',
    threshold: 'No threshold. Tax is deducted from the first rupee',
    needsTAN: true,
    artefacts: ['Challan', 'Quarterly return 27Q', 'Form 16A', 'Forms 15CA and 15CB'],
    needsAcknowledgement: true,
  },
};

/**
 * Which section governs. Residency first, and not as a tie-break: a non-resident
 * landlord puts every payer on section 195. ADR-0024 §1.
 */
export function selectSection(deductor: DeductorClass, residency: Residency): Section {
  if (residency === 'non_resident') return '195';
  if (deductor === 'individual_no_audit') return '194ib';
  return '194i';
}

export function pathFor(deductor: DeductorClass, residency: Residency): Path {
  const p = paths[selectSection(deductor, residency)];
  if (deductor !== 'government') return p;
  return { ...p, artefacts: p.artefacts.map((a) => (a === 'Challan' ? 'Form 24G book entry' : a)) };
}

/**
 * What a tenant is accepting when they acknowledge a section 195 obligation.
 *
 * Three sentences, because there are three separate things and a single "I
 * understand" hides two of them. This is the text the acknowledgement screen
 * shows and the text stored against the acknowledgement.
 */
export const section195Acknowledgement = [
  'Tax must be deducted from every rent payment, starting with the first rupee. There is no threshold to fall below.',
  'The obligation is mine as the payer. If I fail to deduct or deposit it, the tax, the interest under section 201(1A) and the penalty are recovered from me, not from the landlord.',
  'Dwellm8 calculates, reminds and keeps the records. It does not deduct, deposit or file on my behalf.',
];

/** ADR-0024 §8, in one sentence, wherever the product describes its own role. */
export const facilitationNotice =
  'The deduction, deposit, return and certificate are the deductor’s obligation. Dwellm8 computes the deduction, records what was deposited and produces references; it does not deduct, deposit or file on anyone’s behalf.';
