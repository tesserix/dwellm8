import {
  emptyIdentity, identityReady, missingRule37BC, panLooksRight,
  prefillFromPAN, prefillFromPassport, taxProfileAction, taxProfileFrom, taxSummary,
} from './identity';
import type { TaxProfile } from '@dwellm8/mobile-shared';

// What the manager collects at the owner's identity step (#318). The number is
// held only as far as the request that furnishes it — everything here is about
// getting it right before it goes, because what is stored is a mask.

describe('a permanent account number', () => {
  it('is five letters, four digits and a letter', () => {
    expect(panLooksRight('ABCPD1234E')).toBe(true);
    expect(panLooksRight('abcpd1234e')).toBe(true);
  });

  it('is not anything else', () => {
    for (const bad of ['ABCD1234E', 'ABCPD1234', 'ABCPD12345', '123456789012', '']) {
      expect(panLooksRight(bad)).toBe(false);
    }
  });
});

describe('what the step needs before it can go on', () => {
  it('lets a resident through with no PAN at all', () => {
    // Section 206AA deducts at 20% without one, which is a rate, not a blocker.
    expect(identityReady({ ...emptyIdentity(), resident: true })).toBe(true);
  });

  it('refuses a PAN that is half typed', () => {
    expect(identityReady({ ...emptyIdentity(), resident: true, pan: 'ABCPD12' })).toBe(false);
  });

  it('asks a non-resident which country', () => {
    const nri = { ...emptyIdentity(), resident: false };
    expect(identityReady(nri)).toBe(false);
    expect(identityReady({ ...nri, country: 'AE' })).toBe(true);
    expect(identityReady({ ...nri, country: 'UAE' })).toBe(false);
  });
});

describe('the rule 37BC(2) particulars', () => {
  const furnished = {
    ...emptyIdentity(), resident: false, country: 'AE',
    foreignTin: '784197412345671', trcNumber: 'TRC-AE-2026-0041',
    foreignAddress: 'Villa 12, Al Barsha, Dubai',
    foreignEmail: 'anjali@example.ae', foreignPhone: '+971500000001',
  };

  it('are all five, and naming what is missing is the point', () => {
    expect(missingRule37BC(furnished)).toEqual([]);
    expect(missingRule37BC({ ...furnished, trcNumber: '' })).toEqual(['a tax residency certificate']);
    expect(missingRule37BC({ ...emptyIdentity(), resident: false, country: 'AE' })).toHaveLength(5);
  });

  it('do not apply to a resident, who is not inside 206AA for want of them', () => {
    expect(missingRule37BC({ ...emptyIdentity(), resident: true })).toEqual([]);
  });
});

describe('what is sent', () => {
  it('agrees residency with the country, because together they select the section', () => {
    const resident = taxProfileFrom({ ...emptyIdentity(), resident: true }, '2026-04-01');
    expect(resident.residency).toBe('resident');
    expect(resident.residence_country).toBe('IN');

    const nri = taxProfileFrom(
      { ...emptyIdentity(), resident: false, country: 'ae' }, '2026-04-01');
    expect(nri.residency).toBe('non_resident');
    expect(nri.residence_country).toBe('AE');
  });

  // The fourth character of the PAN is the payee's form, and the form picks the
  // surcharge ladder. The wizard read it off the card and then dropped it, so
  // every landlord was deducted on the individual ladder (#320).
  it('carries the payee form the card decided', () => {
    expect(taxProfileFrom({ ...emptyIdentity(), individual: true }, '2026-04-01').payee_form)
      .toBe('individual');
    expect(taxProfileFrom({ ...emptyIdentity(), individual: false }, '2026-04-01').payee_form)
      .toBe('company');
  });

  it('says who furnished it and from when', () => {
    const out = taxProfileFrom({ ...emptyIdentity(), resident: true }, '2026-04-01');
    expect(out.source).toBe('owner_declaration');
    expect(out.valid_from).toBe('2026-04-01');
  });

  it('carries the PAN whole, uppercased, and omits it when there is none', () => {
    expect(taxProfileFrom({ ...emptyIdentity(), resident: true, pan: 'abcpd1234e' }, '2026-04-01').pan)
      .toBe('ABCPD1234E');
    expect(taxProfileFrom({ ...emptyIdentity(), resident: true }, '2026-04-01').pan).toBeUndefined();
  });

  it('leaves a resident carrying no foreign particulars', () => {
    const out = taxProfileFrom(
      { ...emptyIdentity(), resident: true, foreignTin: '784197412345671' }, '2026-04-01');
    expect(out.foreign_tin).toBeUndefined();
    expect(out.trc_number).toBeUndefined();
  });
});

describe('prefilling from a photographed card', () => {
  it('fills the number the card carries and leaves what the manager typed', () => {
    const before = { ...emptyIdentity(), resident: true, foreignEmail: 'typed@example.in' };
    const after = prefillFromPAN(before, { number: 'ABCPD1234E', name: 'ANJALI SHARMA', individual: true });
    expect(after.pan).toBe('ABCPD1234E');
    expect(after.foreignEmail).toBe('typed@example.in');
  });

  it('reports the name so the wizard can offer it, without overwriting one', () => {
    const scanned = prefillFromPAN(emptyIdentity(), { number: 'ABCPD1234E', name: 'ANJALI SHARMA', individual: true });
    expect(scanned.scannedName).toBe('Anjali Sharma');
  });

  it('carries the fourth character through, because it decides the section', () => {
    const company = prefillFromPAN(emptyIdentity(), { number: 'ABCCD1234E', individual: false });
    expect(company.individual).toBe(false);
  });

  // A passport says where somebody is a national, which is not where they are
  // resident for tax. Prefilling the country from it would be a wrong answer
  // that looks confirmed.
  it('does not take a residence country off a passport', () => {
    const after = prefillFromPassport({ ...emptyIdentity(), resident: false }, {
      surname: 'ERIKSSON', given_names: 'ANNA MARIA', number: 'L898902C3',
      nationality: 'UTO', date_of_birth: '1974-08-12', expires_on: '2032-04-15',
    });
    expect(after.country).toBe('');
    expect(after.scannedName).toBe('Anna Maria Eriksson');
  });

  it('refuses an expired passport as proof of anything', () => {
    const after = prefillFromPassport(emptyIdentity(), {
      surname: 'ERIKSSON', given_names: 'ANNA MARIA', number: 'L898902C3',
      nationality: 'UTO', date_of_birth: '1974-08-12', expires_on: '2012-04-15',
    });
    expect(after.warning).toMatch(/expired/i);
  });
});

// An owner the firm already acts for has furnished this once. The wizard reads
// their profile back but never sees the PAN itself — only that one is on file —
// so posting the draft it seeded would declare "no PAN" over a PAN, and drop the
// landlord to section 206AA's 20% floor (#319).
describe('deciding whether to record what the wizard collected', () => {
  const onFile = (over: Partial<TaxProfile> = {}): TaxProfile => ({
    party_id: 'p1', residency: 'resident', residence_country: 'IN',
    pan_furnished: true, payee_form: 'individual' as const, rule_37bc_furnished: false,
    source: 'owner_declaration', valid_from: '2026-08-07', ...over,
  });

  it('records for an owner with nothing on file', () => {
    expect(taxProfileAction(emptyIdentity(), null).kind).toBe('record');
  });

  it('keeps what is on file when the manager re-declared nothing', () => {
    const seeded = { ...emptyIdentity(), resident: true };
    expect(taxProfileAction(seeded, onFile()).kind).toBe('keep');
  });

  it('records a PAN the manager has now furnished for an owner without one', () => {
    const typed = { ...emptyIdentity(), pan: 'ABCPD1234E' };
    expect(taxProfileAction(typed, onFile({ pan_furnished: false })).kind).toBe('record');
  });

  it('asks for the PAN again rather than erasing it when residency changes', () => {
    const moved = { ...emptyIdentity(), resident: false, country: 'AE' };
    expect(taxProfileAction(moved, onFile()).kind).toBe('needs-pan');
  });

  it('records the move once the PAN is furnished with it', () => {
    const moved = { ...emptyIdentity(), resident: false, country: 'AE', pan: 'ABCPD1234E' };
    expect(taxProfileAction(moved, onFile()).kind).toBe('record');
  });

  it('records a move for an owner who never furnished a PAN', () => {
    const moved = { ...emptyIdentity(), resident: false, country: 'AE' };
    expect(taxProfileAction(moved, onFile({ pan_furnished: false })).kind).toBe('record');
  });
});

// The once-over said "no PAN — 20% applies" for an owner whose profile the same
// screen had just read back as furnished (#319). It reads the record, not the
// blank draft, whenever the record is what will stand.
describe('what the once-over says about tax', () => {
  const furnished: TaxProfile = {
    party_id: 'p1', residency: 'resident', residence_country: 'IN',
    pan_furnished: true, payee_form: 'individual' as const, rule_37bc_furnished: false,
    source: 'owner_declaration', valid_from: '2026-08-07',
  };

  it('reports the PAN on file for an owner nobody re-declared', () => {
    expect(taxSummary(emptyIdentity(), furnished)).toMatch(/PAN on file/i);
  });

  it('reports the draft once the manager has changed it', () => {
    const moved = { ...emptyIdentity(), resident: false, country: 'AE', pan: 'ABCPD1234E' };
    expect(taxSummary(moved, furnished)).toMatch(/Non-resident · AE/);
  });

  it('says the 20% floor applies to a new owner who furnished nothing', () => {
    expect(taxSummary(emptyIdentity(), null)).toMatch(/20%/);
  });
});
