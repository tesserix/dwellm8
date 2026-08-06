/**
 * What the manager is holding on somebody else's behalf (#270). One collection
 * is divided once, at capture, and this is that division read back — the screen
 * never recomputes a share.
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv, type Settlement } from '@dwellm8/mobile-shared';

type Load<T> = { loading: boolean; error?: string; data: T };

let version = 0;
const listeners = new Set<() => void>();

export function refreshSettlements() {
  version++;
  listeners.forEach((l) => l());
}

export function useSettlements(): Load<Settlement[]> {
  const api = useMemo(() => apiFromEnv(), []);
  const [v, setV] = useState(version);
  const [state, setState] = useState<Load<Settlement[]>>({ loading: !!api, data: [] });

  useEffect(() => {
    const bump = () => setV(version);
    listeners.add(bump);
    return () => { listeners.delete(bump); };
  }, []);

  useEffect(() => {
    if (!api) {
      setState({ loading: false, data: [], error: 'The API is not configured on this build.' });
      return;
    }
    let alive = true;
    api.opsSettlements()
      .then((data) => { if (alive) setState({ loading: false, data }); })
      .catch((err: Error) => { if (alive) setState((p) => ({ ...p, loading: false, error: err.message })); });
    return () => { alive = false; };
  }, [api, v]);

  return state;
}

export async function releaseSettlement(id: string, beneficiaryRef: string): Promise<Settlement> {
  const api = apiFromEnv();
  if (!api) throw new Error('The API is not configured on this build.');
  const out = await api.opsReleaseSettlement(id, beneficiaryRef);
  refreshSettlements();
  return out;
}
