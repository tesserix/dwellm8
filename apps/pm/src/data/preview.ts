/**
 * An instrument, previewed as a PDF and shared to be signed (#350).
 *
 * The .docx behind a template is what a firm edits; it is not something a
 * manager can read on a phone, nor something an owner or a tenant can open.
 * This fetches the rendered blank form and writes it to the phone, so the page
 * on screen is a local file rather than a link that expires while it is read.
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
  /** The PDF on this phone — what the page on screen renders. */
  fileUri?: string;
  /** How long the shared link lives, in whole minutes. */
  linkMinutes: number;
  /** Opens the share sheet on the same file. */
  share: () => Promise<void>;
  reload: () => void;
};

type State = {
  loading: boolean;
  error?: string;
  preview?: OpsTemplatePreview;
  fileUri?: string;
};

// The response carries the PDF itself, so nothing here depends on the bucket.
function writePdf(preview: OpsTemplatePreview): string {
  const file = new File(Paths.cache, preview.filename);
  file.write(preview.pdf_base64, { encoding: 'base64' });
  return file.uri;
}

export function useTemplatePreview(id: string): PreviewedTemplate {
  const api = useMemo(() => apiFromEnv(), []);
  const [state, setState] = useState<State>({ loading: Boolean(api && id) });
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
      .then((preview) => {
        if (!alive) return;
        try {
          setState({ loading: false, preview, fileUri: writePdf(preview) });
        } catch (err) {
          setState({ loading: false, preview, error: (err as Error).message });
        }
      })
      .catch((err: Error) => { if (alive) setState({ loading: false, error: err.message }); });
    return () => { alive = false; };
  }, [api, id, attempt]);

  const share = useCallback(async () => {
    const { preview, fileUri } = state;
    if (!preview || !fileUri) return;
    try {
      if (!(await Sharing.isAvailableAsync())) {
        setState((s) => ({ ...s, error: 'This device cannot share a file.' }));
        return;
      }
      await Sharing.shareAsync(fileUri, {
        mimeType: preview.content_type,
        dialogTitle: preview.name,
        UTI: 'com.adobe.pdf',
      });
    } catch (err) {
      setState((s) => ({ ...s, error: (err as Error).message }));
    }
  }, [state]);

  const reload = useCallback(() => setAttempt((n) => n + 1), []);

  return {
    ...state,
    linkMinutes: Math.round((state.preview?.expires_in_seconds ?? 0) / 60),
    share,
    reload,
  };
}
