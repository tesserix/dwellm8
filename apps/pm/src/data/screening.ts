/**
 * Screening (#258, #259): the applications on properties this firm manages,
 * and the pack it collects on each — profile, household, five years of
 * addresses. Reads go through the mandate, so nothing here is org-owned.
 */

import { useEffect, useMemo, useState } from 'react';
import {
  apiFromEnv,
  type AddressHistory,
  type ApplicantAddress,
  type ApplicantPack,
  type ApplicantPerson,
  type RentalApplication,
} from '@dwellm8/mobile-shared';

type Load<T> = { loading: boolean; error?: string; data: T };

let version = 0;
const listeners = new Set<() => void>();
export function refreshScreening() {
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

function useLoad<T>(empty: T, load: (() => Promise<T>) | null, deps: unknown[]): Load<T> {
  const [state, setState] = useState<Load<T>>({ loading: !!load, data: empty });
  useEffect(() => {
    if (!load) {
      setState({ loading: false, data: empty, error: 'The API is not configured on this build.' });
      return;
    }
    let alive = true;
    load()
      .then((data) => { if (alive) setState({ loading: false, data }); })
      .catch((err: Error) => { if (alive) setState((p) => ({ ...p, loading: false, error: err.message })); });
    return () => { alive = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return state;
}

export function useApplications(state?: string): Load<RentalApplication[]> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  return useLoad<RentalApplication[]>([], api ? () => api.opsApplications(state) : null, [api, state, v]);
}

export function useApplicantPack(applicationId: string | undefined): Load<ApplicantPack | null> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  return useLoad<ApplicantPack | null>(
    null,
    api && applicationId ? () => api.opsApplicantPack(applicationId) : null,
    [api, applicationId, v],
  );
}

export function useAddressHistory(applicationId: string | undefined): Load<AddressHistory | null> {
  const api = useMemo(() => apiFromEnv(), []);
  const v = useVersion();
  return useLoad<AddressHistory | null>(
    null,
    api && applicationId ? () => api.opsAddressHistory(applicationId) : null,
    [api, applicationId, v],
  );
}

function client() {
  const api = apiFromEnv();
  if (!api) throw new Error('The API is not configured on this build.');
  return api;
}

export async function savePack(applicationId: string, p: {
  full_name: string; date_of_birth?: string; nationality?: string;
  tax_residency?: 'resident' | 'non_resident'; occupants?: number;
  pets?: boolean; pets_note?: string;
}): Promise<void> {
  await client().opsSaveApplicantPack(applicationId, p);
  refreshScreening();
}

export async function saveHousehold(applicationId: string, people: ApplicantPerson[]): Promise<void> {
  await client().opsSaveHousehold(applicationId, people);
  refreshScreening();
}

export async function saveAddresses(applicationId: string, addresses: ApplicantAddress[]): Promise<void> {
  await client().opsSaveAddressHistory(applicationId, addresses);
  refreshScreening();
}

export async function submitPack(applicationId: string): Promise<void> {
  await client().opsSubmitApplicantPack(applicationId);
  refreshScreening();
}
