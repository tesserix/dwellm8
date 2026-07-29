/**
 * Money formatting for the Indian market.
 *
 * Amounts are integer paise everywhere in dwellm8 — a float never touches
 * money, in any layer. These helpers are the only sanctioned way to render an
 * amount, so grouping and rounding cannot drift between apps.
 */

/** Format paise as Indian rupees with lakh/crore grouping: 4200000 → ₹42,000.00 */
export function inr(paise: number, opts: { sign?: boolean; noPaise?: boolean } = {}): string {
  const neg = paise < 0;
  const rupees = Math.abs(paise) / 100;
  const fixed = rupees.toFixed(2);
  const [whole, frac] = fixed.split('.');
  const last3 = whole.slice(-3);
  const rest = whole.slice(0, -3);
  const grouped = rest ? `${rest.replace(/\B(?=(\d{2})+(?!\d))/g, ',')},${last3}` : last3;
  const body = opts.noPaise ? `₹${grouped}` : `₹${grouped}.${frac}`;
  if (opts.sign) return `${neg ? '−' : '+'}${body}`;
  return neg ? `−${body}` : body;
}

/** A short form for chart axes and tiles: 4200000 → ₹42K */
export function inrShort(paise: number): string {
  const r = Math.abs(paise) / 100;
  if (r >= 1_00_00_000) return `₹${(r / 1_00_00_000).toFixed(r % 1_00_00_000 === 0 ? 0 : 1)}Cr`;
  if (r >= 1_00_000) return `₹${(r / 1_00_000).toFixed(r % 1_00_000 === 0 ? 0 : 1)}L`;
  if (r >= 1_000) return `₹${Math.round(r / 1000)}K`;
  return `₹${Math.round(r)}`;
}

/** Percentage of a paise amount, rounded to the nearest paisa. Never floats out. */
export function pctOf(paise: number, percent: number): number {
  return Math.round(paise * (percent / 100));
}

export const PLATFORM_FEE_PCT = 2.99;
