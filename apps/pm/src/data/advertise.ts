/**
 * Advertising a vacant unit (#370): a draft is written, then published.
 *
 * The two are separate calls because the API's disclosure and claims gates
 * refuse at publication, not at creation. A refused draft cannot be amended —
 * there is no such endpoint — so it is withdrawn rather than left on the
 * register as a listing nobody can fix or see.
 */

import { useCallback, useMemo, useState } from 'react';
import { apiFromEnv, type ListingDraft } from '@dwellm8/mobile-shared';

export type Advertising = {
  working: boolean;
  error?: string;
  live: boolean;
  publish: (draft: ListingDraft) => Promise<void>;
};

export function useAdvertise(): Advertising {
  const api = useMemo(() => apiFromEnv(), []);
  const [working, setWorking] = useState(false);
  const [error, setError] = useState<string | undefined>();
  const [live, setLive] = useState(false);

  const publish = useCallback(async (draft: ListingDraft) => {
    if (!api) {
      setError('The API is not configured on this build.');
      return;
    }
    setWorking(true);
    setError(undefined);
    let id = '';
    try {
      id = (await api.createListing(draft)).id;
      await api.publishListing(id);
      setLive(true);
    } catch (err) {
      setError((err as Error).message);
      if (id) await api.withdrawListing(id).catch(() => {});
    } finally {
      setWorking(false);
    }
  }, [api]);

  return { working, error, live, publish };
}
