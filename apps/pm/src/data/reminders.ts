/**
 * What falls due next, per property (GET /v1/ops/reminders, #337).
 *
 * The API answers soonest first over the whole book; the grouping is here
 * because a manager works building by building, and a flat list of forty rows
 * from eleven buildings is a list nobody finishes reading.
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsReminder } from '@dwellm8/mobile-shared';

export type ReminderGroup = {
  id: string;
  name: string;
  locality?: string;
  reminders: OpsReminder[];
};

export type RemindersView = {
  loading: boolean;
  error?: string;
  properties: ReminderGroup[];
  duePaise: number;
  overduePaise: number;
  endingCount: number;
};

const empty: RemindersView = {
  loading: false, properties: [], duePaise: 0, overduePaise: 0, endingCount: 0,
};

export function useReminders(days?: number): RemindersView {
  const api = useMemo(() => apiFromEnv(), []);
  const [rows, setRows] = useState<OpsReminder[] | null>(null);
  const [state, setState] = useState<{ loading: boolean; error?: string }>({ loading: Boolean(api) });

  useEffect(() => {
    if (!api) {
      setState({ loading: false, error: 'The API is not configured on this build.' });
      return;
    }
    let alive = true;
    setState({ loading: true });
    api.opsReminders(days)
      .then((list) => { if (alive) { setRows(list); setState({ loading: false }); } })
      .catch((err: Error) => { if (alive) { setRows(null); setState({ loading: false, error: err.message }); } });
    return () => { alive = false; };
  }, [api, days]);

  return useMemo(() => {
    if (!rows) return { ...empty, loading: state.loading, error: state.error };

    const groups = new Map<string, ReminderGroup>();
    for (const r of rows) {
      const id = r.property_id || r.property;
      const group = groups.get(id)
        ?? { id, name: r.property, locality: r.locality, reminders: [] };
      group.reminders.push(r);
      groups.set(id, group);
    }
    for (const group of groups.values()) {
      group.reminders.sort((a, b) => a.on.localeCompare(b.on));
    }
    const properties = [...groups.values()].sort(
      (a, b) => a.reminders[0].on.localeCompare(b.reminders[0].on));

    return {
      loading: state.loading,
      error: state.error,
      properties,
      duePaise: total(rows, 'rent_due'),
      overduePaise: total(rows, 'rent_overdue'),
      endingCount: rows.filter((r) => r.kind === 'tenancy_ending').length,
    };
  }, [rows, state]);
}

function total(rows: OpsReminder[], kind: OpsReminder['kind']): number {
  return rows.reduce((sum, r) => (r.kind === kind ? sum + (r.amount_minor ?? 0) : sum), 0);
}
