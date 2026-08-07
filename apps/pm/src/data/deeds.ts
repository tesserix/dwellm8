import type { DwellmApi } from '@dwellm8/mobile-shared';

/**
 * The paperwork behind a property (#339). Either the deed or the power of
 * attorney proves the owner may let it; the rest is worth holding and blocks
 * nothing. The bytes reach the bucket first and the record second, so a row
 * never points at an object that was never written.
 */
export type Deed = {
  kind: string;
  /** The local file the picker returned. Nothing is copied out of it. */
  uri: string;
  contentType: string;
};

export const deedKinds: { kind: string; label: string; proves: boolean }[] = [
  { kind: 'title_deed', label: 'Title deed', proves: true },
  { kind: 'power_of_attorney', label: 'Power of attorney', proves: true },
  { kind: 'encumbrance_certificate', label: 'Encumbrance certificate', proves: false },
  { kind: 'tax_receipt', label: 'Property tax receipt', proves: false },
  { kind: 'khata_patta', label: 'Khata or patta', proves: false },
  { kind: 'occupancy_certificate', label: 'Occupancy certificate', proves: false },
  { kind: 'society_noc', label: 'Society NOC', proves: false },
];

const extensions: Record<string, string> = {
  'image/jpeg': '.jpg', 'image/png': '.png', 'image/heic': '.heic',
  'application/pdf': '.pdf',
};

// The camera calls it IMG_4821. A file somebody opens two years later should
// say what it is.
const nameFor = (d: Deed) => `${d.kind}${extensions[d.contentType] ?? '.jpg'}`;

export async function fileDeed(api: DwellmApi, propertyId: string, d: Deed): Promise<void> {
  const filename = nameFor(d);
  const { upload_url, object_path } = await api.opsPropertyDocumentUploadUrl(propertyId, {
    filename, content_type: d.contentType,
  });

  const body = await (await fetch(d.uri)).blob();
  const put = await fetch(upload_url, {
    method: 'PUT', headers: { 'Content-Type': d.contentType }, body,
  });
  if (!put.ok) {
    throw new Error('that document did not reach the store — try again');
  }

  await api.opsRecordPropertyDocument(propertyId, {
    kind: d.kind, object_path, filename, content_type: d.contentType,
  });
}
