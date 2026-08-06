/**
 * Recording rent that arrived outside the app — cash at the door, a cheque, a
 * transfer the manager watched land (#297).
 *
 * The idempotency key is fixed for the life of the screen rather than per tap:
 * a double tap, a slow line and a retry all resolve to the one receipt.
 */

import { useCallback, useMemo, useRef, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsCollection } from '@dwellm8/mobile-shared';

export type CollectionMethod = 'offline_cash' | 'offline_cheque' | 'offline_transfer';

export type Recorder = {
  record: (amountMinor: number, method: CollectionMethod, reference: string) => Promise<void>;
  saving: boolean;
  error?: string;
  result?: OpsCollection;
};

function newKey(): string {
  return `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 10)}`;
}

export function useRecordCollection(leaseId: string): Recorder {
  const api = useMemo(() => apiFromEnv(), []);
  const key = useRef(newKey());
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [result, setResult] = useState<OpsCollection | undefined>();

  const record = useCallback(async (amountMinor: number, method: CollectionMethod, reference: string) => {
    if (!api) {
      setError('The API is not configured on this build.');
      return;
    }
    setSaving(true);
    setError(undefined);
    try {
      setResult(await api.opsRecordCollection(leaseId, {
        amount_minor: amountMinor, method, reference,
        idempotency_key: key.current,
      }));
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSaving(false);
    }
  }, [api, leaseId]);

  return { record, saving, error, result };
}
