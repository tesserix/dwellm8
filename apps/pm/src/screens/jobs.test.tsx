import React from 'react';
import { render, waitFor } from '@testing-library/react-native';
import Jobs from '../../app/(tabs)/jobs';

// An empty queue is the firm's normal state on a quiet week. A line of grey
// text inside an otherwise blank card reads as a screen that failed to load.

const mockTickets = jest.fn();

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));

jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return { ...actual, apiFromEnv: () => ({ opsTickets: mockTickets }) };
});

beforeEach(() => mockTickets.mockReset().mockResolvedValue([]));

it('says why the queue is empty rather than showing a blank card', async () => {
  const { getByTestId, getByText, queryByTestId } = await render(<Jobs />);

  await waitFor(() => expect(getByTestId('empty-state')).toBeTruthy());
  expect(getByText('Nothing to fix')).toBeTruthy();
  expect(queryByTestId('row-card')).toBeNull();
});
