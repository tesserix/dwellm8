/**
 * The bed board of a hostel or a PG (#299).
 *
 * A warden walks the building floor by floor, so the board is grouped that
 * way. Every figure on it comes from the register: a bed the app invented is
 * a bed nobody is billed for.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsBed, OpsBedState } from '@dwellm8/mobile-shared';

export type Floor = { floor: number; beds: OpsBed[] };

export type Board = {
  loading: boolean;
  error?: string;
  beds: OpsBed[];
  floors: Floor[];
  occupied: number;
  vacant: number;
  onNotice: number;
  allocate: (bedId: string, state: OpsBedState, leaseId?: string) => Promise<void>;
  /** Put a bed in a room, which is how a new PG's board gets its first (#367). */
  add: (unitId: string, label: string, rentAmountMinor: number) => Promise<void>;
  reload: () => void;
};

function byFloor(beds: OpsBed[]): Floor[] {
  const held = new Map<number, OpsBed[]>();
  for (const b of beds) held.set(b.floor, [...(held.get(b.floor) ?? []), b]);
  return [...held.entries()]
    .sort(([a], [b]) => a - b)
    .map(([floor, on]) => ({ floor, beds: on }));
}

export function useBeds(propertyId: string): Board {
  const api = useMemo(() => apiFromEnv(), []);
  const [beds, setBeds] = useState<OpsBed[]>([]);
  const [loading, setLoading] = useState(Boolean(api));
  const [error, setError] = useState<string | undefined>();
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    setLoading(true);
    api.opsBeds(propertyId)
      .then((got) => { if (alive) { setBeds(got); setError(undefined); } })
      .catch((err: Error) => { if (alive) setError(err.message); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [api, propertyId, attempt]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  const allocate = useCallback(async (bedId: string, state: OpsBedState, leaseId?: string) => {
    // The server refuses this too; saying it here saves a round trip and puts
    // the reason where the warden tapped.
    if ((state === 'occupied' || state === 'notice') && !leaseId) {
      throw new Error('Name the tenancy this bed is let under, or nobody is billed for it.');
    }
    if (!api) throw new Error('The API is not configured on this build.');
    await api.opsAllocateBed(bedId, state, leaseId);
    setAttempt((n) => n + 1);
  }, [api]);

  const add = useCallback(async (unitId: string, label: string, rentAmountMinor: number) => {
    if (!label.trim()) throw new Error('A bed needs a label — it is what the warden allocates by.');
    if (!api) throw new Error('The API is not configured on this build.');
    await api.opsAddBed(unitId, label.trim(), rentAmountMinor);
    setAttempt((n) => n + 1);
  }, [api]);

  return {
    loading,
    error,
    beds,
    floors: useMemo(() => byFloor(beds), [beds]),
    occupied: beds.filter((b) => b.state === 'occupied').length,
    vacant: beds.filter((b) => b.state === 'vacant').length,
    onNotice: beds.filter((b) => b.state === 'notice').length,
    allocate,
    add,
    reload,
  };
}
