import { inr, inrShort, pctOf, PLATFORM_FEE_PCT } from './money';

// The only sanctioned way to render an amount. Every app imports these
// instead of formatting paise itself, so a bug here is a bug on every screen
// that shows money at once.

describe('inr', () => {
  it('groups whole rupees in the Indian lakh/crore style', () => {
    expect(inr(4200000)).toBe('₹42,000.00');
  });

  it('groups a crore-scale amount', () => {
    expect(inr(150000000)).toBe('₹15,00,000.00');
  });

  it('does not group an amount under a thousand', () => {
    expect(inr(50000)).toBe('₹500.00');
  });

  it('keeps a negative amount as a minus sign in front of the currency mark', () => {
    expect(inr(-4200000)).toBe('−₹42,000.00');
  });

  it('signs a positive amount when asked to', () => {
    expect(inr(4200000, { sign: true })).toBe('+₹42,000.00');
  });

  it('signs a negative amount with the minus, not a double sign', () => {
    expect(inr(-4200000, { sign: true })).toBe('−₹42,000.00');
  });

  it('drops paise when asked to', () => {
    expect(inr(4200050, { noPaise: true })).toBe('₹42,000');
  });

  it('renders zero without a sign', () => {
    expect(inr(0)).toBe('₹0.00');
  });

  it('renders a sub-rupee amount with its paise', () => {
    expect(inr(150)).toBe('₹1.50');
  });
});

describe('inrShort', () => {
  it('renders crores with one decimal unless exact', () => {
    expect(inrShort(1_00_00_000 * 100)).toBe('₹1Cr');
    expect(inrShort(1_50_00_000 * 100)).toBe('₹1.5Cr');
  });

  it('renders lakhs with one decimal unless exact', () => {
    expect(inrShort(1_00_000 * 100)).toBe('₹1L');
    expect(inrShort(1_25_000 * 100)).toBe('₹1.3L');
  });

  it('renders thousands rounded to the nearest K', () => {
    expect(inrShort(4200 * 100)).toBe('₹4K');
  });

  it('renders amounts under a thousand rupees as a bare number', () => {
    expect(inrShort(50000)).toBe('₹500');
  });
});

describe('pctOf', () => {
  it('rounds to the nearest paisa rather than truncating', () => {
    // 2.99% of ₹25,000 (2,500,000 paise) is 74,750 paise exactly — the same
    // headline figure the backend fee schedule computes.
    expect(pctOf(2_500_000, PLATFORM_FEE_PCT)).toBe(74750);
  });

  it('rounds a fraction up at the half-paisa boundary', () => {
    expect(pctOf(100, 12.5)).toBe(13); // 12.5 rounds to 13, not 12
  });

  it('returns zero for a zero amount', () => {
    expect(pctOf(0, PLATFORM_FEE_PCT)).toBe(0);
  });
});
