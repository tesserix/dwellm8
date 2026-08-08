/**
 * What is near a property (#354): schools, transport, hospitals, and how far.
 *
 * A renter asks which school and how far before they ask the rent, so the
 * manager records the walk they measured and the app never guesses it.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsPlace, OpsPlaceDraft } from '@dwellm8/mobile-shared';

/** A place with the distance said the way a person says it. */
export type NearbyPlace = OpsPlace & { away: string };

export type NearbyGroup = { category: string; places: NearbyPlace[] };

export type Nearby = {
  loading: boolean;
  error?: string;
  places: NearbyPlace[];
  groups: NearbyGroup[];
  add: (place: OpsPlaceDraft) => Promise<void>;
  remove: (placeId: string) => Promise<void>;
  reload: () => void;
};

// The kinds a renter scans for first, in that order; anything else follows.
const order = ['school', 'college', 'childcare', 'coaching', 'metro', 'bus', 'railway',
  'airport', 'hospital', 'clinic', 'pharmacy', 'market', 'supermarket', 'mall', 'park'];

// A comfortable walk is about 80 metres a minute — the figure a listing quotes.
function away(p: OpsPlace): string {
  const far = p.distance_m >= 1000
    ? `${(p.distance_m / 1000).toFixed(1)} km`
    : `${p.distance_m} m`;
  if (p.travel_mode !== 'walk') return `${far} · ${p.travel_mode}`;
  return `${far} · ${Math.max(1, Math.round(p.distance_m / 80))} min walk`;
}

function group(places: NearbyPlace[]): NearbyGroup[] {
  const held = new Map<string, NearbyPlace[]>();
  for (const p of places) held.set(p.category, [...(held.get(p.category) ?? []), p]);
  const rank = (c: string) => (order.indexOf(c) < 0 ? order.length : order.indexOf(c));
  return [...held.entries()]
    .sort(([a], [b]) => rank(a) - rank(b) || a.localeCompare(b))
    .map(([category, on]) => ({
      category,
      places: [...on].sort((a, b) => a.distance_m - b.distance_m),
    }));
}

export function useNearby(propertyId: string | undefined): Nearby {
  const api = useMemo(() => apiFromEnv(), []);
  const [places, setPlaces] = useState<NearbyPlace[]>([]);
  const [loading, setLoading] = useState(Boolean(api && propertyId));
  const [error, setError] = useState<string | undefined>();
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    if (!propertyId) {
      setError('No property was named.');
      setLoading(false);
      return;
    }
    let alive = true;
    setLoading(true);
    api.opsPlaces(propertyId)
      .then((got) => {
        if (!alive) return;
        setPlaces(got.map((p) => ({ ...p, away: away(p) })));
        setError(undefined);
      })
      .catch((err: Error) => { if (alive) setError(err.message); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [api, propertyId, attempt]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  const add = useCallback(async (place: OpsPlaceDraft) => {
    if (!api || !propertyId) throw new Error('The API is not configured on this build.');
    if (!place.name?.trim()) throw new Error('Name the place, so a renter recognises it.');
    if (!place.distance_m) throw new Error('Say how far it is, in metres.');
    await api.opsAddPlace(propertyId, { ...place, name: place.name.trim() });
    setAttempt((n) => n + 1);
  }, [api, propertyId]);

  const remove = useCallback(async (placeId: string) => {
    if (!api) throw new Error('The API is not configured on this build.');
    await api.opsRetirePlace(placeId);
    setAttempt((n) => n + 1);
  }, [api]);

  return {
    loading,
    error,
    places,
    groups: useMemo(() => group(places), [places]),
    add,
    remove,
    reload,
  };
}
