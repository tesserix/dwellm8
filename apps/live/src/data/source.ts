/**
 * Where the Live app's screens get their data.
 *
 * live: EXPO_PUBLIC_API_URL set — the resident surface (/v1/resident, issue
 * #51, ADR-0029): the tenancy, its dues as the ledger holds them, the payment
 * that settles them, and every receipt since move-in. Locally the API
 * impersonates DEV_IMPERSONATE_RESIDENT; in production requests carry a GIP
 * bearer for the `live` tenant.
 *
 * demo: the §9.6 sample tenancy, exactly as before. Sections with no API yet
 * (tickets, visitors, notices) render empty in live mode rather than passing
 * fiction off as record.
 */

import { useEffect, useMemo, useState } from 'react';
import {
  apiFromEnv,
  type DwellmApi,
  type ResidentPayment,
  type ResidentTenancy,
} from '@dwellm8/mobile-shared';
import * as demo from './mock';

export type Mode = 'live' | 'demo';

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export function fmtDate(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${String(d.getDate()).padStart(2, '0')} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

export type TenancyView = typeof demo.tenancy;
export type ReceiptView = demo.Receipt;

export type LiveData = {
  mode: Mode;
  loading: boolean;
  error?: string;
  leaseId?: string;
  tenancy: TenancyView;
  /** What is owed right now, from ledger postings — never a stored balance. */
  dueMinor: number;
  dueAsOf: string;
  receipts: ReceiptView[];
  /** Empty in live mode until their modules ship; the demo set otherwise. */
  tickets: typeof demo.tickets;
  notices: typeof demo.notices;
};

const demoData: LiveData = {
  mode: 'demo',
  loading: false,
  tenancy: demo.tenancy,
  dueMinor: demo.totalDue,
  dueAsOf: '',
  receipts: demo.receipts,
  tickets: demo.tickets,
  notices: demo.notices,
};

function toTenancy(t: ResidentTenancy): TenancyView {
  return {
    id: t.lease_id,
    unit: t.unit ? `${t.unit}, ${t.property}` : t.property,
    locality: `${t.locality}, ${t.city}`,
    agency: t.organisation,
    manager: '',
    rentPaise: t.rent_amount_minor,
    dueDay: t.due_day,
    paidTo: t.dues && t.dues.paid_amount_minor > 0 ? fmtDate(t.dues.as_of) : '—',
    leaseExpires: t.end_on ? fmtDate(t.end_on) : 'Open-ended',
    depositPaise: t.dues?.deposit_amount_minor ?? 0,
    noticeDays: t.notice_days,
    autopay: false,
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

/** useLiveData is the one hook every screen reads. */
export function useLiveData(): LiveData {
  const client = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<LiveData>(
    client
      ? { ...demoData, mode: 'live', loading: true, receipts: [], tickets: [], notices: [], dueMinor: 0 }
      : demoData,
  );

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
      const history = await client.residentHistory(current.lease_id).catch(() => ({ charges: [], payments: [] }));
      if (!alive) return;
      setState({
        mode: 'live',
        loading: false,
        leaseId: detail.lease_id,
        tenancy: toTenancy(detail),
        dueMinor: detail.dues?.due_amount_minor ?? 0,
        dueAsOf: detail.dues ? fmtDate(detail.dues.as_of) : '',
        receipts: history.payments
          .filter((p) => p.status === 'received' || p.status === 'succeeded' || p.receipt_number)
          .map(toReceipt),
        tickets: [],
        notices: [],
      });
    })().catch((err: Error) => {
      if (alive) setState((p) => ({ ...p, loading: false, error: err.message }));
    });
    return () => { alive = false; };
  }, [client]);

  return state;
}

/** pay starts a payment against the ledger's due — UPI through the provider
 * chain, or an offline record the landlord confirms (ADR-0011/ADR-0022). */
export async function pay(leaseId: string, amountMinor: number, method: 'upi' | 'offline'):
  Promise<{ status: string; payUrl?: string }> {
  const client = apiFromEnv();
  if (!client) return { status: 'demo' };
  const key = `live-${leaseId}-${Date.now()}`;
  const out = await client.residentPay(leaseId, amountMinor, method, key);
  return { status: out.status, payUrl: out.pay_url };
}

export function api(): DwellmApi | null {
  return apiFromEnv();
}
