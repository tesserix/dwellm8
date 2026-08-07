/**
 * The manager's viewing times (#330, #333): the recurring series on a listing
 * and the occurrences they produced. Live only — a time shown here is a time a
 * prospect can already book.
 */

import { useEffect, useMemo, useState } from 'react';
import {
  apiFromEnv,
  type OpsInspection,
  type OpsListing,
  type ViewingSchedule,
  type ViewingScheduleInput,
} from '@dwellm8/mobile-shared';

const DAYS = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday'];
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];

export const DAY_INITIALS = ['S', 'M', 'T', 'W', 'T', 'F', 'S'];

function fmtDay(ymd: string): string {
  const [y, m, d] = ymd.split('-').map(Number);
  if (!y || !m || !d) return ymd;
  return `${String(d).padStart(2, '0')} ${MONTHS[m - 1]} ${y}`;
}

function inWeekOrder(days: number[]): string {
  const names = [...days].sort((a, b) => a - b).map((d) => `${DAYS[d]}s`);
  if (names.length <= 1) return names[0] ?? '';
  return `${names.slice(0, -1).join(', ')} and ${names[names.length - 1]}`;
}

/** When the series repeats. */
export function seriesWhen(s: ViewingSchedule): string {
  const when = `${inWeekOrder(s.weekdays)} at ${s.start_time}`;
  return s.state === 'ended' ? `Ended — ${when}` : when;
}

/** What shape each viewing is — nothing, once the series has stopped. */
export function seriesShape(s: ViewingSchedule): string {
  if (s.state === 'ended') return '';
  const parts = [`${s.duration_mins} min`, `up to ${s.capacity} people`];
  if (s.ends_on) parts.push(`until ${fmtDay(s.ends_on)}`);
  return parts.join(' · ');
}

/** The series as the manager said it out loud. */
export function seriesInWords(s: ViewingSchedule): string {
  return [seriesWhen(s), seriesShape(s)].filter(Boolean).join(' · ');
}

export const DEFAULT_ZONE = 'Asia/Kolkata';

type Wall = { year: number; month: number; day: number; weekday: number; hour: number; minute: number };

/** The wall clock an instant reads as at the property, not on this phone. */
function wallClock(at: Date, zone: string): Wall {
  const parts = new Intl.DateTimeFormat('en-US', {
    timeZone: zone, hour12: false, weekday: 'short',
    year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit',
  }).formatToParts(at).reduce<Record<string, string>>((acc, p) => {
    acc[p.type] = p.value;
    return acc;
  }, {});
  return {
    year: Number(parts.year), month: Number(parts.month), day: Number(parts.day),
    weekday: DAYS.findIndex((d) => d.startsWith(parts.weekday)),
    // Some zones format midnight as 24 under hour12: false.
    hour: Number(parts.hour) % 24, minute: Number(parts.minute),
  };
}

function offsetMs(at: Date, zone: string): number {
  const w = wallClock(at, zone);
  return Date.UTC(w.year, w.month - 1, w.day, w.hour, w.minute, 0) -
    Math.floor(at.getTime() / 60000) * 60000;
}

export function fmtOccurrence(iso: string, zone = DEFAULT_ZONE): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const w = wallClock(at, zone);
  const h = w.hour % 12 || 12;
  const ampm = w.hour >= 12 ? 'pm' : 'am';
  return `${DAYS[w.weekday].slice(0, 3)} ${String(w.day).padStart(2, '0')} ${MONTHS[w.month - 1]}` +
    ` · ${h}:${String(w.minute).padStart(2, '0')} ${ampm}`;
}

/**
 * The instant of that wall clock, on the day this viewing already falls on at
 * the property. Two passes, because the clocks may change between the guess and
 * the answer.
 */
export function atLocalTime(iso: string, hhmm: string, zone = DEFAULT_ZONE): string {
  const [h, m] = hhmm.split(':').map(Number);
  if (!/^\d{1,2}:\d{2}$/.test(hhmm) || Number.isNaN(h) || Number.isNaN(m) || h > 23 || m > 59) {
    throw new Error('The new time must read as HH:MM.');
  }
  const from = new Date(iso);
  const w = wallClock(from, zone);
  const naive = Date.UTC(w.year, w.month - 1, w.day, h, m, 0);
  let at = new Date(naive - offsetMs(from, zone));
  at = new Date(naive - offsetMs(at, zone));
  return at.toISOString();
}

type Load<T> = { loading: boolean; error?: string; data: T };

const noApi = 'The API is not configured on this build.';

/* Screens that mutate call refreshViewings(); every mounted hook refetches. */
let version = 0;
const listeners = new Set<() => void>();

export function refreshViewings() {
  version++;
  listeners.forEach((l) => l());
}

function useVersion(): number {
  const [v, setV] = useState(version);
  useEffect(() => {
    const bump = () => setV(version);
    listeners.add(bump);
    return () => { listeners.delete(bump); };
  }, []);
  return v;
}

export function useOpsListings(state?: string): Load<OpsListing[]> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  const [s, setS] = useState<Load<OpsListing[]>>({ loading: Boolean(api), data: [] });

  useEffect(() => {
    if (!api) {
      setS({ loading: false, data: [], error: noApi });
      return;
    }
    let alive = true;
    api.opsListings(state)
      .then((data) => { if (alive) setS({ loading: false, data }); })
      .catch((err: Error) => { if (alive) setS({ loading: false, data: [], error: err.message }); });
    return () => { alive = false; };
  }, [api, state, v]);

  return s;
}

export type Viewings = {
  loading: boolean;
  error?: string;
  schedules: ViewingSchedule[];
  /** Only the times still to come — a past viewing is history, not a calendar. */
  slots: OpsInspection[];
};

export function useListingViewings(listingId: string | undefined): Viewings {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  const [s, setS] = useState<Viewings>({ loading: Boolean(api && listingId), schedules: [], slots: [] });

  useEffect(() => {
    if (!api || !listingId) {
      setS({ loading: false, schedules: [], slots: [], error: api ? 'No listing was named.' : noApi });
      return;
    }
    let alive = true;
    setS((p) => ({ ...p, loading: true, error: undefined }));
    Promise.all([api.listingSchedules(listingId), api.opsListingSlots(listingId)])
      .then(([schedules, slots]) => {
        if (!alive) return;
        const now = Date.now();
        setS({
          loading: false,
          schedules,
          slots: slots
            .filter((sl) => new Date(sl.starts_at).getTime() >= now)
            .sort((a, b) => a.starts_at.localeCompare(b.starts_at)),
        });
      })
      .catch((err: Error) => {
        if (alive) setS({ loading: false, schedules: [], slots: [], error: err.message });
      });
    return () => { alive = false; };
  }, [api, listingId, v]);

  return s;
}

function client() {
  const api = apiFromEnv();
  if (!api) throw new Error(noApi);
  return api;
}

export async function addSeries(listingId: string, d: ViewingScheduleInput): Promise<void> {
  await client().createSchedule(listingId, d);
  refreshViewings();
}

export async function amendSeries(id: string, d: ViewingScheduleInput & { from: string }): Promise<void> {
  await client().amendSchedule(id, d);
  refreshViewings();
}

export async function endSeries(id: string, from?: string): Promise<void> {
  await client().endSeries(id, from);
  refreshViewings();
}

export async function cancelViewing(slotId: string): Promise<void> {
  await client().cancelSlot(slotId);
  refreshViewings();
}

export async function moveViewing(slotId: string, startsAt: string): Promise<void> {
  await client().moveSlot(slotId, startsAt);
  refreshViewings();
}
