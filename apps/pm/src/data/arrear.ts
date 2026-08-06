/**
 * One tenancy's position, read from the same roster the Collections tab reads
 * (#251). There is no per-tenancy arrears endpoint yet, so the row is picked
 * out of the roster rather than invented locally.
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
    api.opsArrears()
      .then((list) => {
        if (!alive) return;
        const row = list.find((a) => a.lease_id === leaseId);
        setState({
          loading: false, row,
          owes: (row?.due_amount_minor ?? 0) > 0,
          error: row ? undefined : 'That tenancy is not on this roster.',
        });
      })
      .catch((err: Error) => { if (alive) setState({ loading: false, owes: false, error: err.message }); });
    return () => { alive = false; };
  }, [api, leaseId]);

  return state;
}
