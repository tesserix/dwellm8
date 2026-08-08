import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useBeds } from './beds';

// The bed board (#299). What the warden sees must be what somebody is billed
// for, so it comes from the register and never from the app.

const mockBeds = jest.fn();
const mockAllocate = jest.fn();
const mockAddBed = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({ opsBeds: mockBeds, opsAllocateBed: mockAllocate, opsAddBed: mockAddBed }),
}));

beforeEach(() => {
  mockBeds.mockReset().mockResolvedValue([
    { id: 'b1', label: '09A', room: '09', floor: 1, sharing: 2, rent_amount_minor: 1200000, state: 'occupied', lease_id: 'l1' },
    { id: 'b2', label: '09B', room: '09', floor: 1, sharing: 2, rent_amount_minor: 1200000, state: 'vacant' },
    { id: 'b3', label: '10A', room: '10', floor: 2, sharing: 3, rent_amount_minor: 1000000, state: 'notice', lease_id: 'l2' },
  ]);
  mockAllocate.mockReset().mockResolvedValue({});
  mockAddBed.mockReset().mockResolvedValue({ bed: { id: 'b4', label: '09C' } });
});

it('counts what is taken and what is free', async () => {
  const { result } = await renderHook(() => useBeds('p1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.beds).toHaveLength(3);
  expect(result.current.occupied).toBe(1);
  expect(result.current.vacant).toBe(1);
  expect(result.current.onNotice).toBe(1);
});

it('groups the board by floor, the way a warden walks it', async () => {
  const { result } = await renderHook(() => useBeds('p1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.floors.map((f) => f.floor)).toEqual([1, 2]);
  expect(result.current.floors[0].beds.map((b) => b.label)).toEqual(['09A', '09B']);
});

it('vacating a bed asks the register and reloads the board', async () => {
  const { result } = await renderHook(() => useBeds('p1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(async () => { await result.current.allocate('b1', 'vacant'); });
  expect(mockAllocate).toHaveBeenCalledWith('b1', 'vacant', undefined);
  expect(mockBeds).toHaveBeenCalledTimes(2);
});

it('refuses to occupy a bed without the tenancy that pays for it', async () => {
  const { result } = await renderHook(() => useBeds('p1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  await expect(result.current.allocate('b2', 'occupied')).rejects.toThrow(/tenancy/i);
  expect(mockAllocate).not.toHaveBeenCalled();
});

// A board with nothing on it is the whole of a new PG, and until now there was
// no way to put the first bed there (#367).
it('puts a bed in a room and reads the board again', async () => {
  const { result } = await renderHook(() => useBeds('p1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  await act(async () => { await result.current.add('u1', '09C', 1200000); });
  expect(mockAddBed).toHaveBeenCalledWith('u1', '09C', 1200000);
  expect(mockBeds).toHaveBeenCalledTimes(2);
});

it('refuses a bed with no label, since that is what it is allocated by', async () => {
  const { result } = await renderHook(() => useBeds('p1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  await expect(result.current.add('u1', '  ', 1200000)).rejects.toThrow(/label/i);
  expect(mockAddBed).not.toHaveBeenCalled();
});
