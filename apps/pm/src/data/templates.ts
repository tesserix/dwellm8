/**
 * The agreements the firm issues (GET /v1/ops/document-templates, #341).
 *
 * Every firm starts on the platform's India library and takes an instrument
 * over by uploading its own revision. There is no e-signing yet: the manager
 * downloads the .docx, it is signed on paper, and the copy comes back as an
 * upload.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsDocumentTemplate } from '@dwellm8/mobile-shared';

export type TemplateLibrary = {
  loading: boolean;
  error?: string;
  templates: OpsDocumentTemplate[];
  /** The instruments this firm has replaced with its own version. */
  ownRevisions: string[];
  /** A short-lived link to the .docx, fetched at the moment of the tap. */
  download: (id: string) => Promise<string | undefined>;
  /** Ask again — what failed once on a phone is usually the network (#343). */
  reload: () => void;
};

export function useDocumentTemplates(opts: { kind?: string; history?: boolean } = {})
  : TemplateLibrary {
  const api = useMemo(() => apiFromEnv(), []);
  const { kind, history } = opts;
  const [state, setState] = useState<{ loading: boolean; error?: string; templates: OpsDocumentTemplate[] }>(
    { loading: Boolean(api), templates: [] });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!api) {
      setState({ loading: false, templates: [], error: 'The API is not configured on this build.' });
      return;
    }
    let alive = true;
    setState({ loading: true, templates: [] });
    const query: { kind?: string; history?: boolean } = {};
    if (kind) query.kind = kind;
    if (history) query.history = true;
    api.opsDocumentTemplates(query)
      .then((templates) => { if (alive) setState({ loading: false, templates }); })
      .catch((err: Error) => {
        if (alive) setState({ loading: false, templates: [], error: err.message });
      });
    return () => { alive = false; };
  }, [api, kind, history, attempt]);

  const download = useCallback(async (id: string) => {
    if (!api) return undefined;
    const one = await api.opsDocumentTemplate(id);
    return one.download_url;
  }, [api]);

  const ownRevisions = useMemo(
    () => state.templates.filter((t) => !t.is_default).map((t) => t.kind),
    [state.templates]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  return { ...state, ownRevisions, download, reload };
}
