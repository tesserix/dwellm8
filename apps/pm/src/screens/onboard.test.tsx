import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import Onboard from '../../app/onboard';

// The once-over calls itself the look before it becomes the record, and the
// deposit — the largest number in the transaction — was not on it (#317).

jest.mock('expo-router', () => ({ useRouter: () => ({ back: jest.fn(), replace: jest.fn() }) }));

jest.mock('../data/firm', () => ({ useFirmContact: () => ({ name: '', phone: '' }) }));

jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return { ...actual, apiFromEnv: () => null };
});

const fill = (label: string | RegExp, text: string) =>
  fireEvent.changeText(screen.getByLabelText(label), text);
const next = () => fireEvent.press(screen.getByText('Next'));

async function walkToTheOnceOver() {
  await fill("Owner's name", 'Anjali Menon');
  await fill('Mobile number', '+919999000001');
  await next();

  await fill('Property name', 'Menon Palm Court');
  await fill('Address', 'Panampilly Nagar Avenue');
  await fill('Locality', 'Panampilly Nagar');
  await fill('City', 'Ernakulam');
  await fill(/State code/i, 'KL');
  await fill(/PIN code/i, '682036');
  await next();

  await fill('Unit codes', 'G1, G2, 101');
  await fill(/Carpet area/i, '1150');
  await next();

  await fireEvent(screen.getByLabelText('Set up the first tenancy'), 'valueChange', true);
  await fill("Tenant's name", 'Rahul Varghese');
  await fill("Tenant's mobile", '+919999000002');
  await fill(/Which unit/i, 'G1');
  await fill(/Monthly rent/i, '38000');
  await fill(/Deposit/i, '114000');
  await next();

  await waitFor(() => expect(screen.getByText('One look before it becomes the record.')).toBeTruthy());
}

describe('Onboard — the once-over', () => {
  it('shows the deposit that is about to become the record', async () => {
    await render(<Onboard />);
    await walkToTheOnceOver();

    expect(screen.getByText(/₹1,14,000/)).toBeTruthy();
  });

  it('still shows the rent and the term beside it', async () => {
    await render(<Onboard />);
    await walkToTheOnceOver();

    expect(screen.getByText(/₹38,000/)).toBeTruthy();
    expect(screen.getByText(/→/)).toBeTruthy();
  });
});
