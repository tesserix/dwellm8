import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import Beds from '../../app/beds';

// The bed board of a new PG is empty, and the rooms it should fill from are
// already on the register. Until #367 there was no way to join the two.

const mockProperties = jest.fn();
const mockProperty = jest.fn();
const mockBeds = jest.fn();
const mockAddBed = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => ({}),
}));

jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return {
    ...actual,
    apiFromEnv: () => ({
      opsProperties: mockProperties, opsProperty: mockProperty,
      opsBeds: mockBeds, opsAddBed: mockAddBed, opsAllocateBed: jest.fn(),
    }),
  };
});

const pg = {
  id: 'p1', code: 'SPG', name: 'Samyak Stay PG', kind: 'coliving',
  address_line1: 'Koramangala', locality: 'Koramangala', city: 'Bengaluru', unit_count: 2,
};

beforeEach(() => {
  mockProperties.mockReset().mockResolvedValue([pg]);
  mockProperty.mockReset().mockResolvedValue({
    property: pg,
    units: [
      { id: 'u1', code: 'G1', kind: 'room', floor: 0, occupancy: 'vacant' },
      { id: 'u2', code: 'F1', kind: 'room', floor: 1, occupancy: 'vacant' },
    ],
  });
  mockBeds.mockReset().mockResolvedValue([]);
  mockAddBed.mockReset().mockResolvedValue({ bed: { id: 'b1', label: 'G1-A' } });
});

it('offers the rooms a bed can go in when the board is empty', async () => {
  await render(<Beds />);
  await waitFor(() => expect(screen.getByText('No beds on the register')).toBeTruthy());
  expect(screen.getByText('G1')).toBeTruthy();
  expect(screen.getByText('F1')).toBeTruthy();
});

it('puts a bed in the room the warden picked, at the rent it is let for', async () => {
  await render(<Beds />);
  await waitFor(() => expect(screen.getByText('G1')).toBeTruthy());

  await fireEvent.press(screen.getByText('G1'));
  await fireEvent.changeText(screen.getByPlaceholderText('Bed — G1-A, upper bunk'), 'G1-A');
  await fireEvent.changeText(screen.getByPlaceholderText('Rent per month, ₹'), '9500');
  await fireEvent.press(screen.getByText('Add this bed'));

  await waitFor(() => expect(mockAddBed).toHaveBeenCalledWith('u1', 'G1-A', 950000));
  // A rent left in the box is typed over, not replaced, and the next bed lands
  // at a number nobody meant — ₹9,50,09,500 was the first one on the live board.
  expect(screen.getByPlaceholderText('Rent per month, ₹').props.value).toBe('');
});

it('says a bed cannot be added before a room is picked', async () => {
  await render(<Beds />);
  await waitFor(() => expect(screen.getByText('G1')).toBeTruthy());

  await fireEvent.changeText(screen.getByPlaceholderText('Bed — G1-A, upper bunk'), 'G1-A');
  await fireEvent.press(screen.getByText('Add this bed'));

  await waitFor(() => expect(screen.getByText(/which room/i)).toBeTruthy());
  expect(mockAddBed).not.toHaveBeenCalled();
});
