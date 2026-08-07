import { renderHook, waitFor } from '@testing-library/react-native';
import { useReminders } from './reminders';

// What is about to happen, grouped by the building it happens in (#337). A
// manager reads this standing in a corridor, so the order is the order things
// happen and the grouping is where they happen.

const mockReminders = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsReminders: mockReminders } : null),
}));

const due = {
  kind: 'rent_due', lease_id: 'l1', property_id: 'p1', property: 'Chandra Arcade',
  unit: 'A-302', locality: 'Kadavanthra', on: '2026-09-05', days_away: 29,
  amount_minor: 2500000,
};
const overdue = {
  kind: 'rent_overdue', lease_id: 'l2', property_id: 'p1', property: 'Chandra Arcade',
  unit: 'A-303', locality: 'Kadavanthra', on: '2026-08-07', days_away: 0,
  amount_minor: 4000000,
};
const ending = {
  kind: 'tenancy_ending', lease_id: 'l3', property_id: 'p2', property: 'Marine Court',
  unit: 'B-101', on: '2026-08-31', days_away: 24, inside_notice_window: true,
};

describe('useReminders', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockReminders.mockReset().mockResolvedValue([overdue, ending, due]);
  });

  it('groups what is coming by the property it is coming on', async () => {
    const { result } = await renderHook(() => useReminders());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.properties.map((p) => p.name)).toEqual(['Chandra Arcade', 'Marine Court']);
    expect(result.current.properties[0].reminders).toHaveLength(2);
  });

  // A property whose next event is tomorrow is read before one whose next event
  // is next month, whatever order the API returned them in.
  it('leads with the building something happens in soonest', async () => {
    mockReminders.mockResolvedValue([due, ending, overdue]);
    const { result } = await renderHook(() => useReminders());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.properties[0].name).toBe('Chandra Arcade');
    expect(result.current.properties[0].reminders[0].kind).toBe('rent_overdue');
  });

  it('totals the rent about to fall due and the rent already late', async () => {
    const { result } = await renderHook(() => useReminders());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.duePaise).toBe(2500000);
    expect(result.current.overduePaise).toBe(4000000);
    expect(result.current.endingCount).toBe(1);
  });

  it('reports a failure rather than an empty book', async () => {
    mockReminders.mockRejectedValue(new Error('could not read the reminders'));
    const { result } = await renderHook(() => useReminders());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('could not read the reminders');
    expect(result.current.properties).toEqual([]);
  });

  it('asks over the window it was given', async () => {
    const { result } = await renderHook(() => useReminders(60));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockReminders).toHaveBeenCalledWith(60);
  });
});
