import { istDate } from './clock';

// The date above live money has to be today's, in the timezone the rent is
// due in — a fixed string from the demonstration set reads as stale (#291).

describe('istDate', () => {
  it('reads as a manager reads a diary', () => {
    expect(istDate(new Date('2026-08-07T06:30:00Z'))).toBe('Friday, 7 August 2026');
  });

  it('is the Indian day, not UTC, late in the evening', () => {
    // 22:00 UTC on the 6th is already 03:30 on the 7th in Kolkata.
    expect(istDate(new Date('2026-08-06T22:00:00Z'))).toBe('Friday, 7 August 2026');
  });

  it('is the Indian day, not UTC, early in the morning', () => {
    expect(istDate(new Date('2026-08-07T01:00:00Z'))).toBe('Friday, 7 August 2026');
  });
});
