/**
 * Where the manager's own rent settles (#269). Dwellm8 never pools client
 * money: the account belongs to the manager, with their own aggregator, so
 * every read here is of their organisation's own row.
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv, type ConnectMerchant, type MerchantAccount } from '@dwellm8/mobile-shared';

type Load<T> = { loading: boolean; error?: string; data: T };

let version = 0;
const listeners = new Set<() => void>();

export function refreshMerchants() {
  version++;
  listeners.forEach((l) => l());
}

export function useMerchants(): Load<MerchantAccount[]> {
  const api = useMemo(() => apiFromEnv(), []);
  const [v, setV] = useState(version);
  const [state, setState] = useState<Load<MerchantAccount[]>>({ loading: !!api, data: [] });

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
    api.opsMerchants()
      .then((data) => { if (alive) setState({ loading: false, data }); })
      .catch((err: Error) => { if (alive) setState((p) => ({ ...p, loading: false, error: err.message })); });
    return () => { alive = false; };
  }, [api, v]);

  return state;
}

function client() {
  const api = apiFromEnv();
  if (!api) throw new Error('The API is not configured on this build.');
  return api;
}

export async function connectMerchant(a: ConnectMerchant): Promise<MerchantAccount> {
  const out = await client().opsConnectMerchant(a);
  refreshMerchants();
  return out;
}

export async function refreshMerchant(provider: string): Promise<MerchantAccount> {
  const out = await client().opsRefreshMerchant(provider);
  refreshMerchants();
  return out;
}
