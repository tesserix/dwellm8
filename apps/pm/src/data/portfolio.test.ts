import { renderHook, waitFor, act } from '@testing-library/react-native';
import { usePortfolio } from './portfolio';

// The properties under this scope. A record made one screen away has to be
// here when the manager comes back, so the read is repeatable (#289).

const mockOpsProperties = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsProperties: mockOpsProperties } : null),
}));

const one = [{ id: 'p1', code: 'KVH', name: 'Kadavanthra Heights', kind: 'building',
  address_line1: '18 Chandra Nagar Road', locality: 'Kadavanthra', city: 'Kochi', unit_count: 3 }];

describe('usePortfolio', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockOpsProperties.mockReset();
  });

  it('reads the properties under this scope', async () => {
    mockOpsProperties.mockResolvedValue(one);
    const { result } = await renderHook(() => usePortfolio());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.rows).toHaveLength(1);
    expect(result.current.units).toBe(3);
    expect(result.current.error).toBeUndefined();
  });

  it('reads again on reload, so a new property is there without a relaunch', async () => {
    mockOpsProperties.mockResolvedValueOnce([]).mockResolvedValueOnce(one);
    const { result } = await renderHook(() => usePortfolio());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.rows).toEqual([]);

    await act(async () => { result.current.reload(); });
    await waitFor(() => expect(result.current.rows).toHaveLength(1));
  });

  it('says why it is empty when the read fails, and lists nothing', async () => {
    mockOpsProperties.mockRejectedValue(new Error('sign in again'));
    const { result } = await renderHook(() => usePortfolio());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('sign in again');
    expect(result.current.rows).toEqual([]);
  });

  it('says so rather than listing nothing when the build has no API', async () => {
    delete process.env.EXPO_PUBLIC_API_URL;
    const { result } = await renderHook(() => usePortfolio());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('The API is not configured on this build.');
    expect(mockOpsProperties).not.toHaveBeenCalled();
  });
});
