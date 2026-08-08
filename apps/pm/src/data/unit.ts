/**
 * One flat, opened from the property record (GET /v1/ops/units/{id}, #338).
 *
 * Bedrooms are read off the advert: the register models rooms as units rather
 * than as a count on the flat, so the listing is the only place the system
 * knows a 2 BHK is a 2 BHK. A flat nobody advertised has no count, and this
 * says so rather than guessing one.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type {
  OpsProperty, OpsUnitDetail, OpsUnitListing, OpsUnitTenancy,
} from '@dwellm8/mobile-shared';

export type UnitView = {
  loading: boolean;
  error?: string;
  unit?: OpsUnitDetail;
  property?: OpsProperty;
  tenancy?: OpsUnitTenancy;
  listing?: OpsUnitListing;
  ancillaries: { id: string; code: string; kind: string }[];
  bedrooms?: number;
  /** Read the flat again — what the screen was editing may have changed (#366). */
  reload: () => void;
};

type UnitState = Omit<UnitView, 'reload'>;

const nothing: UnitState = { loading: false, ancillaries: [] };

export function useUnitRecord(id: string | undefined): UnitView {
  const api = useMemo(() => apiFromEnv(), []);
  const [attempt, setAttempt] = useState(0);
  const reload = useCallback(() => setAttempt((n) => n + 1), []);
  const [state, setState] = useState<UnitState>({ ...nothing, loading: Boolean(api && id) });

  useEffect(() => {
    if (!api) {
      setState({ ...nothing, error: 'The API is not configured on this build.' });
      return;
    }
    if (!id) {
      setState({ ...nothing, error: 'No unit was named.' });
      return;
    }
    let alive = true;
    setState((prev) => ({ ...prev, loading: true, error: undefined }));
    api.opsUnit(id)
      .then((r) => {
        if (!alive) return;
        setState({
          loading: false,
          unit: r.unit,
          property: r.property,
          tenancy: r.tenancy,
          listing: r.listing,
          ancillaries: r.ancillaries,
          bedrooms: r.listing?.bedrooms,
        });
      })
      .catch((err: Error) => { if (alive) setState({ ...nothing, error: err.message }); });
    return () => { alive = false; };
  }, [api, id, attempt]);

  return useMemo(() => ({ ...state, reload }), [state, reload]);
}
