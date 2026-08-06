import { renderHook, act } from '@testing-library/react-native';
import { nextSlots, useDispatch } from './dispatch';

// Sending a vendor to somebody's home. The slot offered must be a real one —
// "Today, 14:00" printed at four in the afternoon is how a tenant ends up
// waiting in for nobody (#251).

const mockAdvance = jest.fn();
jest.mock('./worklists', () => ({ advanceTicket: (...a: unknown[]) => mockAdvance(...a) }));

describe('nextSlots', () => {
  it('offers nothing already in the past', () => {
    const at = new Date('2026-08-06T11:30:00+05:30');
    const slots = nextSlots(at);

    expect(slots.length).toBeGreaterThan(0);
    expect(slots.some((s) => s.label.includes('09:00 – 11:00') && s.label.startsWith('Today'))).toBe(false);
  });

  it('moves to tomorrow once the day’s slots have gone', () => {
    const slots = nextSlots(new Date('2026-08-06T21:00:00+05:30'));
    expect(slots.every((s) => !s.label.startsWith('Today'))).toBe(true);
  });

  it('carries an ISO start the API can store, not just a label', () => {
    const slots = nextSlots(new Date('2026-08-06T06:00:00+05:30'));
    expect(slots[0].starts_at).toMatch(/^\d{4}-\d{2}-\d{2}T/);
  });
});

describe('useDispatch', () => {
  beforeEach(() => mockAdvance.mockReset());

  it('schedules the ticket with the vendor and the slot', async () => {
    mockAdvance.mockResolvedValue(undefined);
    const { result } = await renderHook(() => useDispatch('tk-1'));

    await act(async () => { await result.current.dispatchTo('Kochi Cooling Services', '2026-08-07T09:00:00+05:30'); });

    expect(mockAdvance).toHaveBeenCalledWith('tk-1', {
      action: 'schedule', vendor: 'Kochi Cooling Services', slot: '2026-08-07T09:00:00+05:30',
    });
    expect(result.current.sent).toBe(true);
  });

  it('does not claim the vendor was told when the call failed', async () => {
    mockAdvance.mockRejectedValue(new Error('that ticket is already resolved'));
    const { result } = await renderHook(() => useDispatch('tk-1'));

    await act(async () => { await result.current.dispatchTo('Kochi Cooling', '2026-08-07T09:00:00+05:30'); });

    expect(result.current.sent).toBe(false);
    expect(result.current.error).toBe('that ticket is already resolved');
  });

  it('refuses without a vendor named', async () => {
    const { result } = await renderHook(() => useDispatch('tk-1'));

    await act(async () => { await result.current.dispatchTo('  ', '2026-08-07T09:00:00+05:30'); });

    expect(mockAdvance).not.toHaveBeenCalled();
    expect(result.current.error).toBe('Name the vendor being sent.');
  });
});
