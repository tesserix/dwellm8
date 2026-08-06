/**
 * Sending a vendor to a tenant's home (#251).
 *
 * There is no vendor directory endpoint yet, so the manager names who they are
 * sending; the slot is generated from the clock in the timezone the tenancy
 * lives in, never the device's, because a slot that has already passed is how
 * a tenant ends up waiting in for nobody.
 */

import { useCallback, useState } from 'react';
import { advanceTicket } from './worklists';

export type Slot = { label: string; starts_at: string };

const WINDOWS = [
  { from: 9, to: 11 }, { from: 11, to: 13 }, { from: 14, to: 16 }, { from: 17, to: 19 },
];

const IST_OFFSET_MINUTES = 330;

function istParts(at: Date): { y: number; m: number; d: number; hour: number } {
  const ist = new Date(at.getTime() + IST_OFFSET_MINUTES * 60_000);
  return {
    y: ist.getUTCFullYear(), m: ist.getUTCMonth() + 1, d: ist.getUTCDate(), hour: ist.getUTCHours(),
  };
}

function isoAt(y: number, m: number, d: number, hour: number): string {
  const two = (n: number) => String(n).padStart(2, '0');
  return `${y}-${two(m)}-${two(d)}T${two(hour)}:00:00+05:30`;
}

/** The next few visiting windows, in IST, none of them already gone. */
export function nextSlots(at: Date = new Date(), count = 4): Slot[] {
  const { y, m, d, hour } = istParts(at);
  const out: Slot[] = [];

  for (let day = 0; out.length < count && day < 7; day++) {
    // Date arithmetic in UTC so a month or year boundary rolls correctly.
    const on = new Date(Date.UTC(y, m - 1, d + day));
    const [yy, mm, dd] = [on.getUTCFullYear(), on.getUTCMonth() + 1, on.getUTCDate()];
    const when = day === 0 ? 'Today' : day === 1 ? 'Tomorrow'
      : new Intl.DateTimeFormat('en-IN', { timeZone: 'UTC', weekday: 'long' }).format(on);

    for (const w of WINDOWS) {
      if (out.length === count) break;
      // An hour's notice, so nothing is offered that is effectively now.
      if (day === 0 && w.from <= hour + 1) continue;
      out.push({
        label: `${when}, ${String(w.from).padStart(2, '0')}:00 – ${String(w.to).padStart(2, '0')}:00`,
        starts_at: isoAt(yy, mm, dd, w.from),
      });
    }
  }
  return out;
}

export type Dispatcher = {
  dispatchTo: (vendor: string, slot: string) => Promise<void>;
  sending: boolean;
  sent: boolean;
  error?: string;
};

export function useDispatch(ticketId: string): Dispatcher {
  const [sending, setSending] = useState(false);
  const [sent, setSent] = useState(false);
  const [error, setError] = useState<string | undefined>();

  const dispatchTo = useCallback(async (vendor: string, slot: string) => {
    if (!vendor.trim()) { setError('Name the vendor being sent.'); return; }
    setSending(true);
    setError(undefined);
    try {
      await advanceTicket(ticketId, { action: 'schedule', vendor: vendor.trim(), slot });
      setSent(true);
    } catch (err) {
      setError((err as Error).message);
    } finally {
      setSending(false);
    }
  }, [ticketId]);

  return { dispatchTo, sending, sent, error };
}
