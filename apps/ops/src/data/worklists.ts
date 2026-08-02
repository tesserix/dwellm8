/**
 * The manager's worklists: tickets (#237), the tenancy conversations and the
 * gate (#238) — real reads over /v1/ops. SLA clocks, priorities and the
 * vendor panel are not modelled server-side yet, so nothing here pretends
 * they are.
 */

import { useEffect, useMemo, useState } from 'react';
import {
  apiFromEnv,
  type OpsMessage,
  type OpsPass,
  type OpsThread,
  type OpsTicket,
} from '@dwellm8/mobile-shared';

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export function fmtDate(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${String(d.getDate()).padStart(2, '0')} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

export function fmtTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const h = d.getHours() % 12 || 12;
  const ampm = d.getHours() >= 12 ? 'PM' : 'AM';
  return `${h}:${String(d.getMinutes()).padStart(2, '0')} ${ampm}`;
}

/* Screens that mutate call refreshWorklists(); every mounted hook refetches. */
let version = 0;
const listeners = new Set<() => void>();
export function refreshWorklists() {
  version++;
  listeners.forEach((l) => l());
}

function useVersion(): number {
  const [v, setV] = useState(version);
  useEffect(() => {
    const bump = () => setV(version);
    listeners.add(bump);
    return () => { listeners.delete(bump); };
  }, []);
  return v;
}

type Load<T> = { loading: boolean; error?: string; data: T };

function useLoad<T>(empty: T, load: (() => Promise<T>) | null, deps: unknown[]): Load<T> {
  const [state, setState] = useState<Load<T>>({ loading: !!load, data: empty });
  useEffect(() => {
    if (!load) {
      setState({ loading: false, data: empty, error: 'The API is not configured on this build.' });
      return;
    }
    let alive = true;
    load()
      .then((data) => { if (alive) setState({ loading: false, data }); })
      .catch((err: Error) => { if (alive) setState((p) => ({ ...p, loading: false, error: err.message })); });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return state;
}

export const TICKET_STATUS_LABEL: Record<string, string> = {
  open: 'Open', acknowledged: 'Acknowledged', scheduled: 'Scheduled',
  in_progress: 'In progress', resolved: 'Resolved', cancelled: 'Cancelled',
};

export function useOpsTickets(settled: boolean): Load<OpsTicket[]> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  return useLoad<OpsTicket[]>([], api ? () => api.opsTickets(settled) : null, [api, settled, v]);
}

export function useOpsTicket(id: string | undefined): Load<OpsTicket | null> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  return useLoad<OpsTicket | null>(null, api && id ? () => api.opsTicket(id) : null, [api, id, v]);
}

export async function advanceTicket(id: string, req: {
  action: string; slot?: string; vendor?: string;
  liability?: string; liability_reason?: string; cost_minor?: number; note?: string;
}): Promise<void> {
  const api = apiFromEnv();
  if (!api) throw new Error('The API is not configured on this build.');
  await api.opsAdvanceTicket(id, req);
  refreshWorklists();
}

export function useOpsThreads(): Load<OpsThread[]> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  return useLoad<OpsThread[]>([], api ? () => api.opsThreads() : null, [api, v]);
}

export function useOpsThread(leaseId: string | undefined):
  Load<OpsMessage[]> & { send: (body: string) => Promise<void> } {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<Load<OpsMessage[]>>({ loading: true, data: [] });

  useEffect(() => {
    if (!api || !leaseId) {
      setState({ loading: false, data: [], error: !api ? 'The API is not configured on this build.' : undefined });
      return;
    }
    let alive = true;
    const load = () =>
      api.opsMessages(leaseId)
        .then((ms) => { if (alive) setState({ loading: false, data: ms }); })
        .catch((err: Error) => { if (alive) setState((p) => ({ ...p, loading: false, error: err.message })); });
    load();
    const poll = setInterval(load, 15000);
    return () => { alive = false; clearInterval(poll); };
  }, [api, leaseId]);

  const send = async (body: string) => {
    if (!api || !leaseId) throw new Error('The API is not configured on this build.');
    await api.opsSendMessage(leaseId, body);
    const ms = await api.opsMessages(leaseId);
    setState({ loading: false, data: ms });
  };

  return { ...state, send };
}

export function useOpsPasses(): Load<OpsPass[]> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  return useLoad<OpsPass[]>([], api ? () => api.opsPasses() : null, [api, v]);
}

export async function setPassState(id: string, state: 'arrived' | 'inside' | 'left' | 'denied'): Promise<void> {
  const api = apiFromEnv();
  if (!api) throw new Error('The API is not configured on this build.');
  await api.opsSetPassState(id, state);
  refreshWorklists();
}
