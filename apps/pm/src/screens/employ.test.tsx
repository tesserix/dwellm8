import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import EmployScreen from '../../app/employ';

// Employing a sub-manager (#353). The firm types the terms once: what the
// person is paid, what they may do, and how many buildings they carry.

const mockTeam = jest.fn();
const mockEmploy = jest.fn();
const mockBack = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: mockBack, push: jest.fn(), replace: jest.fn() }),
  useLocalSearchParams: () => ({}),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsTeam: mockTeam,
    opsEmployStaff: mockEmploy,
    opsSaveStaffRole: jest.fn(),
    opsAssignProperty: jest.fn(),
    opsReleaseAssignment: jest.fn(),
    opsUpdateStaff: jest.fn(),
  }),
}));

beforeEach(() => {
  mockBack.mockReset();
  mockEmploy.mockReset().mockResolvedValue({ id: 's9', full_name: 'Nisha Kurian' });
  mockTeam.mockReset().mockResolvedValue({
    roles: [
      { id: 'r1', name: 'Field Executive', permissions: ['property.read'], property_limit: 6 },
      { id: 'r2', name: 'Warden', permissions: ['property.read'], property_limit: 2 },
    ],
    team: [],
    assignments: [],
  });
});

async function fillTheBasics() {
  await fireEvent.changeText(screen.getByLabelText('Full name'), 'Nisha Kurian');
  await fireEvent.changeText(screen.getByLabelText('Mobile'), '+919876500009');
}

it('will not employ somebody with no name or no way to reach them', async () => {
  await render(<EmployScreen />);
  await waitFor(() => expect(screen.getByLabelText('Full name')).toBeTruthy());

  await fireEvent.press(screen.getByText('Add to the team'));
  expect(await screen.findByText(/needs a name/i)).toBeTruthy();
  expect(mockEmploy).not.toHaveBeenCalled();
});

it('refuses a PAN that is not a PAN before the server sees it', async () => {
  await render(<EmployScreen />);
  await waitFor(() => expect(screen.getByLabelText('Full name')).toBeTruthy());
  await fillTheBasics();

  await fireEvent.changeText(screen.getByLabelText('PAN'), 'ABCD1234');
  await fireEvent.press(screen.getByText('Add to the team'));

  expect(await screen.findByText(/ten characters/i)).toBeTruthy();
  expect(mockEmploy).not.toHaveBeenCalled();
});

it('sends the whole employment record, salary in paise', async () => {
  await render(<EmployScreen />);
  await waitFor(() => expect(screen.getByLabelText('Full name')).toBeTruthy());
  await fillTheBasics();

  await fireEvent.press(screen.getByText('Warden'));
  await fireEvent.changeText(screen.getByLabelText('PAN'), 'abcde1234f');
  await fireEvent.changeText(screen.getByLabelText('Salary'), '45000');
  await fireEvent.changeText(screen.getByLabelText('Designation'), 'Warden, Kaloor');
  await fireEvent.press(screen.getByText('Add to the team'));

  await waitFor(() => expect(mockEmploy).toHaveBeenCalled());
  expect(mockEmploy).toHaveBeenCalledWith(expect.objectContaining({
    full_name: 'Nisha Kurian',
    phone: '+919876500009',
    role_id: 'r2',
    pan: 'ABCDE1234F',
    salary_minor: 4500000,
    salary_currency: 'INR',
    pay_frequency: 'monthly',
    employment_type: 'full_time',
    designation: 'Warden, Kaloor',
    state: 'invited',
  }));
});

it('carries the role’s own cap unless the firm overrides it for this person', async () => {
  await render(<EmployScreen />);
  await waitFor(() => expect(screen.getByLabelText('Full name')).toBeTruthy());
  await fillTheBasics();

  await fireEvent.press(screen.getByText('Warden'));
  expect(screen.getByText('Carries up to 2 properties')).toBeTruthy();

  await fireEvent.changeText(screen.getByLabelText('Properties they can carry'), '4');
  await fireEvent.press(screen.getByText('Add to the team'));

  await waitFor(() => expect(mockEmploy).toHaveBeenCalledWith(
    expect.objectContaining({ property_limit: 4 })));
});

it('says what the server refused, and keeps what was typed', async () => {
  mockEmploy.mockRejectedValue(new Error('that is not a PAN'));
  await render(<EmployScreen />);
  await waitFor(() => expect(screen.getByLabelText('Full name')).toBeTruthy());
  await fillTheBasics();

  await fireEvent.press(screen.getByText('Add to the team'));

  expect(await screen.findByText('that is not a PAN')).toBeTruthy();
  expect(screen.getByLabelText('Full name').props.value).toBe('Nisha Kurian');
});

it('goes back to the team once somebody is employed', async () => {
  await render(<EmployScreen />);
  await waitFor(() => expect(screen.getByLabelText('Full name')).toBeTruthy());
  await fillTheBasics();

  await fireEvent.press(screen.getByText('Add to the team'));
  await waitFor(() => expect(mockBack).toHaveBeenCalled());
});

it('does not ask for an Aadhaar number, which is not held (ADR-0013)', async () => {
  await render(<EmployScreen />);
  await waitFor(() => expect(screen.getByLabelText('Full name')).toBeTruthy());
  expect(screen.queryByLabelText(/aadhaar/i)).toBeNull();
});
