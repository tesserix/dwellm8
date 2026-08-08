import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useNearby } from './nearby';

// What is near a property (#354). A renter asks "which school, how far" before
// they ask the rent, so the list is grouped by kind and nearest first.

const mockPlaces = jest.fn();
const mockAdd = jest.fn();
const mockRetire = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsPlaces: mockPlaces,
    opsAddPlace: mockAdd,
    opsRetirePlace: mockRetire,
  }),
}));

beforeEach(() => {
  mockPlaces.mockReset().mockResolvedValue([
    { id: 'p1', category: 'school', name: 'Spotswood Primary', distance_m: 600, travel_mode: 'walk', tags: ['government', 'primary'] },
    { id: 'p2', category: 'school', name: 'Bayside College', distance_m: 3200, travel_mode: 'drive', tags: [] },
    { id: 'p3', category: 'metro', name: 'Indiranagar', distance_m: 900, travel_mode: 'walk', tags: [] },
  ]);
  mockAdd.mockReset().mockResolvedValue({ place: { id: 'p4' } });
  mockRetire.mockReset().mockResolvedValue(undefined);
});

it('groups what is nearby by kind, nearest first', async () => {
  const { result } = await renderHook(() => useNearby('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  expect(result.current.groups.map((g) => g.category)).toEqual(['school', 'metro']);
  expect(result.current.groups[0].places.map((p) => p.name))
    .toEqual(['Spotswood Primary', 'Bayside College']);
});

it('says a walk in minutes, because that is how far feels', async () => {
  const { result } = await renderHook(() => useNearby('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.groups[0].places[0].away).toBe('600 m · 8 min walk');
  expect(result.current.groups[0].places[1].away).toBe('3.2 km · drive');
});

it('adding a school records it and re-reads the list', async () => {
  const { result } = await renderHook(() => useNearby('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => {
    await result.current.add({ category: 'school', name: 'Corner High', distance_m: 400 });
  });
  expect(mockAdd).toHaveBeenCalledWith('prop-1',
    expect.objectContaining({ category: 'school', name: 'Corner High', distance_m: 400 }));
  expect(mockPlaces).toHaveBeenCalledTimes(2);
});

it('refuses a place with no distance before the server has to', async () => {
  const { result } = await renderHook(() => useNearby('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await expect(result.current.add({ category: 'school', name: 'Corner High', distance_m: 0 }))
    .rejects.toThrow(/how far/i);
  expect(mockAdd).not.toHaveBeenCalled();
});

it('removing a place retires it and re-reads the list', async () => {
  const { result } = await renderHook(() => useNearby('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { await result.current.remove('p1'); });
  expect(mockRetire).toHaveBeenCalledWith('p1');
  expect(mockPlaces).toHaveBeenCalledTimes(2);
});
