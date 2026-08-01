import { pathFor, selectSection, deductorOptions, residencyOptions } from './tds';

// This mirrors the backend's ADR-0024 matrix for the lease builder's live
// preview only — the API is authoritative. The property that matters here is
// the same one the Go tests assert: residency decides first, and it is not a
// tie-break.

describe('selectSection', () => {
  it('puts a non-resident landlord on section 195 regardless of the deductor', () => {
    expect(selectSection('individual_no_audit', 'non_resident')).toBe('195');
    expect(selectSection('business', 'non_resident')).toBe('195');
    expect(selectSection('government', 'non_resident')).toBe('195');
  });

  it('puts an unaudited individual on section 194-IB with a resident landlord', () => {
    expect(selectSection('individual_no_audit', 'resident')).toBe('194ib');
  });

  it('puts every other resident-landlord deductor on section 194-I', () => {
    expect(selectSection('individual_audited', 'resident')).toBe('194i');
    expect(selectSection('business', 'resident')).toBe('194i');
    expect(selectSection('government', 'resident')).toBe('194i');
  });
});

describe('pathFor', () => {
  it('names the right section, threshold and forms for the common tenant case', () => {
    const p = pathFor('individual_no_audit', 'resident');
    expect(p.section).toBe('194ib');
    expect(p.needsTAN).toBe(false);
    expect(p.needsAcknowledgement).toBe(false);
    expect(p.artefacts).toContain('Form 26QC');
  });

  it('requires acknowledgement only for section 195', () => {
    expect(pathFor('individual_no_audit', 'non_resident').needsAcknowledgement).toBe(true);
    expect(pathFor('business', 'resident').needsAcknowledgement).toBe(false);
  });

  it('swaps the challan for a book entry when the deductor is government', () => {
    const p = pathFor('government', 'resident');
    expect(p.artefacts).not.toContain('Challan');
    expect(p.artefacts).toContain('Form 24G book entry');
  });

  it('leaves the challan in place for a non-government deductor on the same section', () => {
    const p = pathFor('business', 'resident');
    expect(p.artefacts).toContain('Challan');
  });

  it('carries no TAN requirement for the one path that has none', () => {
    expect(pathFor('individual_no_audit', 'resident').needsTAN).toBe(false);
    expect(pathFor('business', 'resident').needsTAN).toBe(true);
    expect(pathFor('individual_no_audit', 'non_resident').needsTAN).toBe(true);
  });
});

describe('the option lists shown on the form', () => {
  it('cover every deductor class exactly once', () => {
    const keys = deductorOptions.map((o) => o.k).sort();
    expect(keys).toEqual(
      ['business', 'government', 'individual_audited', 'individual_no_audit'].sort(),
    );
  });

  it('cover both residency values exactly once', () => {
    expect(residencyOptions.map((o) => o.k).sort()).toEqual(['non_resident', 'resident']);
  });

  it('gives every option a non-empty label and hint, since a blank one reaches the form', () => {
    for (const o of [...deductorOptions, ...residencyOptions]) {
      expect(o.label.trim().length).toBeGreaterThan(0);
      expect(o.hint.trim().length).toBeGreaterThan(0);
    }
  });
});
