/**
 * Where the Ops app's Today screen gets its numbers.
 *
 * Same two modes as the Own app's source.ts, for the same reason: live when
 * EXPO_PUBLIC_API_URL is set, the §9.6 demonstration figures otherwise, and
 * the switch visible in one place.
 *
 * Only the roster's real facts are wired — rent roll, what is outstanding
 * against it, and how many tenancies are in arrears (GET /v1/ops/today,
 * /v1/ops/arrears). Everything the mock's `today` export calls a payout, an
 * open job, an inspection or an occupancy percentage has no schema behind it
 * yet (no maintenance-ticket, payout-run or vacancy tracking exists as of
 * this surface — internal/surface/ops's own package comment says so), so
 * those figures keep rendering the demonstration data rather than a number
 * this system cannot actually stand behind.
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv, type DwellmApi, type OpsToday } from '@dwellm8/mobile-shared';
import * as demo from './mock';

export type Mode = 'live' | 'demo';

export type OpsTodayData = {
  mode: Mode;
  loading: boolean;
  error?: string;
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

async function loadLive(api: DwellmApi): Promise<OpsTodayData> {
  const t: OpsToday = await api.opsToday();
  return {
    ...demoData,
    mode: 'live',
    loading: false,
    billedPaise: t.rent_roll_amount_minor,
    outstandingPaise: t.outstanding_amount_minor,
    arrearsCount: t.tenancies_in_arrears,
    activeTenancies: t.active_tenancies,
  };
}

/** useOpsTodayData is what the Today screen's headline card reads. */
export function useOpsTodayData(): OpsTodayData {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<OpsTodayData>(
    api ? { ...demoData, mode: 'live', loading: true } : demoData,
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
