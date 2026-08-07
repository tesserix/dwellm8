import { renderHook, waitFor } from '@testing-library/react-native';
import { useArrear } from './arrear';

// One tenancy's position, for the screen a manager opens standing at a door.
// It must show what the ledger says and nothing it merely assumes: a deposit,
// a mandate or a promise to pay that was invented is worse than a blank (#251).

const mockPosition = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsPosition: mockPosition } : null),
}));

const row = {
  lease_id: 'l1', unit: 'A-302', property: 'Chandra Arcade', locality: 'Kadavanthra',
  phone: '+919847012345', rent_amount_minor: 2500000, due_amount_minor: 2500000,
  as_of: '2026-08-06',
};

describe('useArrear', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockPosition.mockReset().mockResolvedValue(row);
  });

  // The arrears list holds only tenancies in arrears, so a settled one is asked
  // about directly rather than searched for in a list it is not on (#306).
  it('asks for the one tenancy the screen was opened for', async () => {
    mockPosition.mockResolvedValue({ ...row, lease_id: 'l2', unit: 'B-101' });
    const { result } = await renderHook(() => useArrear('l2'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockPosition).toHaveBeenCalledWith('l2');
    expect(result.current.row?.unit).toBe('B-101');
  });

  it('says the tenancy is square when nothing is owed', async () => {
    mockPosition.mockResolvedValue({ ...row, due_amount_minor: 0 });
    const { result } = await renderHook(() => useArrear('l1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.owes).toBe(false);
    expect(result.current.error).toBeUndefined();
  });

  it('reports a tenancy the firm does not hold rather than showing another', async () => {
    mockPosition.mockRejectedValue(new Error('no such tenancy'));
    const { result } = await renderHook(() => useArrear('gone'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.row).toBeUndefined();
    expect(result.current.error).toBe('no such tenancy');
  });

  it('asks for nothing without a tenancy to ask about', async () => {
    const { result } = await renderHook(() => useArrear(undefined));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockPosition).not.toHaveBeenCalled();
  });
});
