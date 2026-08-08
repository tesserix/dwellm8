import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import NearbyScreen from '../../app/nearby';

// What is near the property (#354) — the schools and the metro a renter asks
// about first, entered by the manager who measured the walk.

const mockPlaces = jest.fn();
const mockAdd = jest.fn();
const mockRetire = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => ({ id: 'prop-1', name: 'Kadavanthra Heights' }),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsPlaces: mockPlaces, opsAddPlace: mockAdd, opsRetirePlace: mockRetire,
  }),
}));

beforeEach(() => {
  mockPlaces.mockReset().mockResolvedValue([
    { id: 'p1', category: 'school', name: 'Spotswood Primary', distance_m: 600, travel_mode: 'walk', tags: ['government'] },
    { id: 'p2', category: 'metro', name: 'Indiranagar', distance_m: 1400, travel_mode: 'walk', tags: [] },
  ]);
  mockAdd.mockReset().mockResolvedValue({ place: { id: 'p3' } });
  mockRetire.mockReset().mockResolvedValue({});
});

it('lists what is nearby with the walk a renter would make', async () => {
  await render(<NearbyScreen />);
  await waitFor(() => expect(screen.getByText('Spotswood Primary')).toBeTruthy());
  expect(screen.getByText('600 m · 8 min walk')).toBeTruthy();
  expect(screen.getByText('Indiranagar')).toBeTruthy();
  // Once as the heading over the list, once as the kind to add another under.
  expect(screen.getAllByText('Schools')).toHaveLength(2);
});

it('adds a school with the distance the manager measured', async () => {
  await render(<NearbyScreen />);
  await waitFor(() => expect(screen.getByText('Spotswood Primary')).toBeTruthy());

  await fireEvent.press(screen.getByText('Hospitals'));
  await fireEvent.changeText(screen.getByLabelText('Name of the place'), 'Lourdes Hospital');
  await fireEvent.changeText(screen.getByLabelText('How far, in metres'), '900');
  await fireEvent.press(screen.getByText('Add to the list'));

  await waitFor(() => expect(mockAdd).toHaveBeenCalledWith('prop-1', expect.objectContaining({
    category: 'hospital', name: 'Lourdes Hospital', distance_m: 900, travel_mode: 'walk',
  })));
});

it('says what the server refused rather than claiming it was added', async () => {
  mockAdd.mockRejectedValue(new Error('that place is already on this list'));
  await render(<NearbyScreen />);
  await waitFor(() => expect(screen.getByText('Spotswood Primary')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('Name of the place'), 'Spotswood Primary');
  await fireEvent.changeText(screen.getByLabelText('How far, in metres'), '600');
  await fireEvent.press(screen.getByText('Add to the list'));

  expect(await screen.findByText('that place is already on this list')).toBeTruthy();
});

it('removing a place takes it off the list a renter reads', async () => {
  await render(<NearbyScreen />);
  await waitFor(() => expect(screen.getByText('Spotswood Primary')).toBeTruthy());

  await fireEvent.press(screen.getByLabelText('Remove Spotswood Primary'));
  await waitFor(() => expect(mockRetire).toHaveBeenCalledWith('p1'));
});

it('offers a way back when the list will not load (#343)', async () => {
  mockPlaces.mockRejectedValueOnce(new Error('This is not available on this server yet.'));
  await render(<NearbyScreen />);
  expect(await screen.findByText('This is not available on this server yet.')).toBeTruthy();
});
