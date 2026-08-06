import type { AddressHistory, ApplicantPack } from './api';

/** The applicant pack in words: months, gaps, and what is still missing (#259). */

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

// An open-ended address is where they live now, so an empty month reads as such.
export function monthLabel(month: string): string {
  if (!month) return 'now';
  const [y, m] = month.split('-');
  const i = Number(m) - 1;
  return MONTHS[i] ? `${MONTHS[i]} ${y}` : month;
}

export function describeHistory(h: AddressHistory): string {
  if (h.complete) return 'Five years covered';
  if (h.gaps.length === 1) {
    return `1 gap — ${monthLabel(h.gaps[0].from)} to ${monthLabel(h.gaps[0].to)}`;
  }
  return `${h.gaps.length} gaps in the five years`;
}

export function packReadiness(pack: ApplicantPack): string[] {
  const missing: string[] = [];
  if (!pack.full_name?.trim()) missing.push('Applicant name');
  if (!pack.address_history_complete) missing.push('Address history');
  return missing;
}
