import { renderHook } from '@testing-library/react-native';
import { useBack } from './nav';

// A screen opened cold — a deep link, or a reload while it was on top — has
// nothing behind it, and router.back() then does nothing at all except log
// "GO_BACK was not handled by any navigator" (#347).

const mockBack = jest.fn();
const mockReplace = jest.fn();
const mockCanGoBack = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: mockBack, replace: mockReplace, canGoBack: mockCanGoBack }),
}));

beforeEach(() => {
  mockBack.mockReset();
  mockReplace.mockReset();
  mockCanGoBack.mockReset();
});

it('goes back when there is something behind the screen', async () => {
  mockCanGoBack.mockReturnValue(true);
  const { result } = await renderHook(() => useBack('/(tabs)'));
  result.current();
  expect(mockBack).toHaveBeenCalled();
  expect(mockReplace).not.toHaveBeenCalled();
});

it('lands on the fallback when the screen was opened cold', async () => {
  mockCanGoBack.mockReturnValue(false);
  const { result } = await renderHook(() => useBack('/(tabs)'));
  result.current();
  expect(mockReplace).toHaveBeenCalledWith('/(tabs)');
  expect(mockBack).not.toHaveBeenCalled();
});

it('falls back to the root when no fallback is named', async () => {
  mockCanGoBack.mockReturnValue(false);
  const { result } = await renderHook(() => useBack());
  result.current();
  expect(mockReplace).toHaveBeenCalledWith('/');
});
