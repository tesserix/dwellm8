/**
 * Where the Own app's screens get their data.
 *
 * Two modes, decided by the environment and visible in one place:
 *
 *   - live: EXPO_PUBLIC_API_URL is set, and everything below comes from the
 *     Dwellm8 API's owner surface — PostgreSQL, via services/api, never a
 *     local figure. Sections whose API does not exist yet (approvals, payouts)
 *     render empty rather than showing demonstration numbers as if real.
 *   - demo: no API configured — the §9.6 demonstration portfolio renders,
 *     exactly as before. No side effect may ever originate from it.
 */

import { useEffect, useMemo, useState } from 'react';
import {
  apiFromEnv,
  type ActivityEntry,
  type DwellmApi,
  type OwnerProperty,
  type OwnerStatement,
} from '@dwellm8/mobile-shared';
import * as demo from './mock';

export type Mode = 'live' | 'demo';

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
const MONTHS_FULL = ['January', 'February', 'March', 'April', 'May', 'June', 'July',
  'August', 'September', 'October', 'November', 'December'];

/** '2026-09-15' or RFC 3339 → '15 Sep 2026'; '' stays ''. */
export function fmtDate(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${String(d.getDate()).padStart(2, '0')} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

function toProperty(p: OwnerProperty): demo.Property {
  return {
    id: p.id,
    address: p.name,
    locality: `${p.locality}, ${p.city} ${p.pin}`.trim(),
    agency: p.agency || 'Self-managed',
    manager: '',
    managerRole: '',
    paidTo: p.billed_through_on ? fmtDate(p.billed_through_on) : '—',
    leaseExpires: p.lease_expires_on ? fmtDate(p.lease_expires_on) : '—',
    beds: 0,
    baths: 0,
    parking: 0,
    rentPaise: 0,
  };
}

function toStatement(st: OwnerStatement, n: number): demo.Statement {
  const from = new Date(st.from);
  return {
    id: `st-${st.property_id}-${st.from}`,
    n,
    date: fmtDate(st.to),
    month: MONTHS_FULL[from.getMonth()] ?? st.from,
    propertyId: st.property_id,
    netPaise: st.net_amount_minor,
    incomePaise: st.income_amount_minor,
    expensePaise: st.expense_amount_minor,
    lines: st.lines.map((l) => ({ label: l.label, paise: l.amount_minor })),
  };
}

function toActivity(e: ActivityEntry, i: number): demo.Activity {
  const kind = e.kind.includes('inspection')
    ? 'inspection'
    : e.kind.includes('statement') || e.kind.includes('invoice')
      ? 'statement'
      : 'maintenance';
  return {
    id: `act-${i}`,
    kind,
    title: e.body || e.kind.replace(/[._]/g, ' '),
    date: fmtDate(e.occurred_at),
    propertyId: '',
  };
}

/** The bar-chart palette, cycled over whatever expense labels the ledger has. */
const EXPENSE_TINTS = ['#6B6BD6', '#E48A55', '#4E6A94', '#B9C0EC', '#57B0CF', '#E8B44A'];

export type OwnerData = {
  mode: Mode;
  loading: boolean;
  error?: string;
  properties: demo.Property[];
  statements: demo.Statement[];
  activities: demo.Activity[];
  chart: { label: string; income: number; expense: number }[];
  chartMax: number;
  expenseBreakdown: { label: string; paise: number; tint: string }[];
  balance: typeof demo.balance;
  approvals: demo.Approval[];
  upNext: typeof demo.upNext | null;
};

const demoData: OwnerData = {
  mode: 'demo',
  loading: false,
  properties: demo.properties,
  statements: demo.statements,
  activities: demo.activities,
  chart: demo.chart,
  chartMax: demo.chartMax,
  expenseBreakdown: demo.expenseBreakdown,
  balance: demo.balance,
  approvals: demo.approvals,
  upNext: demo.upNext,
};

/** yyyy-MM for `monthsAgo` calendar months before now. */
function monthKey(monthsAgo: number): string {
  const d = new Date();
  d.setDate(1);
  d.setMonth(d.getMonth() - monthsAgo);
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}`;
}

async function loadLive(api: DwellmApi): Promise<OwnerData> {
  const [props, activity] = await Promise.all([
    api.ownerProperties(),
    api.ownerActivity().catch(() => [] as ActivityEntry[]),
  ]);

  // Six months of the first property's statement drives the chart and the
  // statements tab. Months the ledger has nothing for are skipped, not shown
  // as zero rows pretending something happened.
  let statements: demo.Statement[] = [];
  const chart: OwnerData['chart'] = [];
  if (props.length > 0) {
    const keys = [5, 4, 3, 2, 1, 0].map(monthKey);
    const months = await Promise.all(
      keys.map((k) => api.ownerStatement(props[0].id, k).catch(() => null)),
    );
    let n = 1;
    for (const st of months) {
      if (!st) continue;
      const from = new Date(st.from);
      chart.push({
        label: MONTHS[from.getMonth()] ?? '',
        income: st.income_amount_minor,
        expense: st.expense_amount_minor,
      });
      if (st.lines.length > 0) statements.push(toStatement(st, n++));
    }
    statements = statements.reverse(); // newest first, the way the tab reads
  }

  const byLabel = new Map<string, number>();
  for (const st of statements) {
    for (const l of st.lines) {
      if (l.paise < 0) byLabel.set(l.label, (byLabel.get(l.label) ?? 0) - l.paise);
    }
  }
  const expenseBreakdown = [...byLabel.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([label, paise], i) => ({ label, paise, tint: EXPENSE_TINTS[i % EXPENSE_TINTS.length] }));

  const moneyIn = chart.reduce((a, c) => a + c.income, 0);
  const moneyOut = chart.reduce((a, c) => a + c.expense, 0);
  const peak = Math.max(1, ...chart.map((c) => Math.max(c.income, c.expense)));

  return {
    mode: 'live',
    loading: false,
    properties: props.map(toProperty),
    statements,
    activities: activity.slice(0, 20).map(toActivity),
    chart,
    chartMax: Math.ceil(peak * 1.15),
    expenseBreakdown,
    balance: {
      net: moneyIn - moneyOut, opening: 0, moneyIn, moneyOut,
      uncleared: 0, unpaidBills: 0,
    },
    // No API serves these yet; empty is honest, demonstration figures are not.
    approvals: [],
    upNext: null,
  };
}

export type DocumentRow = { id: string; name: string; date: string };

/**
 * useOwnerDocuments lists what is on file — the GCS-backed document store in
 * live mode (per-app bucket, organisation-prefixed objects), the §9.6 samples
 * in demo mode.
 */
export function useOwnerDocuments(): { loading: boolean; documents: DocumentRow[] } {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<{ loading: boolean; documents: DocumentRow[] }>(
    api ? { loading: true, documents: [] } : { loading: false, documents: demo.documents },
  );

  useEffect(() => {
    if (!api) return;
    let alive = true;
    api
      .ownerProperties()
      .then((props) =>
        props.length
          ? api.ownerDocuments(props[0].id)
          : Promise.resolve([]),
      )
      .then((docs) => {
        if (!alive) return;
        setState({
          loading: false,
          documents: docs.map((d) => ({ id: d.id, name: d.filename, date: fmtDate(d.created_at) })),
        });
      })
      .catch(() => { if (alive) setState({ loading: false, documents: [] }); });
    return () => { alive = false; };
  }, [api]);

  return state;
}

/**
 * useOwnerData is the one hook every screen reads. It resolves the mode once
 * per mount; live data arrives in one state change, and a failure falls back
 * to an error the screen can show without losing the shell.
 */
export function useOwnerData(): OwnerData {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<OwnerData>(
    api ? { ...demoData, mode: 'live', loading: true, properties: [], statements: [], activities: [], approvals: [], upNext: null, chart: [] } : demoData,
  );

  useEffect(() => {
    if (!api) return;
    let alive = true;
    loadLive(api)
      .then((d) => { if (alive) setState(d); })
      .catch((err: Error) => {
        if (alive) setState((prev) => ({ ...prev, loading: false, error: err.message }));
      });
    return () => { alive = false; };
  }, [api]);

  return state;
}
