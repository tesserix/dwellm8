import { renderHook, waitFor } from '@testing-library/react-native';
import {
  atLocalTime, fmtOccurrence, seriesInWords, seriesShape, seriesWhen,
  useListingViewings, useOpsListings,
} from './viewings';

// The manager's viewing times (#333). A series has to read back as the sentence
// the manager said out loud, and the screen shows the occurrences it produced.

const mockOpsListings = jest.fn();
const mockListingSchedules = jest.fn();
const mockListingSlots = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? {
    opsListings: mockOpsListings,
    listingSchedules: mockListingSchedules,
    opsListingSlots: mockListingSlots,
  } : null),
}));

const saturdays = {
  id: 's1', listing_id: 'l1', weekdays: [6], start_time: '10:00', zone: 'Asia/Kolkata',
  duration_mins: 30, capacity: 4, starts_on: '2026-08-08', state: 'active',
};

describe('seriesInWords', () => {
  it('names the day, the time and how many people fit', () => {
    expect(seriesInWords(saturdays)).toBe('Saturdays at 10:00 · 30 min · up to 4 people');
  });

  it('lists several days in week order, whatever order they were given', () => {
    expect(seriesInWords({ ...saturdays, weekdays: [3, 0] }))
      .toBe('Sundays and Wednesdays at 10:00 · 30 min · up to 4 people');
  });

  it('reads three or more days as a list', () => {
    expect(seriesInWords({ ...saturdays, weekdays: [1, 3, 5] }))
      .toBe('Mondays, Wednesdays and Fridays at 10:00 · 30 min · up to 4 people');
  });

  it('says when a series has an end, because an open one has none', () => {
    expect(seriesInWords({ ...saturdays, ends_on: '2026-09-30' }))
      .toBe('Saturdays at 10:00 · 30 min · up to 4 people · until 30 Sep 2026');
  });

  it('says a series has stopped rather than showing it as running', () => {
    expect(seriesInWords({ ...saturdays, state: 'ended' }))
      .toBe('Ended — Saturdays at 10:00');
  });

  // The row gives the sentence one line, so the shape of the viewing goes on
  // its own line rather than off the end of the screen (#336).
  it('splits into when it repeats and what shape the viewing is', () => {
    expect(seriesWhen(saturdays)).toBe('Saturdays at 10:00');
    expect(seriesShape({ ...saturdays, ends_on: '2026-09-30' }))
      .toBe('30 min · up to 4 people · until 30 Sep 2026');
  });

  it('has no shape to state once the series has stopped', () => {
    expect(seriesShape({ ...saturdays, state: 'ended' })).toBe('');
  });
});

// A viewing happens at the property. The hour on the manager's screen must be
// the hour the person at the door arrives, whatever zone the phone is in (#334).
describe('fmtOccurrence', () => {
  it('reads the time on the property’s clock, not the phone’s', () => {
    expect(fmtOccurrence('2026-08-08T04:30:00Z', 'Asia/Kolkata')).toBe('Sat 08 Aug · 10:00 am');
  });

  it('holds the wall clock across a change of the clocks', () => {
    // 10:00 New York is 14:00 UTC in summer and 15:00 UTC in winter.
    expect(fmtOccurrence('2026-08-08T14:00:00Z', 'America/New_York')).toBe('Sat 08 Aug · 10:00 am');
    expect(fmtOccurrence('2026-12-05T15:00:00Z', 'America/New_York')).toBe('Sat 05 Dec · 10:00 am');
  });
});

describe('atLocalTime', () => {
  it('moves a viewing to that time on the property’s clock, keeping its day', () => {
    expect(atLocalTime('2026-08-08T04:30:00Z', '14:00', 'Asia/Kolkata'))
      .toBe('2026-08-08T08:30:00.000Z');
  });

  it('refuses a time that is not a clock', () => {
    expect(() => atLocalTime('2026-08-08T04:30:00Z', 'later', 'Asia/Kolkata')).toThrow();
  });
});

describe('useOpsListings', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockOpsListings.mockReset();
  });

  it('reads the firm’s listings', async () => {
    mockOpsListings.mockResolvedValue([{ id: 'l1', state: 'live', headline: 'Two-bed in Kadavanthra' }]);
    const { result } = await renderHook(() => useOpsListings());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.data).toHaveLength(1);
  });

  it('says why the screen is empty when the read fails', async () => {
    mockOpsListings.mockRejectedValue(new Error('the network is out'));
    const { result } = await renderHook(() => useOpsListings());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('the network is out');
    expect(result.current.data).toEqual([]);
  });
});

describe('useListingViewings', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockListingSchedules.mockReset();
    mockListingSlots.mockReset();
  });

  it('reads the series and the times they produced together', async () => {
    mockListingSchedules.mockResolvedValue([saturdays]);
    mockListingSlots.mockResolvedValue([
      { id: 'v1', starts_at: '2099-08-08T04:30:00Z', duration_mins: 30, remaining: 4, state: 'open' },
    ]);
    const { result } = await renderHook(() => useListingViewings('l1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockListingSchedules).toHaveBeenCalledWith('l1');
    expect(result.current.schedules).toHaveLength(1);
    expect(result.current.slots).toHaveLength(1);
  });

  it('shows only the times still to come, in order', async () => {
    mockListingSchedules.mockResolvedValue([]);
    mockListingSlots.mockResolvedValue([
      { id: 'later', starts_at: '2099-08-15T04:30:00Z', state: 'open' },
      { id: 'past', starts_at: '2020-01-01T04:30:00Z', state: 'open' },
      { id: 'sooner', starts_at: '2099-08-08T04:30:00Z', state: 'open' },
    ]);
    const { result } = await renderHook(() => useListingViewings('l1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.slots.map((s) => s.id)).toEqual(['sooner', 'later']);
  });

  it('asks for nothing when no listing was named', async () => {
    const { result } = await renderHook(() => useListingViewings(undefined));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockListingSchedules).not.toHaveBeenCalled();
    expect(result.current.error).toBe('No listing was named.');
  });
});
