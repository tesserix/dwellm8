import { e164, isE164 } from './phone';

// A manager reads a mobile off a phone as ten digits, and the schema's CHECK
// wants E.164. Whoever bridges that, it is not the manager (#360).
describe('e164', () => {
  it('gives a bare Indian ten-digit mobile its country code', () => {
    expect(e164('9845012345')).toBe('+919845012345');
  });

  it('reads a number written the way it is spoken', () => {
    expect(e164('98450 12345')).toBe('+919845012345');
    expect(e164('+91 98450 12345')).toBe('+919845012345');
    expect(e164('091-98450-12345')).toBe('+919845012345');
  });

  it('leaves a number from anywhere else alone', () => {
    expect(e164('+442071838750')).toBe('+442071838750');
  });

  it('does not invent a country code for something that is not a number', () => {
    expect(e164('98450')).toBe('98450');
    expect(e164('')).toBe('');
  });
});

describe('isE164', () => {
  it('agrees with the shape the schema enforces', () => {
    expect(isE164('+919845012345')).toBe(true);
    expect(isE164('9845012345')).toBe(false);
    expect(isE164('+0919845012345')).toBe(false);
  });
});
