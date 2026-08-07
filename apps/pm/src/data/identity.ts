import type { ScannedPAN, ScannedPassport, TaxProfileInput } from '@dwellm8/mobile-shared';

/**
 * The owner's identity, as the wizard collects it (#318).
 *
 * The identifiers are held here whole because the manager has just read them
 * off a card and the owner is standing there to correct them. They travel once,
 * to the endpoint that turns the PAN into a boolean and the foreign TIN into a
 * mask, and are never written down on this side.
 */
export type IdentityDraft = {
  resident: boolean;
  individual: boolean;
  pan: string;
  country: string;
  foreignTin: string;
  trcNumber: string;
  trcValidFrom: string;
  trcValidTo: string;
  foreignAddress: string;
  foreignEmail: string;
  foreignPhone: string;
  /** A name a scan read, for the wizard to offer — never applied silently. */
  scannedName?: string;
  warning?: string;
};

export const emptyIdentity = (): IdentityDraft => ({
  resident: true, individual: true, pan: '', country: '',
  foreignTin: '', trcNumber: '', trcValidFrom: '', trcValidTo: '',
  foreignAddress: '', foreignEmail: '', foreignPhone: '',
});

const PAN = /^[A-Z]{5}[0-9]{4}[A-Z]$/;

export const panLooksRight = (pan: string) => PAN.test(pan.trim().toUpperCase());

/**
 * A blank PAN is a complete answer: section 206AA deducts at 20% without one,
 * which is a rate the owner has chosen, not a form left half-filled.
 */
export function identityReady(d: IdentityDraft): boolean {
  if (d.pan.trim() !== '' && !panLooksRight(d.pan)) return false;
  if (d.resident) return true;
  return /^[A-Za-z]{2}$/.test(d.country.trim());
}

/**
 * The five particulars of rule 37BC(2). Furnishing all of them takes a
 * non-resident out of the 206AA floor without a PAN; missing one does not.
 */
export function missingRule37BC(d: IdentityDraft): string[] {
  if (d.resident) return [];
  const missing: string[] = [];
  if (!d.foreignTin.trim()) missing.push('a foreign tax identification number');
  if (!d.trcNumber.trim()) missing.push('a tax residency certificate');
  if (!d.foreignAddress.trim()) missing.push('an address abroad');
  if (!d.foreignEmail.trim()) missing.push('an email address');
  if (!d.foreignPhone.trim()) missing.push('a phone number');
  return missing;
}

const some = (v: string) => (v.trim() ? v.trim() : undefined);

export function taxProfileFrom(d: IdentityDraft, validFrom: string): TaxProfileInput {
  const base: TaxProfileInput = {
    residency: d.resident ? 'resident' : 'non_resident',
    residence_country: d.resident ? 'IN' : d.country.trim().toUpperCase(),
    pan: d.pan.trim() ? d.pan.trim().toUpperCase() : undefined,
    source: 'owner_declaration',
    valid_from: validFrom,
  };
  if (d.resident) return base;
  return {
    ...base,
    foreign_tin: some(d.foreignTin),
    trc_number: some(d.trcNumber),
    trc_valid_from: some(d.trcValidFrom),
    trc_valid_to: some(d.trcValidTo),
    foreign_address: some(d.foreignAddress),
    foreign_email: some(d.foreignEmail),
    foreign_phone: some(d.foreignPhone),
  };
}

// OCR hands back a card's shouting capitals; a name goes on an agreement.
const asName = (raw: string) =>
  raw.trim().toLowerCase().split(/\s+/).filter(Boolean)
    .map((w) => w[0].toUpperCase() + w.slice(1)).join(' ');

export function prefillFromPAN(d: IdentityDraft, card: ScannedPAN): IdentityDraft {
  return {
    ...d,
    pan: card.number,
    individual: card.individual,
    scannedName: card.name ? asName(card.name) : d.scannedName,
    warning: undefined,
  };
}

/**
 * A passport proves who somebody is and when that stops being provable. It
 * does not say where they are resident for tax — a national of one country is
 * routinely resident in another — so the country is left for the manager.
 */
export function prefillFromPassport(d: IdentityDraft, p: ScannedPassport): IdentityDraft {
  const expired = p.expires_on < new Date().toISOString().slice(0, 10);
  return {
    ...d,
    scannedName: asName(`${p.given_names} ${p.surname}`),
    warning: expired ? `This passport expired on ${p.expires_on}` : undefined,
  };
}
