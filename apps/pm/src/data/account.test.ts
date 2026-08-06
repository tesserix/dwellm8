import { renderHook, waitFor } from '@testing-library/react-native';
import { useAccount } from './account';

// Who is signed in and which firm they are acting for. A profile screen that
// shows somebody else's name and somebody else's authority is worse than one
// that shows nothing (#251).

const mockMe = jest.fn();
const mockRegistration = jest.fn();
const mockProperties = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL
    ? { me: mockMe, opsRegistration: mockRegistration, opsProperties: mockProperties }
    : null),
}));

describe('useAccount', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockMe.mockReset().mockResolvedValue({ party_id: 'p1', display_name: 'Samyak Rout', email: 's@example.test' });
    mockRegistration.mockReset().mockResolvedValue({
      legal_name: 'Rout Estates LLP', trade_name: 'Rout Estates', constitution: 'llp',
      state: 'active', may_manage: true, authorities: [], required: [], outstanding: [], fields: [],
    });
    mockProperties.mockReset().mockResolvedValue([{ id: 'pr1' }, { id: 'pr2' }]);
  });

  it('reads the signed-in person and the firm they act for', async () => {
    const { result } = await renderHook(() => useAccount());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.name).toBe('Samyak Rout');
    expect(result.current.initials).toBe('SR');
    expect(result.current.firmName).toBe('Rout Estates');
    expect(result.current.properties).toBe(2);
    expect(result.current.mayManage).toBe(true);
  });

  it('falls back to the legal name when the firm trades under none', async () => {
    mockRegistration.mockResolvedValue({
      legal_name: 'Rout Estates LLP', constitution: 'llp', state: 'submitted', may_manage: false,
      authorities: [], required: [], outstanding: [], fields: [],
    });
    const { result } = await renderHook(() => useAccount());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.firmName).toBe('Rout Estates LLP');
    expect(result.current.mayManage).toBe(false);
  });

  it('still shows the person when the firm registration cannot be read', async () => {
    mockRegistration.mockRejectedValue(new Error('no registration on this organisation'));
    const { result } = await renderHook(() => useAccount());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.name).toBe('Samyak Rout');
    expect(result.current.firmName).toBe('');
  });

  it('names nobody when the build has no API', async () => {
    delete process.env.EXPO_PUBLIC_API_URL;
    const { result } = await renderHook(() => useAccount());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.name).toBe('');
    expect(result.current.initials).toBe('');
    expect(mockMe).not.toHaveBeenCalled();
  });
});
