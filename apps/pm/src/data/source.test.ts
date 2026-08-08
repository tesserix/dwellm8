import { act, renderHook, waitFor } from '@testing-library/react-native';
import { useOpsTodayData, useOpsWorklist } from './source';

// The Today screen's numbers. What matters here is what a manager is shown
// when the server does not answer: demonstration figures on a real firm's
// screen are acted on as if they were the firm's own (#284).

const mockOpsToday = jest.fn();
const mockOpsTickets = jest.fn();
const mockOpsArrears = jest.fn();
const mockOpsApprovals = jest.fn();
const mockGrantListeners = new Set<() => void>();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL
    ? {
      opsToday: mockOpsToday, opsTickets: mockOpsTickets, opsArrears: mockOpsArrears,
      opsApprovals: mockOpsApprovals, opsRegistration: jest.fn(), me: jest.fn(),
    }
    : null),
  onActingGrantChange: (fn: () => void) => {
    mockGrantListeners.add(fn);
    return () => { mockGrantListeners.delete(fn); };
  },
}));

/** What the switcher does: the open book changes under every mounted screen. */
const openAnotherBook = () => mockGrantListeners.forEach((l) => l());

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

  // A tenancy signed for next week is the one thing happening at a firm whose
  // every other figure is zero, and dropping it made the two look alike (#308).
  it('carries the tenancies that have not started yet', async () => {
    mockOpsToday.mockResolvedValue({ ...live, starting_tenancies: 2 });
    const { result } = await load();
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.startingTenancies).toBe(2);
  });

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

// A manager switches from their own books to an owner's and the screen keeps
// the figures it already had. The zeros read as an owner with nothing owed;
// the other direction puts one owner's arrears under another's name (#364).
describe('the open book changing', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockOpsToday.mockReset();
    mockOpsTickets.mockReset().mockResolvedValue([]);
    mockOpsArrears.mockReset().mockResolvedValue([]);
    mockOpsApprovals.mockReset().mockResolvedValue([]);
  });

  it('reads the roster again when another owner’s books are opened', async () => {
    mockOpsToday.mockResolvedValue(live);
    const { result } = await renderHook(() => useOpsTodayData());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.billedPaise).toBe(0);

    mockOpsToday.mockResolvedValue({
      ...live, rent_roll_amount_minor: 3200000, active_tenancies: 1,
    });
    await act(async () => openAnotherBook());

    await waitFor(() => expect(result.current.billedPaise).toBe(3200000));
    expect(result.current.activeTenancies).toBe(1);
  });

  it('reads the worklist again too, so no task is filed under the wrong owner', async () => {
    const { result } = await renderHook(() => useOpsWorklist());
    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.tasks).toHaveLength(0);

    mockOpsArrears.mockResolvedValue([{
      lease_id: 'l-1', due_amount_minor: 3200000, property: 'Nair Gardens',
      unit: '301', as_of: '2026-08-08',
    }]);
    await act(async () => openAnotherBook());

    await waitFor(() => expect(result.current.tasks).toHaveLength(1));
    expect(result.current.tasks[0].where).toBe('Nair Gardens · 301');
  });
});
