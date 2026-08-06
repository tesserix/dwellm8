/**
 * The firm's own name and number, as filed on the registration screen. Used to
 * prefill "It's mine" onboarding rather than asking a manager to retype what
 * the register already holds (#295).
 */

import { useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';

export type FirmContact = { name: string; phone: string };

export function useFirmContact(): FirmContact {
  const api = useMemo(() => apiFromEnv(), []);
  const [contact, setContact] = useState<FirmContact>({ name: '', phone: '' });

  useEffect(() => {
    if (!api) return;
    let alive = true;
    api.opsRegistration()
      .then((r) => {
        if (alive) setContact({ name: r.legal_name ?? '', phone: r.contact_phone ?? '' });
      })
      // A prefill is a convenience: failing to read it leaves the fields empty
      // and typeable, never blocks the flow.
      .catch(() => {});
    return () => { alive = false; };
  }, [api]);

  return contact;
}
