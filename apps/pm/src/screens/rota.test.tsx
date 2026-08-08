import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import RotaScreen from '../../app/rota';

// One colleague's working week (#353). It is edited as a week, so a day
// switched off is a day that goes when the week is saved.

const mockRota = jest.fn();
const mockSetRota = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => ({ id: 's1', name: 'Asha Nair' }),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({ opsRota: mockRota, opsSetRota: mockSetRota }),
}));

beforeEach(() => {
  mockSetRota.mockReset().mockResolvedValue({});
  mockRota.mockReset().mockResolvedValue([
    { weekday: 1, starts_at: '09:00', ends_at: '18:00' },
    { weekday: 2, starts_at: '09:00', ends_at: '18:00' },
  ]);
});

it('lays out the week and says whose it is', async () => {
  await render(<RotaScreen />);
  await waitFor(() => expect(screen.getByText('Monday')).toBeTruthy());
  expect(screen.getByText('Asha Nair')).toBeTruthy();
  expect(screen.getByText('Sunday')).toBeTruthy();
  expect(screen.getByText('18 hours a week')).toBeTruthy();
});

it('switching a day off and saving sends the week without it', async () => {
  await render(<RotaScreen />);
  await waitFor(() => expect(screen.getByText('Monday')).toBeTruthy());

  await fireEvent(screen.getByLabelText('Monday'), 'valueChange', false);
  await fireEvent.press(screen.getByText('Save the week'));

  await waitFor(() => expect(mockSetRota).toHaveBeenCalledWith('s1', [
    { weekday: 2, starts_at: '09:00', ends_at: '18:00' },
  ]));
});

it('refuses a shift that ends before it starts, saying which day', async () => {
  await render(<RotaScreen />);
  await waitFor(() => expect(screen.getByText('Monday')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('Monday ends at'), '08:00');
  await fireEvent.press(screen.getByText('Save the week'));

  expect(await screen.findByText(/Monday must end after it starts/)).toBeTruthy();
  expect(mockSetRota).not.toHaveBeenCalled();
});

it('says what the server refused rather than claiming the week was saved', async () => {
  mockSetRota.mockRejectedValue(new Error('no such colleague'));
  await render(<RotaScreen />);
  await waitFor(() => expect(screen.getByText('Monday')).toBeTruthy());

  await fireEvent.press(screen.getByText('Save the week'));
  expect(await screen.findByText('no such colleague')).toBeTruthy();
});

it('offers a way back when the rota will not load (#343)', async () => {
  mockRota.mockRejectedValueOnce(new Error('This is not available on this server yet.'));
  await render(<RotaScreen />);
  await waitFor(() =>
    expect(screen.getByText('This is not available on this server yet.')).toBeTruthy());
  expect(screen.getByText('Try again')).toBeTruthy();
});
