import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import DescribeScreen from '../../app/describe';

// The description a renter reads (#354). The screen edits the building when it
// is opened from one, and the flat when it is opened from a flat.

const mockProperty = jest.fn();
const mockDescribeProperty = jest.fn();
const mockUnit = jest.fn();
const mockDescribeUnit = jest.fn();

let mockParams: Record<string, string> = { id: 'prop-1' };

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => mockParams,
}));

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
  mockParams = { id: 'prop-1' };
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
      about: 'Over the park.', features: [], bathrooms: 2,
      facing: 'north', furnishing: 'semi_furnished',
    },
    property: { id: 'prop-1', name: 'Kadavanthra Heights' },
    ancillaries: [],
  });
  mockDescribeUnit.mockReset().mockResolvedValue({});
});

it('shows what was already written about the building', async () => {
  await render(<DescribeScreen />);
  await waitFor(() => expect(screen.getByLabelText('About this building')).toBeTruthy());
  expect(screen.getByLabelText('About this building').props.value).toBe('A quiet block.');
  expect(screen.getByText('Kadavanthra Heights')).toBeTruthy();
});

it('saves the building description with the amenities that are on', async () => {
  await render(<DescribeScreen />);
  await waitFor(() => expect(screen.getByLabelText('About this building')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('About this building'), 'Set back from the road.');
  await fireEvent.press(screen.getByLabelText('Gym'));
  await fireEvent.press(screen.getByText('Save the description'));

  await waitFor(() => expect(mockDescribeProperty)
    .toHaveBeenCalledWith('prop-1', 'Set back from the road.', ['lift', 'gym']));
});

it('says what the server refused rather than claiming it saved', async () => {
  mockDescribeProperty.mockRejectedValue(new Error('one of those amenities is not one we list'));
  await render(<DescribeScreen />);
  await waitFor(() => expect(screen.getByLabelText('About this building')).toBeTruthy());

  await fireEvent.press(screen.getByText('Save the description'));
  expect(await screen.findByText('one of those amenities is not one we list')).toBeTruthy();
});

it('opened from a flat, it edits the flat', async () => {
  mockParams = { unit: 'u1' };
  await render(<DescribeScreen />);
  await waitFor(() => expect(screen.getByLabelText('About this flat')).toBeTruthy());

  expect(screen.getByLabelText('Bathrooms').props.value).toBe('2');
  await fireEvent.changeText(screen.getByLabelText('Bathrooms'), '3');
  await fireEvent.press(screen.getByLabelText('Fully furnished'));
  await fireEvent.press(screen.getByText('Save the description'));

  await waitFor(() => expect(mockDescribeUnit).toHaveBeenCalledWith('u1',
    expect.objectContaining({ bathrooms: 3, furnishing: 'fully_furnished' })));
});

it('refuses a count that is not a number, at the flat', async () => {
  mockParams = { unit: 'u1' };
  await render(<DescribeScreen />);
  await waitFor(() => expect(screen.getByLabelText('Bathrooms')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('Bathrooms'), 'two');
  await fireEvent.press(screen.getByText('Save the description'));

  expect(await screen.findByText(/has to be a number/)).toBeTruthy();
  expect(mockDescribeUnit).not.toHaveBeenCalled();
});
