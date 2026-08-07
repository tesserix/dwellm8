import { renderHook, waitFor } from '@testing-library/react-native';
import { usePropertyRecord } from './property';

// The record a manager opens on site. A building whose units cannot be read
// is a screen full of nothing that says why, never a borrowed one (#251).

const mockOpsProperty = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsProperty: mockOpsProperty } : null),
}));

const record = {
  property: { id: 'p1', code: 'MR', name: 'Menon Residency', kind: 'building',
    address_line1: '12 MG Road', locality: 'Kadavanthra', city: 'Kochi', unit_count: 2 },
  units: [
    { id: 'u1', code: '101', kind: 'flat', floor: 1, occupancy: 'occupied',
      lease_id: 'l1', tenant: 'Priya Nair', rent_amount_minor: 2500000,
      lease_ends: '2026-12-31', due_amount_minor: 2500000 },
    { id: 'u2', code: '102', kind: 'flat', floor: 1, occupancy: 'vacant' },
  ],
};

describe('usePropertyRecord', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockOpsProperty.mockReset();
  });

  it('reads the property and its units for the id it was given', async () => {
    mockOpsProperty.mockResolvedValue(record);
    const { result } = await renderHook(() => usePropertyRecord('p1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockOpsProperty).toHaveBeenCalledWith('p1');
    expect(result.current.property?.name).toBe('Menon Residency');
    expect(result.current.units).toHaveLength(2);
    expect(result.current.error).toBeUndefined();
  });

  it('counts what is let and what is empty, because that is the glance', async () => {
    mockOpsProperty.mockResolvedValue(record);
    const { result } = await renderHook(() => usePropertyRecord('p1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.occupied).toBe(1);
    expect(result.current.vacant).toBe(1);
    expect(result.current.rentRollPaise).toBe(2500000);
  });

  // A flat handed over next month is neither income today nor free to offer.
  it('keeps a unit let from a future date out of the let count and the rent roll', async () => {
    mockOpsProperty.mockResolvedValue({
      ...record,
      units: [
        record.units[0],
        { id: 'u2', code: '102', kind: 'flat', floor: 1, occupancy: 'vacant',
          lease_id: 'l2', tenant: 'Arun Menon', rent_amount_minor: 3000000,
          let_from: '2027-03-01' },
      ],
    });
    const { result } = await renderHook(() => usePropertyRecord('p1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.occupied).toBe(1);
    expect(result.current.vacant).toBe(1);
    expect(result.current.spokenFor).toBe(1);
    expect(result.current.rentRollPaise).toBe(2500000);
  });

  it('says why it is empty when the read fails, and shows no unit at all', async () => {
    mockOpsProperty.mockRejectedValue(new Error('sign in again'));
    const { result } = await renderHook(() => usePropertyRecord('p1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('sign in again');
    expect(result.current.units).toEqual([]);
    expect(result.current.property).toBeUndefined();
  });

  it('asks for nothing without an id, rather than for every property', async () => {
    const { result } = await renderHook(() => usePropertyRecord(undefined));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockOpsProperty).not.toHaveBeenCalled();
    expect(result.current.error).toBe('No property was named.');
  });
});
