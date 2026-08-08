import React from 'react';
import { render, fireEvent } from '@testing-library/react-native';
import { SignIn } from './SignIn';

// One way into every Dwellm8 app. The surface differs by a line of text; the
// rules — six characters, a neutral answer to a reset, no reset while signing
// up — are the same everywhere and are settled here once.

function actions(over: Partial<Record<string, jest.Mock>> = {}) {
  return {
    signIn: jest.fn().mockResolvedValue(undefined),
    signUp: jest.fn().mockResolvedValue(undefined),
    resetPassword: jest.fn().mockResolvedValue(undefined),
    ...over,
  };
}

const type = async (r: ReturnType<typeof render> extends Promise<infer T> ? T : never,
  email: string, password?: string) => {
  await fireEvent.changeText(r.getByPlaceholderText('you@firm.in'), email);
  if (password !== undefined) await fireEvent.changeText(r.getByLabelText('Password'), password);
};

describe('SignIn', () => {
  it('names the surface it is the way into', async () => {
    const r = await render(<SignIn subtitle="Resident" configured actions={actions()} />);
    expect(r.getByText('Resident')).toBeTruthy();
  });

  it('signs in with what was typed', async () => {
    const a = actions();
    const r = await render(<SignIn subtitle="Resident" configured actions={a} />);

    await type(r, 'ritika@firm.in', 'correct horse');
    await fireEvent.press(r.getByRole('button', { name: 'Sign in' }));

    expect(a.signIn).toHaveBeenCalledWith('ritika@firm.in', 'correct horse');
  });

  it('will not send a password too short to be accepted', async () => {
    const a = actions();
    const r = await render(<SignIn subtitle="Resident" configured actions={a} />);

    await type(r, 'ritika@firm.in', 'short');
    await fireEvent.press(r.getByRole('button', { name: 'Sign in' }));

    expect(a.signIn).not.toHaveBeenCalled();
  });

  it('answers a reset without saying whether the address is known', async () => {
    const a = actions();
    const r = await render(<SignIn subtitle="Resident" configured actions={a} />);

    await type(r, 'nobody@firm.in');
    await fireEvent.press(r.getByText(/forgot your password/i));

    expect(a.resetPassword).toHaveBeenCalledWith('nobody@firm.in');
    expect(r.getByText(/if that address has an account/i)).toBeTruthy();
  });

  it('asks for the address before sending a reset', async () => {
    const a = actions();
    const r = await render(<SignIn subtitle="Resident" configured actions={a} />);

    await fireEvent.press(r.getByText(/forgot your password/i));

    expect(a.resetPassword).not.toHaveBeenCalled();
    expect(r.getByText(/enter your email/i)).toBeTruthy();
  });

  it('offers no reset while creating an account', async () => {
    const r = await render(<SignIn subtitle="Resident" configured actions={actions()} />);

    await fireEvent.press(r.getByText(/new here\? create an account/i));

    expect(r.queryByText(/forgot your password/i)).toBeNull();
  });

  it('shows the refusal the provider gave', async () => {
    const a = actions({ signIn: jest.fn().mockRejectedValue(new Error('That email and password do not match')) });
    const r = await render(<SignIn subtitle="Resident" configured actions={a} />);

    await type(r, 'ritika@firm.in', 'wrong password');
    await fireEvent.press(r.getByRole('button', { name: 'Sign in' }));

    expect(await r.findByText('That email and password do not match')).toBeTruthy();
  });

  it('says so plainly when the build has no pool to sign into', async () => {
    const r = await render(<SignIn subtitle="Resident" configured={false} actions={actions()} />);
    expect(r.getByText(/not configured/i)).toBeTruthy();
  });
});
