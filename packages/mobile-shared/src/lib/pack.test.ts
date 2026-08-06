import { monthLabel, describeHistory, packReadiness } from './pack';
import type { AddressHistory, ApplicantPack } from './api';

describe('the applicant pack, as the manager reads it (#259)', () => {
  it('names a month the way a person says it', () => {
    expect(monthLabel('2023-03')).toBe('Mar 2023');
    expect(monthLabel('')).toBe('now');
  });

  it('says the five years hold together when they do', () => {
    const h: AddressHistory = { addresses: [], gaps: [], complete: true };
    expect(describeHistory(h)).toBe('Five years covered');
  });

  it('names the single hole rather than counting it', () => {
    const h: AddressHistory = {
      addresses: [],
      gaps: [{ from: '2023-03', to: '2023-06' }],
      complete: false,
    };
    expect(describeHistory(h)).toBe('1 gap — Mar 2023 to Jun 2023');
  });

  it('counts the holes once there are several', () => {
    const h: AddressHistory = {
      addresses: [],
      gaps: [{ from: '2023-03', to: '2023-06' }, { from: '2021-01', to: '2021-04' }],
      complete: false,
    };
    expect(describeHistory(h)).toBe('2 gaps in the five years');
  });

  it('lists what is still missing before the pack can be submitted', () => {
    const pack = {
      id: 'p1', state: 'draft', full_name: '', occupants: 1,
      people: [], address_history_complete: false, address_history_gaps: 1,
    } as unknown as ApplicantPack;
    expect(packReadiness(pack)).toEqual(['Applicant name', 'Address history']);
  });

  it('is ready when the name is in and the years add up', () => {
    const pack = {
      id: 'p1', state: 'draft', full_name: 'Meera Menon', occupants: 2,
      people: [], address_history_complete: true, address_history_gaps: 0,
    } as unknown as ApplicantPack;
    expect(packReadiness(pack)).toEqual([]);
  });
});
