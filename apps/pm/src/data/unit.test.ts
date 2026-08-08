import { act, renderHook, waitFor } from '@testing-library/react-native';
import { useUnitRecord } from './unit';

// The flat a manager opens from the property record (#338). Bedrooms come from
// the advert because the register has no such column, and a flat that was never
// advertised must read as "not stated" rather than as a studio.

const mockUnit = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsUnit: mockUnit } : null),
}));

const record = {
  unit: {
    id: 'u1', code: '101', kind: 'flat', floor: 1, occupancy: 'occupied',
    carpet_area_sqft: 620, builtup_area_sqft: 890, electricity_consumer_no: 'KSEB-4471',
  },
  property: { id: 'p1', name: 'Chandra Arcade', locality: 'Kadavanthra', city: 'Kochi' },
  tenancy: {
    lease_id: 'l1', state: 'active', tenant: 'Priya', rent_amount_minor: 2500000,
    due_amount_minor: 2500000, ends: '2026-12-31',
  },
  listing: { id: 'ls1', state: 'live', bedrooms: 2, rent_amount_minor: 2600000 },
  ancillaries: [{ id: 'a1', code: 'P-1', kind: 'parking' }],
};

describe('useUnitRecord', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockUnit.mockReset().mockResolvedValue(record);
  });

  it('reads the flat it was asked for', async () => {
    const { result } = await renderHook(() => useUnitRecord('u1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockUnit).toHaveBeenCalledWith('u1');
    expect(result.current.unit?.code).toBe('101');
    expect(result.current.property?.name).toBe('Chandra Arcade');
  });

  it('takes the bedroom count from the advert', async () => {
    const { result } = await renderHook(() => useUnitRecord('u1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.bedrooms).toBe(2);
  });

  // A flat nobody advertised has no bedroom count anywhere in the system, and
  // saying nothing is the only honest answer.
  it('leaves the bedroom count unstated when the flat was never advertised', async () => {
    mockUnit.mockResolvedValue({ ...record, listing: undefined });
    const { result } = await renderHook(() => useUnitRecord('u1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.bedrooms).toBeUndefined();
  });

  it('reports a failure rather than an empty flat', async () => {
    mockUnit.mockRejectedValue(new Error('no such unit'));
    const { result } = await renderHook(() => useUnitRecord('u1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('no such unit');
    expect(result.current.unit).toBeUndefined();
  });

  it('asks for nothing when no unit was named', async () => {
    const { result } = await renderHook(() => useUnitRecord(undefined));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockUnit).not.toHaveBeenCalled();
    expect(result.current.error).toBeTruthy();
  });
});

// A manager writes the description, saves it, and comes back to the flat. The
// screen it returns to is the one that says whether the work landed (#366).
describe('useUnitRecord — coming back to the flat', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockUnit.mockReset();
  });

  it('reads the flat again on demand, so a saved description is on it', async () => {
    mockUnit.mockResolvedValue({ ...record, listing: undefined });
    const { result } = await renderHook(() => useUnitRecord('u1'));
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.listing).toBeUndefined();

    mockUnit.mockResolvedValue(record);
    await act(async () => result.current.reload());

    await waitFor(() => expect(result.current.listing?.bedrooms).toBe(2));
  });
});
