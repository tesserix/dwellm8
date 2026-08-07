/**
 * An instrument, previewed as a PDF and shared to be signed (#350).
 *
 * The .docx behind a template is what a firm edits; it is not something a
 * manager can read on a phone, nor something an owner or a tenant can open.
 * This fetches the rendered blank form and the short-lived link to it.
 */

import { useCallback, useEffect, useMemo, useState } from 'react';
import { File, Paths } from 'expo-file-system';
import * as Sharing from 'expo-sharing';
import { apiFromEnv } from '@dwellm8/mobile-shared';
import type { OpsTemplatePreview } from '@dwellm8/mobile-shared';

export type PreviewedTemplate = {
  loading: boolean;
  error?: string;
  preview?: OpsTemplatePreview;
  /** How long the shared link lives, in whole minutes. */
  linkMinutes: number;
  /** Writes the PDF to the cache and opens the share sheet. */
  share: () => Promise<void>;
  reload: () => void;
};

export function useTemplatePreview(id: string): PreviewedTemplate {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<{ loading: boolean; error?: string; preview?: OpsTemplatePreview }>(
    { loading: Boolean(api && id) });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!api) {
      setState({ loading: false, error: 'The API is not configured on this build.' });
      return;
    }
    if (!id) {
      setState({ loading: false, error: 'No instrument was asked for.' });
      return;
    }
    let alive = true;
    setState({ loading: true });
    api.opsTemplatePreview(id)
      .then((preview) => { if (alive) setState({ loading: false, preview }); })
      .catch((err: Error) => { if (alive) setState({ loading: false, error: err.message }); });
    return () => { alive = false; };
  }, [api, id, attempt]);

  const share = useCallback(async () => {
    const preview = state.preview;
    if (!preview) return;
    try {
      if (!(await Sharing.isAvailableAsync())) {
        setState((s) => ({ ...s, error: 'This device cannot share a file.' }));
        return;
      }
      const file = new File(Paths.cache, preview.filename);
      file.write(preview.pdf_base64, { encoding: 'base64' });
      await Sharing.shareAsync(file.uri, {
        mimeType: preview.content_type,
        dialogTitle: preview.name,
        UTI: 'com.adobe.pdf',
      });
    } catch (err) {
      setState((s) => ({ ...s, error: (err as Error).message }));
    }
  }, [state.preview]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  return {
    ...state,
    linkMinutes: Math.round((state.preview?.expires_in_seconds ?? 0) / 60),
    share,
    reload,
  };
}
