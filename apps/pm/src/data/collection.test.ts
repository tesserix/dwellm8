import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useRecordCollection } from './collection';

// Rent taken at the door. The manager taps once; a slow line, a double tap or
// a retry must not turn one ₹25,000 into two (#297).

const mockRecord = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsRecordCollection: mockRecord } : null),
}));

const captured = {
  payment_id: 'pay-1', lease_id: 'l1', status: 'captured', amount_minor: 2500000,
  method: 'offline_cash', due_amount_minor: 0, advance_amount_minor: 0,
};

describe('useRecordCollection', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockRecord.mockReset();
  });

  it('records the payment against the tenancy and keeps what came back', async () => {
    mockRecord.mockResolvedValue(captured);
    const { result } = await renderHook(() => useRecordCollection('l1'));

    await act(async () => { await result.current.record(2500000, 'offline_cash', 'receipt book 41'); });

    expect(mockRecord).toHaveBeenCalledWith('l1', expect.objectContaining({
      amount_minor: 2500000, method: 'offline_cash', reference: 'receipt book 41',
    }));
    expect(result.current.result?.payment_id).toBe('pay-1');
    expect(result.current.saving).toBe(false);
    expect(result.current.error).toBeUndefined();
  });

  it('sends the same key twice, so a double tap is one receipt', async () => {
    mockRecord.mockResolvedValue(captured);
    const { result } = await renderHook(() => useRecordCollection('l1'));

    await act(async () => {
      await Promise.all([
        result.current.record(2500000, 'offline_cash', ''),
        result.current.record(2500000, 'offline_cash', ''),
      ]);
    });

    const [first, second] = mockRecord.mock.calls;
    expect(second[1].idempotency_key).toBe(first[1].idempotency_key);
  });

  it('surfaces the server’s own refusal rather than a generic one', async () => {
    mockRecord.mockRejectedValue(new Error('that tenancy has nobody to credit the payment to'));
    const { result } = await renderHook(() => useRecordCollection('l1'));

    await act(async () => { await result.current.record(1000, 'offline_cash', ''); });

    await waitFor(() =>
      expect(result.current.error).toBe('that tenancy has nobody to credit the payment to'));
    expect(result.current.result).toBeUndefined();
  });

  it('refuses without an API rather than pretending the money was banked', async () => {
    delete process.env.EXPO_PUBLIC_API_URL;
    const { result } = await renderHook(() => useRecordCollection('l1'));

    await act(async () => { await result.current.record(1000, 'offline_cash', ''); });

    expect(mockRecord).not.toHaveBeenCalled();
    expect(result.current.error).toBe('The API is not configured on this build.');
  });
});
