/**
 * Where Find's screens get their data.
 *
 * live: EXPO_PUBLIC_API_URL set — everything comes from the public discovery
 * API (ADR-0019): anonymous search over the schema's one public branch, a
 * browsing token for the shortlist, phone verification at the point of
 * contact. demo: the §9.6 sample listings, exactly as before.
 *
 * The photographs in live mode are neutral placeholders cycled per listing —
 * the media pipeline (#136) is what will carry real ones, and a stock photo
 * pretending to be the flat is the thing this marketplace exists not to do,
 * so the card copy never claims they are the property's own.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv, ApiError, type DwellmApi, type PublicListing } from '@dwellm8/mobile-shared';
import * as demo from './mock';

export type Mode = 'live' | 'demo';

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export function fmtDate(iso?: string): string {
  if (!iso) return '';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return `${String(d.getDate()).padStart(2, '0')} ${MONTHS[d.getMonth()]} ${d.getFullYear()}`;
}

const PHOTO_KEYS = Object.keys(demo.photos) as (keyof typeof demo.photos)[];

/** A live listing in the shape every Find screen already renders. */
export function toListing(p: PublicListing, i: number): demo.Listing {
  const published = new Date(p.published_at);
  const ageDays = Number.isNaN(published.getTime())
    ? 0
    : Math.floor((Date.now() - published.getTime()) / 86_400_000);
  return {
    id: p.id,
    title: p.headline,
    locality: p.locality,
    city: p.city,
    rentPaise: p.rent_minor,
    depositPaise: p.deposit_minor,
    bhk: p.bedrooms ?? 0,
    baths: 0,
    sqft: Math.round(p.carpet_area_sqft ?? 0),
    floor: '',
    furnishing: 'Unfurnished',
    facing: '',
    availableFrom: p.available_from ? fmtDate(p.available_from) : 'Now',
    preferred: '',
    listedBy: 'Owner',
    lister: '',
    // Published from live inventory by an authenticated organisation — the
    // marketplace's verification promise, kept by construction (ADR-0019).
    managed: true,
    verification: { ownership: true, address: true, identity: true, photosOwn: false },
    postedOn: fmtDate(p.published_at),
    daysLeft: Math.max(0, demo.LISTING_DAYS - ageDays),
    boosted: false,
    views: 0,
    enquiries: 0,
    photo: PHOTO_KEYS[i % PHOTO_KEYS.length],
    amenities: [],
    about: '',
    inspections: [],
  };
}

/** The browsing token, held for the app session. A stranger's key, so it
 * lives in memory rather than durable storage until sign-in exists. */
let sessionToken: string | null = null;

export async function prospectToken(api: DwellmApi): Promise<string> {
  if (!sessionToken) sessionToken = await api.prospectStart();
  return sessionToken;
}

export function api(): DwellmApi | null {
  return apiFromEnv();
}

export type SearchState = {
  mode: Mode;
  loading: boolean;
  error?: string;
  listings: demo.Listing[];
  refresh: () => void;
};

/** useSearch drives the search tab: server-side city/budget/bhk, the free-text
 * box narrowing the fetched page client-side the way the demo always did. */
export function useSearch(city?: string, bedrooms?: number, maxRentMinor?: number): SearchState {
  const client = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<Omit<SearchState, 'refresh'>>(
    client
      ? { mode: 'live', loading: true, listings: [] }
      : { mode: 'demo', loading: false, listings: demo.listings },
  );
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!client) return;
    let alive = true;
    setState((p) => ({ ...p, loading: true, error: undefined }));
    client
      .publicSearch({ city, bedrooms, maxRentMinor, limit: 50 })
      .then((out) => {
        if (alive) setState({ mode: 'live', loading: false, listings: out.listings.map(toListing) });
      })
      .catch((err: Error) => {
        if (alive) setState({ mode: 'live', loading: false, listings: [], error: err.message });
      });
    return () => { alive = false; };
  }, [client, city, bedrooms, maxRentMinor, tick]);

  const refresh = useCallback(() => setTick((t) => t + 1), []);
  return { ...state, refresh };
}

/** One listing's detail — live fetch, or the demo row. */
export function useListing(id: string | undefined) {
  const client = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<{ loading: boolean; listing?: demo.Listing; error?: string }>(
    client ? { loading: true } : { loading: false, listing: demo.listings.find((l) => l.id === id) },
  );

  useEffect(() => {
    if (!client || !id) return;
    let alive = true;
    client
      .publicListing(id)
      .then((p) => { if (alive) setState({ loading: false, listing: toListing(p, 0) }); })
      .catch((err: ApiError | Error) => {
        if (alive) setState({ loading: false, error: err.message });
      });
    return () => { alive = false; };
  }, [client, id]);

  return state;
}

/** The shortlist, mapped back to cards; and the prospect's enquiry timeline. */
export function useMyFind() {
  const client = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<{
    mode: Mode; loading: boolean;
    saved: demo.Listing[]; enquiries: { id: string; title: string; state: string; when: string }[];
  }>(
    client
      ? { mode: 'live', loading: true, saved: [], enquiries: [] }
      : {
          mode: 'demo', loading: false, saved: demo.listings.slice(0, 2),
          enquiries: demo.enquiries.map((e) => ({
            id: e.id,
            title: demo.listings.find((l) => l.id === e.listingId)?.title ?? 'Enquiry',
            state: e.state, when: e.at,
          })),
        },
  );
  const [tick, setTick] = useState(0);

  useEffect(() => {
    if (!client) return;
    let alive = true;
    (async () => {
      const token = await prospectToken(client);
      const [saved, enquiries] = await Promise.all([
        client.shortlist(token).catch(() => []),
        client.myEnquiries(token).catch(() => []),
      ]);
      if (!alive) return;
      setState({
        mode: 'live', loading: false,
        saved: saved.map((s, i) => toListing({
          id: s.listing_id, headline: s.headline, locality: s.locality, city: s.city,
          state_code: '', rent_minor: s.rent_minor, maintenance_minor: 0, parking_minor: 0,
          other_monthly_minor: 0, deposit_minor: 0, one_time_minor: 0,
          total_monthly_minor: s.rent_minor, total_one_time_minor: 0,
          currency: 'INR', published_at: '',
        }, i)),
        enquiries: enquiries.map((e) => ({
          id: e.id, title: e.headline || 'Enquiry', state: e.state, when: fmtDate(e.created_at),
        })),
      });
    })().catch(() => { if (alive) setState((p) => ({ ...p, loading: false })); });
    return () => { alive = false; };
  }, [client, tick]);

  const refresh = useCallback(() => setTick((t) => t + 1), []);
  return { ...state, refresh };
}
