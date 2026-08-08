import React from 'react';
import { fireEvent, render, screen } from '@testing-library/react-native';
import ReceiptScreen from '../../app/receipt';

// A screen reached without the thing it acts on is a dead end unless it hands
// the manager the way back to one (#356).

const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: mockPush }),
  useLocalSearchParams: () => ({}),
}));

beforeEach(() => mockPush.mockReset());

it('sends the manager to the tenancies when opened without one', async () => {
  await render(<ReceiptScreen />);

  expect(screen.getByText('No tenancy to post against')).toBeTruthy();
  await fireEvent.press(screen.getByRole('button', { name: 'Find a tenancy' }));
  expect(mockPush).toHaveBeenCalledWith('/(tabs)/collect');
});
