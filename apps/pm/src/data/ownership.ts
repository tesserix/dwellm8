/**
 * What proves the owner may let the property (#339). A firm that markets a flat
 * on somebody's say-so has no answer when the real owner appears, so onboarding
 * asks for the deed — or the power of attorney where the owner authorised
 * somebody else to act.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsOwnershipEvidence, OpsPropertyDocument } from '@dwellm8/mobile-shared';

export type FiledDocument = {
  kind: string;
  object_path: string;
  filename: string;
  content_type: string;
};

export type OwnershipEvidence = {
  loading: boolean;
  error?: string;
  documents: OpsPropertyDocument[];
  proven: boolean;
  held: string[];
  /** What would prove the right to let. Empty once one of them is in. */
  missing: string[];
  /** Worth holding, blocks nothing. */
  advisory: string[];
  file: (doc: FiledDocument) => Promise<void>;
};

const nothing = {
  loading: false, documents: [] as OpsPropertyDocument[],
  proven: false, held: [] as string[], missing: [] as string[], advisory: [] as string[],
};

type Read = typeof nothing & { error?: string };

function fold(out: { documents: OpsPropertyDocument[]; ownership: OpsOwnershipEvidence }): Read {
  return {
    loading: false,
    documents: out.documents ?? [],
    proven: Boolean(out.ownership?.proven),
    held: out.ownership?.held ?? [],
    missing: out.ownership?.missing ?? [],
    advisory: out.ownership?.advisory ?? [],
  };
}

export function useOwnershipEvidence(propertyId: string | undefined): OwnershipEvidence {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<Read>({ ...nothing, loading: Boolean(api && propertyId) });

  useEffect(() => {
    if (!api) {
      setState({ ...nothing, error: 'The API is not configured on this build.' });
      return;
    }
    if (!propertyId) {
      setState({ ...nothing });
      return;
    }
    let alive = true;
    setState({ ...nothing, loading: true });
    api.opsPropertyDocuments(propertyId)
      .then((out) => { if (alive) setState(fold(out)); })
      .catch((err: Error) => { if (alive) setState({ ...nothing, error: err.message }); });
    return () => { alive = false; };
  }, [api, propertyId]);

  // Filing the deed answers the question, so what comes back from the write is
  // what the screen shows — not the answer it opened with.
  const file = useCallback(async (doc: FiledDocument) => {
    if (!api || !propertyId) return;
    const out = await api.opsRecordPropertyDocument(propertyId, doc);
    setState(fold(out));
  }, [api, propertyId]);

  return { ...state, file };
}
