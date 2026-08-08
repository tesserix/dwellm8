import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import Advertise from '../../app/advertise';

// Putting a vacant flat on the market (#370). Everything a renter is quoted is
// stated here or explicitly nil — the API refuses to publish otherwise.

const mockUnit = jest.fn();
const mockCreate = jest.fn();
const mockPublish = jest.fn();
const mockWithdraw = jest.fn();


jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => ({ unit: 'u1' }),
}));

jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return {
    ...actual,
    apiFromEnv: () => ({
      opsUnit: mockUnit, createListing: mockCreate, publishListing: mockPublish,
      withdrawListing: mockWithdraw,
    }),
  };
});

beforeEach(() => {
  mockUnit.mockReset().mockResolvedValue({
    unit: { id: 'u1', code: '102', kind: 'flat', floor: 1, occupancy: 'vacant', carpet_area_sqft: 900 },
    property: {
      id: 'p1', name: 'Samyak Residency', locality: 'Indiranagar',
      city: 'Bengaluru', state_code: 'KA',
    },
    ancillaries: [],
  });
  mockCreate.mockReset().mockResolvedValue({ id: 'lst-1', state: 'draft' });
  mockPublish.mockReset().mockResolvedValue({});
  mockWithdraw.mockReset().mockResolvedValue({});
});

const fill = async () => {
  await fireEvent.changeText(screen.getByLabelText('Headline'), 'Two bedrooms off the park');
  await fireEvent.changeText(screen.getByLabelText('Rent a month'), '32000');
  await fireEvent.changeText(screen.getByLabelText('Deposit'), '96000');
};

it('advertises the flat where it is, without asking the manager to retype it', async () => {
  await render(<Advertise />);
  await waitFor(() => expect(screen.getByText(/Samyak Residency/)).toBeTruthy());
  expect(screen.getByText(/Indiranagar/)).toBeTruthy();
});

it('will not publish until every cost is stated or called nil', async () => {
  await render(<Advertise />);
  await waitFor(() => expect(screen.getByLabelText('Headline')).toBeTruthy());
  await fill();
  await fireEvent.press(screen.getByText('Put it on the market'));

  await waitFor(() => expect(screen.getByText(/a renter is quoted what is here/)).toBeTruthy());
  expect(mockCreate).not.toHaveBeenCalled();
});

it('creates the draft and puts it on the market, money in minor units', async () => {
  await render(<Advertise />);
  await waitFor(() => expect(screen.getByLabelText('Headline')).toBeTruthy());
  await fill();
  await fireEvent(screen.getByLabelText('These are all the costs'), 'valueChange', true);
  await fireEvent.press(screen.getByText('Put it on the market'));

  await waitFor(() => expect(mockCreate).toHaveBeenCalledWith(expect.objectContaining({
    property_id: 'p1', unit_id: 'u1', locality: 'Indiranagar', city: 'Bengaluru',
    state_code: 'KA', rent_minor: 3200000, deposit_minor: 9600000, costs_confirmed: true,
  })));
  expect(mockPublish).toHaveBeenCalledWith('lst-1');
});

// The claims gate is the API's (#143). A manager must read the rule it cites,
// not "could not publish". A draft refused there cannot be amended, so it goes.
it('reads the refusal the API gives, and leaves nothing half-advertised', async () => {
  mockPublish.mockRejectedValue(new Error('“no brokerage” may not be claimed — RERA 2016 s. 9'));
  await render(<Advertise />);
  await waitFor(() => expect(screen.getByLabelText('Headline')).toBeTruthy());
  await fill();
  await fireEvent(screen.getByLabelText('These are all the costs'), 'valueChange', true);
  await fireEvent.press(screen.getByText('Put it on the market'));

  await waitFor(() => expect(screen.getByText(/RERA 2016 s. 9/)).toBeTruthy());
  expect(screen.getByText(/Nothing was advertised/)).toBeTruthy();
  expect(mockWithdraw).toHaveBeenCalledWith('lst-1');
});
