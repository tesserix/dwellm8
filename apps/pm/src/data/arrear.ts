/**
 * One tenancy's position (GET /v1/ops/tenancies/{lease}/position, #251).
 * Asked for directly: the arrears list carries only tenancies in arrears, so a
 * settled one is not on it and searching it would report a live tenancy as
 * missing (#306).
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsArrear } from '@dwellm8/mobile-shared';

export type ArrearView = {
  loading: boolean;
  error?: string;
  row?: OpsArrear;
  owes: boolean;
};

export function useArrear(leaseId: string | undefined): ArrearView {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<ArrearView>({ loading: Boolean(api && leaseId), owes: false });

  useEffect(() => {
    if (!api || !leaseId) {
      setState({ loading: false, owes: false, error: api ? undefined : 'The API is not configured on this build.' });
      return;
    }
    let alive = true;
    api.opsPosition(leaseId)
      .then((row) => {
        if (!alive) return;
        setState({ loading: false, row, owes: (row?.due_amount_minor ?? 0) > 0 });
      })
      .catch((err: Error) => { if (alive) setState({ loading: false, owes: false, error: err.message }); });
    return () => { alive = false; };
  }, [api, leaseId]);

  return state;
}
