import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useTeam } from './team';

// The firm's own team (#353). The cap is the point of this screen: a manager
// handed a tenth building has ten neglected ones, so what each person carries
// and what they may still take has to be visible before anybody is assigned.

const mockTeam = jest.fn();
const mockEmploy = jest.fn();
const mockSaveRole = jest.fn();
const mockUpdate = jest.fn();
const mockAssign = jest.fn();
const mockRelease = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  ...jest.requireActual('@dwellm8/mobile-shared'),
  apiFromEnv: () => ({
    opsTeam: mockTeam,
    opsEmployStaff: mockEmploy,
    opsSaveStaffRole: mockSaveRole,
    opsUpdateStaff: mockUpdate,
    opsAssignProperty: mockAssign,
    opsReleaseAssignment: mockRelease,
  }),
}));

beforeEach(() => {
  mockTeam.mockReset().mockResolvedValue({
    roles: [
      { id: 'r1', name: 'Field Executive', permissions: ['property.read'], property_limit: 6, people: 2 },
    ],
    team: [
      { id: 's1', full_name: 'Asha Nair', role_id: 'r1', role_name: 'Field Executive',
        state: 'active', property_limit: 6, held: 6, pan_masked: 'XXXXXX234F' },
      { id: 's2', full_name: 'Ravi Menon', role_id: 'r1', role_name: 'Field Executive',
        state: 'active', property_limit: 6, held: 2 },
      { id: 's3', full_name: 'Deepa Rao', role_id: 'r1', role_name: 'Field Executive',
        state: 'exited', exited_on: '2026-03-31', property_limit: 6, held: 0 },
    ],
    assignments: [
      { id: 'a1', staff_id: 's1', staff_name: 'Asha Nair', property_id: 'p1', property_name: 'Menon Residency' },
      { id: 'a2', staff_id: 's2', staff_name: 'Ravi Menon', property_id: 'p2', property_name: 'Kaloor Heights' },
    ],
  });
  mockEmploy.mockReset().mockResolvedValue({ id: 's4', full_name: 'Nisha K', state: 'active' });
  mockSaveRole.mockReset().mockResolvedValue({ id: 'r2', name: 'Warden', property_limit: 2 });
  mockUpdate.mockReset().mockResolvedValue({});
  mockAssign.mockReset().mockResolvedValue({ id: 'a3', staff_id: 's2', property_id: 'p3' });
  mockRelease.mockReset().mockResolvedValue({});
});

it('shows who is still working here and who has left, separately', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  expect(result.current.working.map((m) => m.full_name)).toEqual(['Asha Nair', 'Ravi Menon']);
  expect(result.current.gone.map((m) => m.full_name)).toEqual(['Deepa Rao']);
});

it('says how much room each person has left before the cap', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  expect(result.current.working[0].spare).toBe(0);
  expect(result.current.working[0].atCapacity).toBe(true);
  expect(result.current.working[1].spare).toBe(4);
  expect(result.current.working[1].atCapacity).toBe(false);
});

it('lists what each person is responsible for beside them', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  expect(result.current.working[0].properties.map((a) => a.property_name)).toEqual(['Menon Residency']);
});

it('refuses to assign to somebody already at their cap, before the round trip', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  await expect(result.current.assign('s1', 'p9')).rejects.toThrow(/as many properties/i);
  expect(mockAssign).not.toHaveBeenCalled();
});

it('assigns a building to somebody with room and reloads the team', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { await result.current.assign('s2', 'p3'); });
  expect(mockAssign).toHaveBeenCalledWith('s2', 'p3');
  expect(mockTeam).toHaveBeenCalledTimes(2);
});

it('hands a building back and reloads', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { await result.current.release('a1'); });
  expect(mockRelease).toHaveBeenCalledWith('a1');
  expect(mockTeam).toHaveBeenCalledTimes(2);
});

it('employs a colleague and reloads the team', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => {
    await result.current.employ({ full_name: 'Nisha K', phone: '+919876500009', pan: 'ABCDE1234F' });
  });
  expect(mockEmploy).toHaveBeenCalledWith(
    expect.objectContaining({ full_name: 'Nisha K', pan: 'ABCDE1234F' }));
  expect(mockTeam).toHaveBeenCalledTimes(2);
});

it('dates somebody out rather than deleting them', async () => {
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));

  await act(async () => { await result.current.exit('s2', '2026-04-30'); });
  expect(mockUpdate).toHaveBeenCalledWith('s2', { state: 'exited', exited_on: '2026-04-30' });
});

it('surfaces what the server said when the team cannot be read', async () => {
  mockTeam.mockRejectedValueOnce(new Error('sign in again'));
  const { result } = await renderHook(() => useTeam());
  await waitFor(() => expect(result.current.loading).toBe(false));
  expect(result.current.error).toBe('sign in again');
});
