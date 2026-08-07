/**
 * The property record: one building, its lettable units, and who is in them
 * (GET /v1/ops/properties/{id}). Live only — a manager standing in a lobby
 * reading somebody else's flat numbers is the failure this replaces (#251).
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv, type OpsProperty, type OpsUnit } from '@dwellm8/mobile-shared';

export type PropertyRecord = {
  loading: boolean;
  error?: string;
  property?: OpsProperty;
  units: OpsUnit[];
  occupied: number;
  vacant: number;
  /** Empty today, let from a date ahead — not free to offer to anybody else. */
  spokenFor: number;
  rentRollPaise: number;
};

const nothing: PropertyRecord = {
  loading: false, units: [], occupied: 0, vacant: 0, spokenFor: 0, rentRollPaise: 0,
};

export function usePropertyRecord(id: string | undefined): PropertyRecord {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<PropertyRecord>({ ...nothing, loading: Boolean(api && id) });

  useEffect(() => {
    if (!api) {
      setState({ ...nothing, error: 'The API is not configured on this build.' });
      return;
    }
    if (!id) {
      setState({ ...nothing, error: 'No property was named.' });
      return;
    }
    let alive = true;
    setState({ ...nothing, loading: true });
    api.opsProperty(id)
      .then(({ property, units }) => {
        if (!alive) return;
        const let_ = units.filter((u) => Boolean(u.lease_id) && !u.let_from);
        const ahead = units.filter((u) => Boolean(u.let_from));
        setState({
          loading: false,
          property,
          units,
          occupied: let_.length,
          vacant: units.length - let_.length,
          spokenFor: ahead.length,
          rentRollPaise: let_.reduce((a, u) => a + (u.rent_amount_minor ?? 0), 0),
        });
      })
      .catch((err: Error) => { if (alive) setState({ ...nothing, error: err.message }); });
    return () => { alive = false; };
  }, [api, id]);

  return state;
}
