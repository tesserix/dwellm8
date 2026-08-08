import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useRota } from './rota';

// One colleague's working week (#353). It is edited as a week — what is saved
// is what the week becomes — so the hook holds a draft until it is sent.

const mockRota = jest.fn();
const mockSetRota = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({ opsRota: mockRota, opsSetRota: mockSetRota }),
}));

beforeEach(() => {
  mockRota.mockReset().mockResolvedValue([
    { weekday: 1, starts_at: '09:00', ends_at: '18:00' },
    { weekday: 3, starts_at: '09:00', ends_at: '13:00' },
  ]);
  mockSetRota.mockReset().mockResolvedValue({});
});

it('lays the week out Monday to Sunday, marking the days off', async () => {
  const { result } = await renderHook(() => useRota('s1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  expect(result.current.week).toHaveLength(7);
  expect(result.current.week[0]).toMatchObject({ weekday: 1, day: 'Monday', working: true, starts_at: '09:00' });
  expect(result.current.week[1]).toMatchObject({ weekday: 2, day: 'Tuesday', working: false });
  expect(result.current.hours).toBe(13);
});

it('turning a day on gives it the firm’s usual hours until they are changed', async () => {
  const { result } = await renderHook(() => useRota('s1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { result.current.toggle(2); });
  expect(result.current.week[1]).toMatchObject({ working: true, starts_at: '09:00', ends_at: '18:00' });
  expect(result.current.dirty).toBe(true);
});

it('refuses a shift that ends before it starts, rather than sending it', async () => {
  const { result } = await renderHook(() => useRota('s1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { result.current.setHours(1, '18:00', '09:00'); });
  await expect(result.current.save()).rejects.toThrow(/must end after it starts/i);
  expect(mockSetRota).not.toHaveBeenCalled();
});

it('refuses a time that is not a time of day', async () => {
  const { result } = await renderHook(() => useRota('s1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { result.current.setHours(1, '9am', '18:00'); });
  await expect(result.current.save()).rejects.toThrow(/HH:MM/);
});

it('saves only the days that are worked', async () => {
  const { result } = await renderHook(() => useRota('s1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { result.current.toggle(3); });
  await act(async () => { await result.current.save(); });

  expect(mockSetRota).toHaveBeenCalledWith('s1', [
    { weekday: 1, starts_at: '09:00', ends_at: '18:00' },
  ]);
  expect(result.current.dirty).toBe(false);
});

it('a colleague who works no days saves an empty week rather than nothing', async () => {
  mockRota.mockResolvedValue([]);
  const { result } = await renderHook(() => useRota('s1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { await result.current.save(); });
  expect(mockSetRota).toHaveBeenCalledWith('s1', []);
});
