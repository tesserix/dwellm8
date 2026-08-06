/**
 * Where the Ops app's Today screen gets its numbers.
 *
 * Same two modes as the Own app's source.ts, for the same reason: live when
 * EXPO_PUBLIC_API_URL is set, the §9.6 demonstration figures otherwise, and
 * the switch visible in one place.
 *
 * Only the roster's real facts are wired — rent roll, what is outstanding
 * against it, and how many tenancies are in arrears (GET /v1/ops/today,
 * /v1/ops/arrears). Payout runs, maintenance tickets, inspections and
 * occupancy have no schema behind them yet (internal/surface/ops's own package
 * comment says so), so in live mode they read zero rather than borrowing the
 * demonstration set's figures — #284, where a real manager was shown 88%
 * collected against a rent roll that was not theirs.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv, type DwellmApi, type OpsToday } from '@dwellm8/mobile-shared';
import * as demo from './mock';

export type Mode = 'live' | 'demo';

export type OpsTodayData = {
  mode: Mode;
  loading: boolean;
  error?: string;
  /** Ask again after a failure — an outage should not need a relaunch. */
  reload: () => void;
  /** Real when live: rent roll, what's outstanding, tenancies in arrears. */
  billedPaise: number;
  outstandingPaise: number;
  arrearsCount: number;
  activeTenancies: number;
  /** Demonstration-only in both modes today — see the file comment. */
  payoutsPending: number;
  payoutsPaise: number;
  openTickets: number;
  breachingSla: number;
  visitsDone: number;
  inspectionsToday: number;
  occupancyPct: number;
  vacantUnits: number;
};

const demoData: OpsTodayData = {
  mode: 'demo',
  loading: false,
  reload: () => {},
  billedPaise: demo.today.targetPaise,
  outstandingPaise: demo.today.arrearsPaise,
  arrearsCount: demo.today.arrearsCount,
  activeTenancies: 0,
  payoutsPending: demo.today.payoutsPending,
  payoutsPaise: demo.today.payoutsPaise,
  openTickets: demo.today.openTickets,
  breachingSla: demo.today.breachingSla,
  visitsDone: demo.today.visitsDone,
  inspectionsToday: demo.today.inspectionsToday,
  occupancyPct: demo.today.occupancyPct,
  vacantUnits: demo.today.vacantUnits,
};

// Live mode never borrows a figure from the demonstration set: a manager who
// acts on ₹21,07,000 that is not theirs is worse off than one shown nothing
// (#284). The fields with no schema behind them read zero until there is one.
const liveNothingYet: OpsTodayData = {
  ...demoData,
  mode: 'live',
  billedPaise: 0, outstandingPaise: 0, arrearsCount: 0, activeTenancies: 0,
  payoutsPending: 0, payoutsPaise: 0, openTickets: 0, breachingSla: 0,
  visitsDone: 0, inspectionsToday: 0, occupancyPct: 0, vacantUnits: 0,
};

async function loadLive(api: DwellmApi): Promise<OpsTodayData> {
  const [t, tickets] = await Promise.all([
    api.opsToday(),
    api.opsTickets(false).catch(() => []),
  ] as const) as [OpsToday, Awaited<ReturnType<DwellmApi['opsTickets']>>];
  return {
    ...liveNothingYet,
    loading: false,
    billedPaise: t.rent_roll_amount_minor,
    outstandingPaise: t.outstanding_amount_minor,
    arrearsCount: t.tenancies_in_arrears,
    activeTenancies: t.active_tenancies,
    openTickets: tickets.length,
  };
}

/** useOpsTodayData is what the Today screen's headline card reads. */
export function useOpsTodayData(): OpsTodayData {
  const api = useMemo(() => apiFromEnv(), []);
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const [state, setState] = useState<OpsTodayData>(
    api ? { ...liveNothingYet, loading: true } : demoData,
  );

  useEffect(() => {
    if (!api) return;
    let alive = true;
    setState((prev) => ({ ...prev, loading: true, error: undefined }));
    loadLive(api)
      .then((d) => { if (alive) setState(d); })
      .catch((err: Error) => {
        if (alive) setState({ ...liveNothingYet, loading: false, error: err.message });
      });
    return () => { alive = false; };
  }, [api, attempt]);

  return useMemo(() => ({ ...state, reload }), [state, reload]);
}

/** Whose firm this is and who is signed into it. Demonstration names are a
 * lie on a real manager's own screen, so live mode reads both. */
export type OpsWho = { mode: Mode; firmName: string; firstName: string };

const demoWho: OpsWho = {
  mode: 'demo',
  firmName: demo.org.portfolios[0].name,
  firstName: demo.staff.name.split(' ')[0],
};

export function useOpsWho(): OpsWho {
  const api = useMemo(() => apiFromEnv(), []);
  const [who, setWho] = useState<OpsWho>(api ? { ...demoWho, mode: 'live', firmName: '', firstName: '' } : demoWho);

  useEffect(() => {
    if (!api) return;
    let alive = true;
    Promise.all([api.opsRegistration().catch(() => null), api.me().catch(() => null)])
      .then(([reg, me]) => {
        if (!alive) return;
        const name = me?.display_name?.trim() ?? '';
        setWho({
          mode: 'live',
          firmName: reg?.trade_name?.trim() || reg?.legal_name?.trim() || '',
          firstName: name ? name.split(' ')[0] : '',
        });
      });
    return () => { alive = false; };
  }, [api]);

  return who;
}

/** One thing on the manager's worklist. */
export type OpsTask = {
  id: string;
  kind: 'collect' | 'ticket' | 'approval';
  ref: string;
  title: string;
  where: string;
  at: string;
  urgent: boolean;
};

export type OpsWorklist = { mode: Mode; loading: boolean; tasks: OpsTask[] };

const demoTasks: OpsTask[] = demo.worklist.map((t) => ({
  id: t.id, kind: t.kind as OpsTask['kind'], ref: t.ref,
  title: t.title, where: t.where, at: t.at, urgent: Boolean(t.urgent),
}));

async function loadWorklist(api: DwellmApi): Promise<OpsTask[]> {
  const [arrears, tickets, approvals] = await Promise.all([
    api.opsArrears().catch(() => []),
    api.opsTickets(false).catch(() => []),
    api.opsApprovals().catch(() => []),
  ]);
  return [
    ...arrears.map((a) => ({
      id: `collect-${a.lease_id}`, kind: 'collect' as const, ref: a.lease_id,
      title: `Collect ${inrRough(a.due_amount_minor)}`,
      where: `${a.property} · ${a.unit}`, at: a.as_of, urgent: true,
    })),
    ...tickets.map((t) => ({
      id: `ticket-${t.ticket_id}`, kind: 'ticket' as const, ref: t.ticket_id,
      title: t.title, where: [t.property, t.unit].filter(Boolean).join(' · '),
      at: t.status, urgent: false,
    })),
    ...approvals.map((a) => ({
      id: `approval-${a.id}`, kind: 'approval' as const, ref: a.subject_id,
      title: `${a.action} — ${inrRough(a.amount_minor)}`,
      where: a.automation, at: 'waiting', urgent: false,
    })),
  ];
}

// Whole rupees, because a worklist row is a glance rather than a statement.
const inrRough = (minor: number) => `₹${Math.round(minor / 100).toLocaleString('en-IN')}`;

export function useOpsWorklist(): OpsWorklist {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<OpsWorklist>(
    api ? { mode: 'live', loading: true, tasks: [] } : { mode: 'demo', loading: false, tasks: demoTasks },
  );

  useEffect(() => {
    if (!api) return;
    let alive = true;
    loadWorklist(api)
      .then((tasks) => { if (alive) setState({ mode: 'live', loading: false, tasks }); })
      .catch(() => { if (alive) setState({ mode: 'live', loading: false, tasks: [] }); });
    return () => { alive = false; };
  }, [api]);

  return state;
}
