import { renderHook, waitFor, act } from '@testing-library/react-native';
import { usePropertyDescription, useUnitDescription, featureVocabulary } from './describe';

// What the manager writes about a building and about a flat (#354). It is read
// back from the record, never held only in the screen.

const mockProperty = jest.fn();
const mockDescribeProperty = jest.fn();
const mockUnit = jest.fn();
const mockDescribeUnit = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsProperty: mockProperty,
    opsDescribeProperty: mockDescribeProperty,
    opsUnit: mockUnit,
    opsDescribeUnit: mockDescribeUnit,
  }),
}));

beforeEach(() => {
  mockProperty.mockReset().mockResolvedValue({
    property: {
      id: 'prop-1', code: 'P1', name: 'Kadavanthra Heights', kind: 'apartment',
      address_line1: '', locality: '', city: '', unit_count: 4,
      about: 'A quiet block.', amenities: ['lift'],
    },
    units: [],
  });
  mockDescribeProperty.mockReset().mockResolvedValue({});
  mockUnit.mockReset().mockResolvedValue({
    unit: {
      id: 'u1', code: '3B', kind: 'flat', floor: 3, occupancy: 'family',
      about: 'Over the park.', features: ['wardrobes'], bathrooms: 2,
      facing: 'north', furnishing: 'semi_furnished',
    },
    property: { id: 'prop-1', name: 'Kadavanthra Heights' },
    ancillaries: [],
  });
  mockDescribeUnit.mockReset().mockResolvedValue({});
});

it('reads back what was already written about the building', async () => {
  const { result } = await renderHook(() => usePropertyDescription('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.about).toBe('A quiet block.');
  expect(result.current.amenities).toEqual(['lift']);
  expect(result.current.name).toBe('Kadavanthra Heights');
});

it('saving the building sends what is on the form', async () => {
  const { result } = await renderHook(() => usePropertyDescription('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => {
    result.current.setAbout('Set back from the road.');
    result.current.toggle('gym');
  });
  await act(async () => { await result.current.save(); });

  expect(mockDescribeProperty).toHaveBeenCalledWith('prop-1', 'Set back from the road.',
    ['lift', 'gym']);
});

it('an amenity tapped twice is turned off again', async () => {
  const { result } = await renderHook(() => usePropertyDescription('prop-1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { result.current.toggle('lift'); });
  expect(result.current.amenities).toEqual([]);
});

it('reads back what was already written about the flat', async () => {
  const { result } = await renderHook(() => useUnitDescription('u1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.about).toBe('Over the park.');
  expect(result.current.bathrooms).toBe('2');
  expect(result.current.furnishing).toBe('semi_furnished');
  expect(result.current.name).toBe('3B');
});

it('saving the flat sends the counts as numbers, and nothing for a blank one', async () => {
  const { result } = await renderHook(() => useUnitDescription('u1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => {
    result.current.setBathrooms('3');
    result.current.setBalconies('');
    result.current.setFacing('east');
  });
  await act(async () => { await result.current.save(); });

  expect(mockDescribeUnit).toHaveBeenCalledWith('u1', expect.objectContaining({
    about: 'Over the park.',
    features: ['wardrobes'],
    bathrooms: 3,
    facing: 'east',
    furnishing: 'semi_furnished',
  }));
  expect(mockDescribeUnit.mock.calls[0][1].balconies).toBeUndefined();
});

// A flat nobody has told us about is not on the ground floor (#358).
it('reads the floor back, and sends the one the manager enters', async () => {
  const { result } = await renderHook(() => useUnitDescription('u1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.floor).toBe('3');

  await act(async () => { result.current.setFloor('0'); });
  await act(async () => { await result.current.save(); });

  expect(mockDescribeUnit.mock.calls[0][1].floor).toBe(0);
});

it('a floor left blank is sent as unrecorded, not as the ground floor', async () => {
  mockUnit.mockResolvedValue({
    unit: { id: 'u1', code: '3B', kind: 'flat', occupancy: 'vacant' },
    property: { id: 'prop-1', name: 'Kadavanthra Heights' },
    ancillaries: [],
  });
  const { result } = await renderHook(() => useUnitDescription('u1'));
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.floor).toBe('');

  await act(async () => { await result.current.save(); });
  expect(mockDescribeUnit.mock.calls[0][1].floor).toBeUndefined();
});

// A basement is a floor; the counts beside it are not allowed to be negative.
it('takes a basement floor and still refuses a negative count', async () => {
  const { result } = await renderHook(() => useUnitDescription('u1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { result.current.setFloor('-1'); });
  await act(async () => { await result.current.save(); });
  expect(mockDescribeUnit.mock.calls[0][1].floor).toBe(-1);

  await act(async () => { result.current.setBathrooms('-1'); });
  await expect(result.current.save()).rejects.toThrow(/number/i);
});

it('a count that is not a number is refused before the server sees it', async () => {
  const { result } = await renderHook(() => useUnitDescription('u1'));
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { result.current.setBathrooms('two'); });
  await expect(result.current.save()).rejects.toThrow(/number/i);
  expect(mockDescribeUnit).not.toHaveBeenCalled();
});

// Every chip the screen offers has to be a value units_features_known accepts,
// or the manager taps it and the save comes back 422 (#354).
it('offers only the features the record holds', () => {
  expect([...featureVocabulary].sort()).toEqual([
    'air_conditioning', 'balcony_covered', 'beds', 'chimney', 'dining_table',
    'false_ceiling', 'garden', 'geyser', 'internet', 'inverter',
    'microwave', 'modular_kitchen', 'pet_friendly', 'piped_gas',
    'pooja_room', 'private_terrace', 'refrigerator', 'servant_room', 'sofa',
    'store_room', 'study', 'wardrobes', 'washing_machine', 'water_purifier',
    'wheelchair_access',
  ].sort());
});
