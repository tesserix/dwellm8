import React from 'react';
import { render, screen, waitFor } from '@testing-library/react-native';
import UnitScreen from '../../app/unit';

// What the flat itself is like (#354) — the bathrooms, the facing and the
// furnishing a renter asks about, on the record rather than in the advert.

const mockUnit = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => ({ id: 'u1' }),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({ opsUnit: mockUnit }),
}));

const record = (over: Record<string, unknown>) => ({
  unit: {
    id: 'u1', code: '3B', kind: 'flat', floor: 3, occupancy: 'family',
    carpet_area_sqft: 900, ...over,
  },
  property: { id: 'p1', name: 'Kadavanthra Heights', locality: 'Kadavanthra' },
  ancillaries: [],
});

beforeEach(() => {
  mockUnit.mockReset().mockResolvedValue(record({
    about: 'Living room over the park.', bathrooms: 2, balconies: 1,
    facing: 'north', furnishing: 'semi_furnished', features: ['wardrobes'],
  }));
});

it('says what the flat is like, in the words a renter would use', async () => {
  await render(<UnitScreen />);
  await waitFor(() => expect(screen.getByText('Living room over the park.')).toBeTruthy());

  expect(screen.getByText('2')).toBeTruthy();
  expect(screen.getByText('Semi furnished')).toBeTruthy();
  expect(screen.getByText('North')).toBeTruthy();
  expect(screen.getByText('Wardrobes')).toBeTruthy();
});

it('offers to write the description when the flat has none', async () => {
  mockUnit.mockResolvedValue(record({}));
  await render(<UnitScreen />);
  await waitFor(() => expect(screen.getByText('What this flat is like')).toBeTruthy());

  expect(screen.getByText(/Nothing written yet/)).toBeTruthy();
});
