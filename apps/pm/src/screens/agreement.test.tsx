import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import AgreementScreen from '../../app/agreement';

// The owner–manager agreement (#340). The firm fills what only it knows, and
// what prints is filed against the property — the manager may enter and
// manage, may not sell, and is owed four months' notice of a sale.

const mockFields = jest.fn();
const mockPrint = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), replace: jest.fn() }),
  useLocalSearchParams: () => ({ id: 'p1' }),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsManagementAgreementFields: mockFields,
    opsPrintManagementAgreement: mockPrint,
  }),
}));

beforeEach(() => {
  mockFields.mockReset().mockResolvedValue({
    fields: ['owner_name', 'management_fee_pct'],
    supplied: ['property_address'],
    sale_notice_months: 4,
  });
  mockPrint.mockReset().mockResolvedValue({
    filename: 'management-agreement-bpg.pdf',
    content_type: 'application/pdf',
    pdf_base64: 'JVBERi0=',
    document_id: 'd1',
    download_url: 'https://signed.get/agreement',
  });
});

it('says what the agreement binds each side to before it is printed', async () => {
  await render(<AgreementScreen />);

  expect(await screen.findByText(/no authority to sell/i)).toBeTruthy();
  expect(screen.getByText(/four months/i)).toBeTruthy();
});

it('asks for the fields the server names, and prints', async () => {
  await render(<AgreementScreen />);

  const owner = await screen.findByLabelText('Owner name');
  await fireEvent.changeText(owner, 'Anjali Menon');
  await fireEvent.changeText(screen.getByLabelText('Management fee (%)'), '8');
  await fireEvent.press(screen.getByLabelText('Print the agreement'));

  await waitFor(() => expect(mockPrint).toHaveBeenCalledWith('p1',
    { owner_name: 'Anjali Menon', management_fee_pct: '8' }));
  expect(await screen.findByText(/management-agreement-bpg\.pdf/)).toBeTruthy();
});

// Printing with a field blank is what puts a placeholder into a signed
// instrument, so the screen refuses before the request is made.
it('refuses to print with a field left blank', async () => {
  await render(<AgreementScreen />);

  await fireEvent.changeText(await screen.findByLabelText('Owner name'), 'Anjali Menon');
  await fireEvent.press(screen.getByLabelText('Print the agreement'));

  await waitFor(() => expect(screen.getByText(/still blank/i)).toBeTruthy());
  expect(mockPrint).not.toHaveBeenCalled();
});
