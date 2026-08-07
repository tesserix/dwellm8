/**
 * Who is signed in, and the firm they are acting for (#251).
 *
 * The registration is read separately from the person because a manager can be
 * signed in before their firm is registered — the profile must still say who
 * they are rather than nothing at all.
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';

export type Account = {
  loading: boolean;
  name: string;
  initials: string;
  email: string;
  phone: string;
  firmName: string;
  constitution: string;
  mayManage: boolean;
  properties: number;
};

const empty: Account = {
  loading: false, name: '', initials: '', email: '', phone: '',
  firmName: '', constitution: '', mayManage: false, properties: 0,
};

function initialsOf(name: string): string {
  return name.split(/[\s.]+/).filter(Boolean).slice(0, 2).map((w) => w[0].toUpperCase()).join('');
}

// Registration never asks for a display name, so most managers have none and
// the profile read "Not signed in" above their own email address (#315).
function nameOf(displayName: string, email: string): string {
  return displayName || email.split('@')[0] || '';
}

export function useAccount(): Account {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<Account>(api ? { ...empty, loading: true } : empty);

  useEffect(() => {
    if (!api) return;
    let alive = true;
    Promise.all([
      api.me().catch(() => null),
      api.opsRegistration().catch(() => null),
      api.opsProperties().catch(() => []),
    ]).then(([me, reg, properties]) => {
      if (!alive) return;
      const email = me?.email ?? '';
      const name = nameOf(me?.display_name?.trim() ?? '', email);
      setState({
        loading: false,
        name, initials: initialsOf(name),
        email, phone: me?.phone ?? '',
        firmName: reg?.trade_name?.trim() || reg?.legal_name?.trim() || '',
        constitution: reg?.constitution ?? '',
        mayManage: reg?.may_manage ?? false,
        properties: properties.length,
      });
    });
    return () => { alive = false; };
  }, [api]);

  return state;
}
