import type { DwellmApi, PartyDocumentKind } from '@dwellm8/mobile-shared';

/**
 * The copy behind the identity (#318).
 *
 * An owner living abroad cannot bring an original to an office, so the manager
 * files a photograph of one and says how it was attested. The bytes go to the
 * bucket first and the record second: a row pointing at nothing would be a
 * document the checklist counts and nobody can open.
 */
export type Copy = {
  kind: PartyDocumentKind;
  /** The local file the picker returned. Nothing is copied out of it. */
  uri: string;
  contentType: string;
  /** Signed and dated across the face by the holder, which is the usual case. */
  selfAttested: boolean;
  number?: string;
  issuingCountry?: string;
  issuedOn?: string;
  expiresOn?: string;
};

const extensions: Record<string, string> = {
  'image/jpeg': '.jpg', 'image/png': '.png', 'image/heic': '.heic',
  'application/pdf': '.pdf',
};

// The camera calls it IMG_4821. A file somebody opens two years later should
// say what it is.
const nameFor = (c: Copy) => `${c.kind}${extensions[c.contentType] ?? '.jpg'}`;

export async function fileCopy(api: DwellmApi, party: string, c: Copy, today: string): Promise<void> {
  const { upload_url, object_path } = await api.opsPartyUploadURL(party, nameFor(c), c.contentType);

  const body = await (await fetch(c.uri)).blob();
  const put = await fetch(upload_url, {
    method: 'PUT', headers: { 'Content-Type': c.contentType }, body,
  });
  if (!put.ok) {
    throw new Error('that copy did not reach the store — try again');
  }

  await api.opsRecordPartyDocument(party, {
    kind: c.kind,
    object_path,
    filename: nameFor(c),
    content_type: c.contentType,
    number: c.number,
    issuing_country: c.issuingCountry,
    issued_on: c.issuedOn,
    expires_on: c.expiresOn,
    attestation: c.selfAttested ? 'self' : 'none',
    attested_on: c.selfAttested ? today : undefined,
  });
}
