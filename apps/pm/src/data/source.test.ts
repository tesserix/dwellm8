import { act, renderHook, waitFor } from '@testing-library/react-native';
import { useOpsTodayData } from './source';

// The Today screen's numbers. What matters here is what a manager is shown
// when the server does not answer: demonstration figures on a real firm's
// screen are acted on as if they were the firm's own (#284).

const mockOpsToday = jest.fn();
const mockOpsTickets = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL
    ? { opsToday: mockOpsToday, opsTickets: mockOpsTickets, opsRegistration: jest.fn(), me: jest.fn() }
    : null),
}));

const live = {
  as_of: '2026-08-07', active_tenancies: 0, rent_roll_amount_minor: 0,
  outstanding_amount_minor: 0, tenancies_in_arrears: 0,
};

describe('useOpsTodayData', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockOpsToday.mockReset();
    mockOpsTickets.mockReset().mockResolvedValue([]);
  });

  const load = async () => renderHook(() => useOpsTodayData());

  it('shows nothing rather than demonstration money while the first load is in flight', async () => {
    mockOpsToday.mockReturnValue(new Promise(() => {}));
    const { result } = await load();
    expect(result.current.loading).toBe(true);
    expect(result.current.billedPaise).toBe(0);
    expect(result.current.outstandingPaise).toBe(0);
  });

  it('keeps no demonstration figure when the live load fails', async () => {
    mockOpsToday.mockRejectedValue(new Error('sign in again'));
    const { result } = await load();
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('sign in again');
    expect(result.current.billedPaise).toBe(0);
    expect(result.current.arrearsCount).toBe(0);
  });

  it('reports a real but empty organisation as empty, not as an error', async () => {
    mockOpsToday.mockResolvedValue(live);
    const { result } = await load();
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBeUndefined();
    expect(result.current.billedPaise).toBe(0);
    expect(result.current.activeTenancies).toBe(0);
  });

  it('carries the server figures through when there are some', async () => {
    mockOpsToday.mockResolvedValue({ ...live, rent_roll_amount_minor: 4500000, active_tenancies: 3 });
    const { result } = await load();
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.billedPaise).toBe(4500000);
    expect(result.current.activeTenancies).toBe(3);
  });

  it('falls back to the demonstration figures only when no API is configured', async () => {
    delete process.env.EXPO_PUBLIC_API_URL;
    const { result } = await load();
    expect(result.current.mode).toBe('demo');
    expect(result.current.billedPaise).toBeGreaterThan(0);
  });
});

describe('useOpsTodayData — recovering', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockOpsToday.mockReset();
    mockOpsTickets.mockReset().mockResolvedValue([]);
  });

  it('retries on demand, so an outage is not a relaunch', async () => {
    mockOpsToday.mockRejectedValueOnce(new Error('sign in again'))
      .mockResolvedValueOnce({ ...live, rent_roll_amount_minor: 990000 });

    const { result } = await renderHook(() => useOpsTodayData());
    await waitFor(() => expect(result.current.error).toBe('sign in again'));

    await act(async () => result.current.reload());
    await waitFor(() => expect(result.current.billedPaise).toBe(990000));
    expect(result.current.error).toBeUndefined();
  });
});
