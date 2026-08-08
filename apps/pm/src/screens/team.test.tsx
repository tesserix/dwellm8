import React from 'react';
import { render, fireEvent, screen, waitFor } from '@testing-library/react-native';
import TeamScreen from '../../app/team';

// The firm's own team (#353). The screen has to make the cap legible before
// anybody is assigned, and it must never show a PAN whole.

const mockTeam = jest.fn();
const mockAssign = jest.fn();
const mockRelease = jest.fn();
const mockUpdate = jest.fn();
const mockProperties = jest.fn();
const mockPush = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: mockPush }),
  useLocalSearchParams: () => ({}),
}));

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsTeam: mockTeam,
    opsAssignProperty: mockAssign,
    opsReleaseAssignment: mockRelease,
    opsUpdateStaff: mockUpdate,
    opsEmployStaff: jest.fn(),
    opsSaveStaffRole: jest.fn(),
    opsProperties: mockProperties,
  }),
}));

beforeEach(() => {
  mockPush.mockReset();
  mockAssign.mockReset().mockResolvedValue({ id: 'a3' });
  mockRelease.mockReset().mockResolvedValue({});
  mockUpdate.mockReset().mockResolvedValue({});
  mockProperties.mockReset().mockResolvedValue([
    { id: 'p1', name: 'Menon Residency' },
    { id: 'p3', name: 'Panampilly Court' },
  ]);
  mockTeam.mockReset().mockResolvedValue({
    roles: [{ id: 'r1', name: 'Field Executive', permissions: ['property.read'], property_limit: 6, people: 2 }],
    team: [
      { id: 's1', full_name: 'Asha Nair', role_name: 'Field Executive', state: 'active',
        property_limit: 6, held: 6, pan_masked: 'XXXXXX234F', designation: 'Field executive' },
      { id: 's2', full_name: 'Ravi Menon', role_name: 'Field Executive', state: 'active',
        property_limit: 6, held: 1 },
    ],
    assignments: [
      { id: 'a1', staff_id: 's1', property_id: 'p1', property_name: 'Menon Residency' },
      { id: 'a2', staff_id: 's2', property_id: 'p2', property_name: 'Kaloor Heights' },
    ],
  });
});

it('names everybody the firm employs and what each carries', async () => {
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Asha Nair')).toBeTruthy());
  expect(screen.getByText('Ravi Menon')).toBeTruthy();
  expect(screen.getByText('6 of 6 properties')).toBeTruthy();
  expect(screen.getByText('1 of 6 properties')).toBeTruthy();
});

it('shows only the mask of a PAN, never the number', async () => {
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Asha Nair')).toBeTruthy());

  await fireEvent.press(screen.getByText('Asha Nair'));
  expect(await screen.findByText('XXXXXX234F')).toBeTruthy();
  expect(screen.queryByText(/ABCDE/)).toBeNull();
});

it('says who is full, so a building is not offered to them', async () => {
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Asha Nair')).toBeTruthy());
  expect(screen.getByText('At capacity')).toBeTruthy();
  expect(screen.getByText('Room for 5 more')).toBeTruthy();
});

it('hands a building to somebody with room', async () => {
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Ravi Menon')).toBeTruthy());

  await fireEvent.press(screen.getByText('Ravi Menon'));
  await fireEvent.press(await screen.findByText('Panampilly Court'));

  await waitFor(() => expect(mockAssign).toHaveBeenCalledWith('s2', 'p3'));
});

it('hands a building back', async () => {
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Asha Nair')).toBeTruthy());

  await fireEvent.press(screen.getByText('Asha Nair'));
  await fireEvent.press(await screen.findByLabelText('Hand back Menon Residency'));

  await waitFor(() => expect(mockRelease).toHaveBeenCalledWith('a1'));
});

it('says what the server refused, rather than failing silently', async () => {
  mockAssign.mockRejectedValue(new Error('another colleague is already responsible for that property'));
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Ravi Menon')).toBeTruthy());

  await fireEvent.press(screen.getByText('Ravi Menon'));
  await fireEvent.press(await screen.findByText('Panampilly Court'));

  await waitFor(() =>
    expect(screen.getByText('another colleague is already responsible for that property')).toBeTruthy());
});

it('offers a way back when the team will not load (#343)', async () => {
  mockTeam.mockRejectedValueOnce(new Error('This is not available on this server yet.'));
  await render(<TeamScreen />);
  await waitFor(() =>
    expect(screen.getByText('This is not available on this server yet.')).toBeTruthy());
  expect(screen.getByText('Try again')).toBeTruthy();
});

it('sends the manager to employ somebody when the firm has nobody yet', async () => {
  mockTeam.mockResolvedValue({ roles: [], team: [], assignments: [] });
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Nobody on the team yet')).toBeTruthy());

  await fireEvent.press(screen.getByText('Add a manager'));
  expect(mockPush).toHaveBeenCalledWith('/employ');
});

it('opens one colleague’s week from their card', async () => {
  await render(<TeamScreen />);
  await waitFor(() => expect(screen.getByText('Asha Nair')).toBeTruthy());

  await fireEvent.press(screen.getByText('Asha Nair'));
  await fireEvent.press(await screen.findByText('Working hours'));

  expect(mockPush).toHaveBeenCalledWith({ pathname: '/rota', params: { id: 's1', name: 'Asha Nair' } });
});
