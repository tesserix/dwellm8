import React from 'react';
import { render, fireEvent } from '@testing-library/react-native';
import Registration from '../../app/registration';

// The statutory-details screen is the longest step of onboarding and the last
// one before a firm exists. Reached under the wrong account it had no way out:
// no sign out, no sign in, and the tab bar is not mounted yet (#310).

const mockSignOut = jest.fn();
const mockBack = jest.fn();
let mockParams: Record<string, string> = {};

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: mockBack, push: jest.fn() }),
  useLocalSearchParams: () => mockParams,
}));

jest.mock('../auth/session', () => ({
  useSession: () => ({
    session: { email: 'wrong@firm.in' },
    refreshRegistration: jest.fn(),
    signOut: mockSignOut,
  }),
}));

jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return { ...actual, apiFromEnv: () => null };
});

describe('Registration', () => {
  beforeEach(() => {
    mockSignOut.mockReset();
    mockBack.mockReset();
    mockParams = {};
  });

  it('offers a way back to sign in as somebody else', async () => {
    const { getByText } = await render(<Registration />);

    fireEvent.press(getByText(/sign in as somebody else/i));
    expect(mockSignOut).toHaveBeenCalled();
  });

  it('says which account the details are being filed under', async () => {
    const { getByText } = await render(<Registration />);
    expect(getByText(/wrong@firm.in/)).toBeTruthy();
  });

  // s.9 registers brokering a sale, not letting: a manager who only finds
  // tenants and collects rent must not read the RERA card as a demand (#359).
  it('offers the RERA registration rather than demanding it', async () => {
    const { getByText } = await render(<Registration />);
    expect(getByText(/^Optional\./)).toBeTruthy();
  });

  it('has no way back during onboarding, and one when opened again after', async () => {
    const first = await render(<Registration />);
    expect(first.queryByLabelText('Back')).toBeNull();
    first.unmount();

    mockParams = { revisit: '1' };
    const again = await render(<Registration />);
    fireEvent.press(again.getByLabelText('Back'));
    expect(mockBack).toHaveBeenCalled();
  });
});
