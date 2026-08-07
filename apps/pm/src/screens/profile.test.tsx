import React from 'react';
import { render, fireEvent, waitFor } from '@testing-library/react-native';
import Profile from '../../app/profile';

// The profile is where a manager checks whose account this is and leaves it.
// It did neither: "Not signed in" sat above their own email, and Log out was
// a label with nothing behind it (#315).

const mockSignOut = jest.fn();

jest.mock('expo-router', () => ({ useRouter: () => ({ back: jest.fn(), push: jest.fn() }) }));

jest.mock('../auth/session', () => ({
  useSession: () => ({ session: { email: 'samyak.rout@gmail.com' }, signOut: mockSignOut }),
}));

jest.mock('../data/account', () => ({
  useAccount: () => ({
    loading: false, name: 'samyak.rout', initials: 'SR',
    email: 'samyak.rout@gmail.com', phone: '', firmName: 'Rout Estates',
    constitution: 'proprietorship', mayManage: false, properties: 1,
  }),
}));

describe('Profile', () => {
  beforeEach(() => mockSignOut.mockReset());

  it('does not call a signed-in manager signed out', async () => {
    const { queryByText, getByText } = await render(<Profile />);
    await waitFor(() => expect(getByText('samyak.rout')).toBeTruthy());

    expect(queryByText(/not signed in/i)).toBeNull();
  });

  it('logs out when the manager asks to', async () => {
    const { getByText } = await render(<Profile />);

    fireEvent.press(getByText('Log out'));
    expect(mockSignOut).toHaveBeenCalled();
  });
});
