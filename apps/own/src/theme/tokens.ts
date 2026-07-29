/**
 * Rentora design tokens — Own app.
 *
 * The visual language: a pale blue-tinted canvas, white rounded cards with a
 * soft shadow, teal links, and money always coloured (green in, red out).
 * Everything is deliberately calm — this is an app people open when they are
 * worried about money, so it should never feel busy.
 */

export const color = {
  // canvas
  bgTop: '#E9F0FA',
  bgBottom: '#E8F6FA',
  bgBand: '#E4EBF7',

  // surfaces
  card: '#FFFFFF',
  cardMuted: '#F7FAFC',
  headerBar: '#FFFFFF',

  // ink
  ink: '#3F4C5A',
  inkStrong: '#2C3743',
  inkSoft: '#7C8896',
  inkFaint: '#A6B0BC',

  // brand
  accent: '#2C8CB0',
  accentDeep: '#1E6E8C',
  segmentFrom: '#2E90B8',
  segmentTo: '#3D5A8C',

  // money
  positive: '#5C9B36',
  negative: '#D9534F',
  positiveTint: '#EDF6E7',

  // support
  line: '#DFE6EE',
  lineDotted: '#CBD5E0',
  warnBg: '#FDEBE4',
  warnInk: '#C4501F',
  chipIdle: '#FFFFFF',
  shadow: '#8FA3B8',
} as const;

export const radius = { sm: 8, md: 12, lg: 18, xl: 22, pill: 999 } as const;

export const space = (n: number) => n * 4;

export const font = {
  // Nunito is the intended brand face; the system rounded fallback keeps the
  // app installable and testable before fonts are bundled.
  h1: { fontSize: 26, fontWeight: '800' as const, letterSpacing: -0.3 },
  h2: { fontSize: 21, fontWeight: '800' as const, letterSpacing: -0.2 },
  h3: { fontSize: 17, fontWeight: '700' as const },
  title: { fontSize: 16, fontWeight: '700' as const },
  body: { fontSize: 15, fontWeight: '500' as const },
  label: { fontSize: 14, fontWeight: '600' as const },
  small: { fontSize: 12.5, fontWeight: '500' as const },
  tiny: { fontSize: 11, fontWeight: '700' as const, letterSpacing: 0.4 },
};

export const shadow = {
  card: {
    shadowColor: color.shadow,
    shadowOpacity: 0.18,
    shadowRadius: 12,
    shadowOffset: { width: 0, height: 4 },
    elevation: 3,
  },
  bar: {
    shadowColor: color.shadow,
    shadowOpacity: 0.12,
    shadowRadius: 8,
    shadowOffset: { width: 0, height: -2 },
    elevation: 8,
  },
};
