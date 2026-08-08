import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import RolesScreen from '../../app/roles';

// The jobs a firm defines (#353): what each may do, and how many buildings it
// carries. The cap is the whole point, so a role cannot be saved without one.

const mockTeam = jest.fn();
const mockSaveRole = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => ({}),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsTeam: mockTeam,
    opsSaveStaffRole: mockSaveRole,
    opsEmployStaff: jest.fn(),
    opsAssignProperty: jest.fn(),
    opsReleaseAssignment: jest.fn(),
    opsUpdateStaff: jest.fn(),
  }),
}));

beforeEach(() => {
  mockSaveRole.mockReset().mockResolvedValue({ id: 'r9', name: 'Warden', property_limit: 2 });
  mockTeam.mockReset().mockResolvedValue({
    roles: [{
      id: 'r1', name: 'Field Executive',
      permissions: ['property.read', 'maintenance.write'], property_limit: 6, people: 2,
    }],
    team: [],
    assignments: [],
  });
});

it('lists the roles the firm has defined and who holds them', async () => {
  await render(<RolesScreen />);
  await waitFor(() => expect(screen.getByText('Field Executive')).toBeTruthy());
  expect(screen.getByText('Carries up to 6 properties · 2 people')).toBeTruthy();
});

it('will not save a role with no name', async () => {
  await render(<RolesScreen />);
  await waitFor(() => expect(screen.getByLabelText('Role name')).toBeTruthy());

  await fireEvent.press(screen.getByText('Save the role'));
  expect(await screen.findByText(/needs a name/i)).toBeTruthy();
  expect(mockSaveRole).not.toHaveBeenCalled();
});

it('will not save a role that may do nothing', async () => {
  await render(<RolesScreen />);
  await waitFor(() => expect(screen.getByLabelText('Role name')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('Role name'), 'Warden');
  await fireEvent.press(screen.getByText('Save the role'));

  expect(await screen.findByText(/may do nothing/i)).toBeTruthy();
  expect(mockSaveRole).not.toHaveBeenCalled();
});

it('keeps the cap inside what one person can maintain', async () => {
  await render(<RolesScreen />);
  await waitFor(() => expect(screen.getByLabelText('Role name')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('Role name'), 'Warden');
  await fireEvent(screen.getByLabelText('See the properties under management'), 'valueChange', true);
  await fireEvent.changeText(screen.getByLabelText('Properties this role carries'), '0');
  await fireEvent.press(screen.getByText('Save the role'));

  expect(await screen.findByText(/between 1 and 50/i)).toBeTruthy();
  expect(mockSaveRole).not.toHaveBeenCalled();
});

it('saves a role with what it may do and what it carries', async () => {
  await render(<RolesScreen />);
  await waitFor(() => expect(screen.getByLabelText('Role name')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('Role name'), 'Warden');
  await fireEvent(screen.getByLabelText('See the properties under management'), 'valueChange', true);
  await fireEvent(screen.getByLabelText('Raise and close maintenance jobs'), 'valueChange', true);
  await fireEvent.changeText(screen.getByLabelText('Properties this role carries'), '2');
  await fireEvent.press(screen.getByText('Save the role'));

  await waitFor(() => expect(mockSaveRole).toHaveBeenCalledWith({
    name: 'Warden',
    permissions: ['property.read', 'maintenance.write'],
    property_limit: 2,
  }));
});

it('says what the server refused', async () => {
  mockSaveRole.mockRejectedValue(new Error('“billing.write” is not something this platform checks'));
  await render(<RolesScreen />);
  await waitFor(() => expect(screen.getByLabelText('Role name')).toBeTruthy());

  await fireEvent.changeText(screen.getByLabelText('Role name'), 'Warden');
  await fireEvent(screen.getByLabelText('See the properties under management'), 'valueChange', true);
  await fireEvent.press(screen.getByText('Save the role'));

  expect(await screen.findByText(/is not something this platform checks/)).toBeTruthy();
});
