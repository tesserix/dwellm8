import React from 'react';
import { render, fireEvent } from '@testing-library/react-native';
import SignIn from '../../app/signin';

// A manager locked out on a site visit needs the way back in to be on the
// screen that refused them, not in a support call (#357).

const mockResetPassword = jest.fn();

jest.mock('../auth/session', () => ({
  useSession: () => ({
    identity: {},
    signIn: jest.fn(),
    signUp: jest.fn(),
    resetPassword: mockResetPassword,
  }),
}));

describe('SignIn', () => {
  beforeEach(() => mockResetPassword.mockReset().mockResolvedValue(undefined));

  it('sends a reset link for the address already typed', async () => {
    const { getByText, getByPlaceholderText } = await render(<SignIn />);

    await fireEvent.changeText(getByPlaceholderText('you@firm.in'), 'ritika@firm.in');
    await fireEvent.press(getByText(/forgot your password/i));

    expect(mockResetPassword).toHaveBeenCalledWith('ritika@firm.in');
  });

  it('confirms without saying whether that address has an account', async () => {
    const { getByText, getByPlaceholderText, queryByText } = await render(<SignIn />);

    await fireEvent.changeText(getByPlaceholderText('you@firm.in'), 'nobody@firm.in');
    await fireEvent.press(getByText(/forgot your password/i));

    expect(getByText(/if that address has an account/i)).toBeTruthy();
    expect(queryByText(/no account/i)).toBeNull();
  });

  it('asks for the address before sending anything', async () => {
    const { getByText } = await render(<SignIn />);

    await fireEvent.press(getByText(/forgot your password/i));

    expect(mockResetPassword).not.toHaveBeenCalled();
    expect(getByText(/enter your email/i)).toBeTruthy();
  });

  it('does not offer a reset while creating an account', async () => {
    const { getByText, queryByText } = await render(<SignIn />);

    await fireEvent.press(getByText(/new here\? create an account/i));

    expect(queryByText(/forgot your password/i)).toBeNull();
  });
});
