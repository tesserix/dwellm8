/**
 * One colleague's working week (#353).
 *
 * The rota is edited as a week, not a shift at a time: the whole seven days
 * are held as a draft so a day switched off is a day that goes, and saving
 * sends what the week has become.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsStaffShift } from '@dwellm8/mobile-shared';

const days = ['Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday', 'Sunday'];

// The hours a day gets when it is first switched on; the firm changes them.
const usualStart = '09:00';
const usualEnd = '18:00';

const clock = /^([01][0-9]|2[0-3]):[0-5][0-9]$/;

export type Day = {
  weekday: number;
  day: string;
  working: boolean;
  starts_at: string;
  ends_at: string;
};

export type Rota = {
  loading: boolean;
  error?: string;
  week: Day[];
  hours: number;
  dirty: boolean;
  toggle: (weekday: number) => void;
  setHours: (weekday: number, starts: string, ends: string) => void;
  save: () => Promise<void>;
  reload: () => void;
};

function emptyWeek(): Day[] {
  return days.map((day, i) => ({
    weekday: i + 1, day, working: false, starts_at: usualStart, ends_at: usualEnd,
  }));
}

function laidOut(shifts: OpsStaffShift[]): Day[] {
  const week = emptyWeek();
  for (const sh of shifts) {
    const d = week[sh.weekday - 1];
    if (d) Object.assign(d, { working: true, starts_at: sh.starts_at, ends_at: sh.ends_at });
  }
  return week;
}

function minutes(hhmm: string): number {
  const [h, m] = hhmm.split(':');
  return Number(h) * 60 + Number(m);
}

export function useRota(staffId: string): Rota {
  const api = useMemo(() => apiFromEnv(), []);
  const [week, setWeek] = useState<Day[]>(emptyWeek);
  const [loading, setLoading] = useState(Boolean(api));
  const [error, setError] = useState<string | undefined>();
  const [dirty, setDirty] = useState(false);
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!api) {
      setError('The API is not configured on this build.');
      setLoading(false);
      return;
    }
    let alive = true;
    setLoading(true);
    api.opsRota(staffId)
      .then((got) => { if (alive) { setWeek(laidOut(got)); setDirty(false); setError(undefined); } })
      .catch((err: Error) => { if (alive) setError(err.message); })
      .finally(() => { if (alive) setLoading(false); });
    return () => { alive = false; };
  }, [api, staffId, attempt]);

  const edit = useCallback((weekday: number, change: Partial<Day>) => {
    setWeek((w) => w.map((d) => (d.weekday === weekday ? { ...d, ...change } : d)));
    setDirty(true);
  }, []);

  const toggle = useCallback((weekday: number) => {
    setWeek((w) => w.map((d) => (d.weekday === weekday ? { ...d, working: !d.working } : d)));
    setDirty(true);
  }, []);

  const setHours = useCallback((weekday: number, starts: string, ends: string) => {
    edit(weekday, { starts_at: starts, ends_at: ends });
  }, [edit]);

  const save = useCallback(async () => {
    const worked = week.filter((d) => d.working);
    for (const d of worked) {
      if (!clock.test(d.starts_at) || !clock.test(d.ends_at)) {
        throw new Error(`${d.day} needs a time of day, as HH:MM.`);
      }
      if (minutes(d.ends_at) <= minutes(d.starts_at)) {
        throw new Error(`${d.day} must end after it starts — overnight cover is two shifts.`);
      }
    }
    if (!api) throw new Error('The API is not configured on this build.');
    await api.opsSetRota(staffId, worked.map((d) => ({
      weekday: d.weekday, starts_at: d.starts_at, ends_at: d.ends_at,
    })));
    setDirty(false);
  }, [api, staffId, week]);

  const hours = useMemo(() => week
    .filter((d) => d.working && clock.test(d.starts_at) && clock.test(d.ends_at))
    .reduce((total, d) => total + Math.max(0, minutes(d.ends_at) - minutes(d.starts_at)) / 60, 0),
  [week]);

  return {
    loading,
    error,
    week,
    hours,
    dirty,
    toggle,
    setHours,
    save,
    reload: useCallback(() => setAttempt((n) => n + 1), []),
  };
}
