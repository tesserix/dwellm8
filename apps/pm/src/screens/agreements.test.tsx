import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import AgreementsScreen from '../../app/agreements';

// The library of instruments the firm issues (#341), and what the screen does
// when the server cannot serve it (#343).

const mockTemplates = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsDocumentTemplates: mockTemplates,
    opsDocumentTemplate: jest.fn(),
  }),
}));

beforeEach(() => {
  mockTemplates.mockReset();
});

it('lists what the firm issues', async () => {
  mockTemplates.mockResolvedValue([{
    id: 't1', kind: 'management_agreement', name: 'Property management agreement',
    version: 1, is_default: true, merge_fields: ['owner_name'],
  }]);
  await render(<AgreementsScreen />);
  await waitFor(() => expect(screen.getByText('Owner–manager agreement')).toBeTruthy());
});

it('offers a way back when the library will not load (#343)', async () => {
  mockTemplates.mockRejectedValue(new Error('This is not available on this server yet.'));
  await render(<AgreementsScreen />);
  await waitFor(() =>
    expect(screen.getByText('This is not available on this server yet.')).toBeTruthy());

  mockTemplates.mockResolvedValue([{
    id: 't1', kind: 'rent_agreement', name: 'Rent agreement',
    version: 1, is_default: true, merge_fields: [],
  }]);
  await fireEvent.press(screen.getByText('Try again'));
  await waitFor(() =>
    expect(screen.getByText('Rent agreement (11 months)')).toBeTruthy());
});
