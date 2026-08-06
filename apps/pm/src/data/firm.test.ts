import { renderHook, waitFor } from '@testing-library/react-native';
import { useFirmContact } from './firm';

// Onboarding your own property should not ask you to retype the name and
// number you filed on the registration screen minutes earlier (#295).

const mockOpsRegistration = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsRegistration: mockOpsRegistration } : null),
}));

describe('useFirmContact', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockOpsRegistration.mockReset();
  });

  it('gives the name and number the firm filed', async () => {
    mockOpsRegistration.mockResolvedValue({ legal_name: 'Samyak Rout', contact_phone: '+919847012345' });
    const { result } = await renderHook(() => useFirmContact());

    await waitFor(() => expect(result.current.name).toBe('Samyak Rout'));
    expect(result.current.phone).toBe('+919847012345');
  });

  it('stays empty rather than guessing when the read fails', async () => {
    mockOpsRegistration.mockRejectedValue(new Error('sign in again'));
    const { result } = await renderHook(() => useFirmContact());

    await waitFor(() => expect(mockOpsRegistration).toHaveBeenCalled());
    expect(result.current.name).toBe('');
    expect(result.current.phone).toBe('');
  });

  it('asks for nothing when the build has no API', async () => {
    delete process.env.EXPO_PUBLIC_API_URL;
    const { result } = await renderHook(() => useFirmContact());

    expect(mockOpsRegistration).not.toHaveBeenCalled();
    expect(result.current.name).toBe('');
  });
});
