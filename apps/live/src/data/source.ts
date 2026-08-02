/**
 * Where the Live app's screens get their data: the resident surface
 * (/v1/resident, issue #51, ADR-0029) — the tenancy, its dues as the ledger
 * holds them, receipts since move-in, maintenance tickets (#245) and the
 * renter's cut of the activity feed. Locally the API impersonates
 * DEV_IMPERSONATE_RESIDENT; in production requests carry a GIP bearer for the
 * `live` tenant. Sections with no API yet (visitors, services, chat, autopay)
 * say so on their screens rather than passing fiction off as record.
 */

import { useEffect, useMemo, useState } from 'react';
import {
  apiFromEnv,
  type DwellmApi,
  type ResidentPayment,
  type ResidentTenancy,
  type ResidentTicket,
} from '@dwellm8/mobile-shared';

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export function fmtDate(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${String(d.getDate()).padStart(2, '0')} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

function fmtDateTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  const h = d.getHours() % 12 || 12;
  const ampm = d.getHours() >= 12 ? 'PM' : 'AM';
  return `${fmtDate(iso)}, ${h}:${String(d.getMinutes()).padStart(2, '0')} ${ampm}`;
}

export type TenancyView = {
  id: string;
  unit: string;
  locality: string;
  agency: string;
  rentPaise: number;
  dueDay: number;
  paidTo: string;
  leaseExpires: string;
  depositPaise: number;
  noticeDays: number;
  startOn: string;
  endOn: string;
  lockInUntil: string;
  state: string;
};

export type ReceiptView = {
  id: string; n: string; date: string; period: string; paise: number; method: string;
};

export type TicketView = {
  id: string;
  title: string;
  body?: string;
  category: string;
  status: 'Open' | 'Acknowledged' | 'Scheduled' | 'In progress' | 'Resolved' | 'Cancelled';
  raised: string;
  /** Undefined until the manager assesses it — render "being assessed", never a guess. */
  liability?: 'Owner' | 'Tenant' | 'Shared';
  liabilityReason?: string;
  slot?: string;
  vendor?: string;
  costPaise?: number;
  timeline: { at: string; what: string }[];
};

export type NoticeView = { id: string; title: string; body: string; date: string };

export const ticketCategories = [
  { code: 'plumbing', label: 'Plumbing' },
  { code: 'electrical', label: 'Electrical' },
  { code: 'carpentry', label: 'Carpentry' },
  { code: 'appliance', label: 'Appliance' },
  { code: 'pest_control', label: 'Pest control' },
  { code: 'cleaning', label: 'Cleaning' },
  { code: 'common_area', label: 'Common area' },
  { code: 'other', label: 'Other' },
];

/** The ways a tenant can settle rent today. UPI through the provider chain,
 * or an offline record the landlord confirms (ADR-0011/ADR-0022). */
export const payMethods = [
  { id: 'upi', label: 'UPI', hint: 'GPay, PhonePe, Paytm, any UPI app', feePaise: 0 },
  { id: 'offline', label: 'I paid by cash or transfer', hint: 'Record it for your landlord to confirm', feePaise: 0 },
];

const emptyTenancy: TenancyView = {
  id: '', unit: '', locality: '', agency: '', rentPaise: 0, dueDay: 0,
  paidTo: '—', leaseExpires: '—', depositPaise: 0, noticeDays: 0,
  startOn: '', endOn: '', lockInUntil: '', state: '',
};

export type LiveData = {
  loading: boolean;
  error?: string;
  leaseId?: string;
  tenancy: TenancyView;
  /** What is owed right now, from ledger postings — never a stored balance. */
  dueMinor: number;
  dueAsOf: string;
  receipts: ReceiptView[];
  tickets: TicketView[];
  notices: NoticeView[];
};

function toTenancy(t: ResidentTenancy): TenancyView {
  return {
    id: t.lease_id,
    unit: t.unit ? `${t.unit}, ${t.property}` : t.property,
    locality: `${t.locality}, ${t.city}`,
    agency: t.organisation,
    rentPaise: t.rent_amount_minor,
    dueDay: t.due_day,
    paidTo: t.dues && t.dues.paid_amount_minor > 0 ? fmtDate(t.dues.as_of) : '—',
    leaseExpires: t.end_on ? fmtDate(t.end_on) : 'Open-ended',
    depositPaise: t.dues?.deposit_amount_minor ?? 0,
    noticeDays: t.notice_days,
    startOn: fmtDate(t.start_on),
    endOn: t.end_on ? fmtDate(t.end_on) : '',
    lockInUntil: t.lock_in_until ? fmtDate(t.lock_in_until) : '',
    state: t.state,
  };
}

function toReceipt(p: ResidentPayment, i: number): ReceiptView {
  return {
    id: p.payment_id,
    n: p.receipt_number || `PMT-${i + 1}`,
    date: fmtDate(p.received_at || p.created_at),
    period: '',
    paise: p.amount_minor,
    method: p.method.toUpperCase(),
  };
}

const TICKET_STATUS: Record<string, TicketView['status']> = {
  open: 'Open', acknowledged: 'Acknowledged', scheduled: 'Scheduled',
  in_progress: 'In progress', resolved: 'Resolved', cancelled: 'Cancelled',
};

const TICKET_LIABILITY: Record<string, TicketView['liability']> = {
  owner: 'Owner', tenant: 'Tenant', shared: 'Shared',
};

export function toTicket(t: ResidentTicket): TicketView {
  const label = ticketCategories.find((c) => c.code === t.category)?.label ?? t.category;
  return {
    id: t.ticket_id,
    title: t.title,
    body: t.body,
    category: label,
    status: TICKET_STATUS[t.status] ?? 'Open',
    raised: fmtDate(t.raised_at),
    liability: t.liability ? TICKET_LIABILITY[t.liability] : undefined,
    liabilityReason: t.liability_reason || undefined,
    slot: t.slot || undefined,
    vendor: t.vendor || undefined,
    costPaise: t.cost_minor,
    timeline: (t.timeline ?? []).map((e) => ({ at: fmtDateTime(e.at), what: e.body })),
  };
}

/** The activity feed rendered as notices: the fact and its day. */
function toNotice(e: { kind: string; occurred_at: string; body?: string }, i: number): NoticeView {
  const title = e.kind.replace(/[._]/g, ' ').replace(/^\w/, (c) => c.toUpperCase());
  return { id: `a-${i}`, title, body: e.body ?? '', date: fmtDate(e.occurred_at) };
}

/* Screens that mutate call refreshLive(); every mounted useLiveData refetches. */
let version = 0;
const listeners = new Set<() => void>();
export function refreshLive() {
  version++;
  listeners.forEach((l) => l());
}

const empty: LiveData = {
  loading: true, tenancy: emptyTenancy, dueMinor: 0, dueAsOf: '',
  receipts: [], tickets: [], notices: [],
};

/** useLiveData is the one hook every screen reads. */
export function useLiveData(): LiveData {
  const client = useMemo(() => apiFromEnv(), []);
  const [v, setV] = useState(version);
  const [state, setState] = useState<LiveData>(
    client ? empty : { ...empty, loading: false, error: 'The API is not configured on this build.' },
  );

  useEffect(() => {
    const bump = () => setV(version);
    listeners.add(bump);
    return () => { listeners.delete(bump); };
  }, []);

  useEffect(() => {
    if (!client) return;
    let alive = true;
    (async () => {
      const tenancies = await client.residentTenancies();
      const current = tenancies.find((t) => t.live) ?? tenancies[0];
      if (!current) {
        if (alive) setState((p) => ({ ...p, loading: false, error: 'No tenancy on this sign-in yet.' }));
        return;
      }
      const detail = await client.residentTenancy(current.lease_id);
      const [history, tickets, activity] = await Promise.all([
        client.residentHistory(current.lease_id).catch(() => ({ charges: [], payments: [] })),
        client.residentTickets(current.lease_id).catch(() => []),
        client.residentActivity(current.lease_id).catch(() => []),
      ]);
      if (!alive) return;
      setState({
        loading: false,
        leaseId: detail.lease_id,
        tenancy: toTenancy(detail),
        dueMinor: detail.dues?.due_amount_minor ?? 0,
        dueAsOf: detail.dues ? fmtDate(detail.dues.as_of) : '',
        receipts: history.payments
          .filter((p) => p.status === 'received' || p.status === 'succeeded' || p.receipt_number)
          .map(toReceipt),
        tickets: tickets.map(toTicket),
        notices: activity.slice(0, 5).map(toNotice),
      });
    })().catch((err: Error) => {
      if (alive) setState((p) => ({ ...p, loading: false, error: err.message }));
    });
    return () => { alive = false; };
  }, [client, v]);

  return state;
}

/** One ticket with its timeline, for the detail screen. */
export function useTicket(leaseId: string | undefined, ticketId: string | undefined):
  { loading: boolean; error?: string; ticket?: TicketView } {
  const client = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<{ loading: boolean; error?: string; ticket?: TicketView }>(
    { loading: true },
  );

  useEffect(() => {
    if (!client || !leaseId || !ticketId) {
      setState({ loading: false, error: !client ? 'The API is not configured on this build.' : undefined });
      return;
    }
    let alive = true;
    client.residentTicket(leaseId, ticketId)
      .then((t) => { if (alive) setState({ loading: false, ticket: toTicket(t) }); })
      .catch((err: Error) => { if (alive) setState({ loading: false, error: err.message }); });
    return () => { alive = false; };
  }, [client, leaseId, ticketId]);

  return state;
}

/** The signed-in renter, for the profile screen. */
export function useMe(): { loading: boolean; phone: string; email: string } {
  const client = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState({ loading: !!client, phone: '', email: '' });

  useEffect(() => {
    if (!client) return;
    let alive = true;
    client.residentMe()
      .then((me) => { if (alive) setState({ loading: false, phone: me.phone ?? '', email: me.email ?? '' }); })
      .catch(() => { if (alive) setState((p) => ({ ...p, loading: false })); });
    return () => { alive = false; };
  }, [client]);

  return state;
}

/** raiseTicket records a repair request and refreshes every mounted screen. */
export async function raiseTicket(leaseId: string, req: { category: string; title: string; body?: string }):
  Promise<TicketView> {
  const client = apiFromEnv();
  if (!client) throw new Error('The API is not configured on this build.');
  const out = await client.residentRaiseTicket(leaseId, req);
  refreshLive();
  return toTicket(out);
}

/** pay starts a payment against the ledger's due — UPI through the provider
 * chain, or an offline record the landlord confirms (ADR-0011/ADR-0022). */
export async function pay(leaseId: string, amountMinor: number, method: 'upi' | 'offline'):
  Promise<{ status: string; payUrl?: string }> {
  const client = apiFromEnv();
  if (!client) throw new Error('The API is not configured on this build.');
  const key = `live-${leaseId}-${Date.now()}`;
  const out = await client.residentPay(leaseId, amountMinor, method, key);
  refreshLive();
  return { status: out.status, payUrl: out.pay_url };
}

export function api(): DwellmApi | null {
  return apiFromEnv();
}
