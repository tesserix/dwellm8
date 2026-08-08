/**
 * Mobile numbers, in the shape the schema's CHECK enforces.
 *
 * A number is read off a phone as ten digits and stored as E.164; the app closes
 * that gap so a manager is not refused for typing what they were told (#360).
 */

/** The same shape as the server's regexp, so a number this accepts it accepts. */
const shape = /^\+[1-9][0-9]{7,14}$/;

export function isE164(v: string): boolean {
  return shape.test(v);
}

/**
 * E.164 for a number a manager typed. Ten digits is Indian and gets +91; a
 * number already carrying its country code keeps it. Anything else is returned
 * untouched, for `isE164` to refuse where the reason can be shown.
 */
/** Spaces, brackets and dashes are how a number is written down; letters are not. */
const written = /^\+?[\d\s()\-.]+$/;

export function e164(v: string): string {
  const trimmed = v.trim();
  if (!trimmed || !written.test(trimmed)) return trimmed;

  const digits = trimmed.replace(/[^\d]/g, '');
  const plus = trimmed.startsWith('+');

  if (!plus && digits.length === 10) return `+91${digits}`;
  // 0 and 00 are how a country code is dialled, not part of the number.
  const bare = digits.replace(/^0+/, '');
  if (!plus && bare.length === 12 && bare.startsWith('91')) return `+${bare}`;
  if (plus && shape.test(`+${digits}`)) return `+${digits}`;
  if (!plus && bare.length === 10) return `+91${bare}`;

  return trimmed;
}
