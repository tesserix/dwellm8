/**
 * The properties under this scope (GET /v1/ops/properties). Readable again on
 * demand: a property onboarded one screen away must be here when the manager
 * comes back, without relaunching the app (#289).
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv, type OpsProperty } from '@dwellm8/mobile-shared';

export type Portfolio = {
  loading: boolean;
  error?: string;
  rows: OpsProperty[];
  units: number;
  reload: () => void;
};

export function usePortfolio(): Portfolio {
  const api = useMemo(() => apiFromEnv(), []);
  const [rows, setRows] = useState<OpsProperty[]>([]);
  const [loading, setLoading] = useState(Boolean(api));
  const [error, setError] = useState<string | undefined>();
  const [round, setRound] = useState(0);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    setLoading(true);
    api.opsProperties()
      .then((list) => { if (alive) { setRows(list); setError(undefined); setLoading(false); } })
      .catch((err: Error) => { if (alive) { setRows([]); setError(err.message); setLoading(false); } });
    return () => { alive = false; };
  }, [api, round]);

  const reload = useCallback(() => setRound((n) => n + 1), []);

  return { loading, error, rows, units: rows.reduce((a, p) => a + p.unit_count, 0), reload };
}
