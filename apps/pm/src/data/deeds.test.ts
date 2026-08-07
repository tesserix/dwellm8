import { fileDeed, deedKinds } from './deeds';

// The deed behind a property (#339). The bytes reach the bucket first and the
// record second: a row pointing at nothing is a document the evidence counts
// and nobody can open.

const api = {
  opsPropertyDocumentUploadUrl: jest.fn(),
  opsRecordPropertyDocument: jest.fn(),
};

beforeEach(() => {
  api.opsPropertyDocumentUploadUrl.mockReset().mockResolvedValue({
    upload_url: 'https://signed.put/deed', object_path: 'org/o1/documents/abc.jpg',
  });
  api.opsRecordPropertyDocument.mockReset().mockResolvedValue({ documents: [], ownership: {} });
  globalThis.fetch = jest.fn()
    .mockResolvedValueOnce({ blob: async () => 'bytes' })
    .mockResolvedValueOnce({ ok: true }) as unknown as typeof fetch;
});

it('puts the scan in the bucket, then files the record against the path it was given', async () => {
  await fileDeed(api as never, 'p1',
    { kind: 'title_deed', uri: 'file:///tmp/IMG_4821.jpg', contentType: 'image/jpeg' });

  expect(api.opsPropertyDocumentUploadUrl).toHaveBeenCalledWith('p1',
    { filename: 'title_deed.jpg', content_type: 'image/jpeg' });
  expect(api.opsRecordPropertyDocument).toHaveBeenCalledWith('p1', {
    kind: 'title_deed',
    object_path: 'org/o1/documents/abc.jpg',
    filename: 'title_deed.jpg',
    content_type: 'image/jpeg',
  });
});

// A record with no object behind it is worse than nothing on file: the
// evidence reads as proven and the deed cannot be produced.
it('does not file a record when the upload fails', async () => {
  globalThis.fetch = jest.fn()
    .mockResolvedValueOnce({ blob: async () => 'bytes' })
    .mockResolvedValueOnce({ ok: false }) as unknown as typeof fetch;

  await expect(fileDeed(api as never, 'p1',
    { kind: 'title_deed', uri: 'file:///tmp/a.jpg', contentType: 'image/jpeg' }))
    .rejects.toThrow(/did not reach the store/);
  expect(api.opsRecordPropertyDocument).not.toHaveBeenCalled();
});

it('offers the deed and the power of attorney as what proves the right to let', () => {
  const proving = deedKinds.filter((k) => k.proves).map((k) => k.kind);
  expect(proving).toEqual(['title_deed', 'power_of_attorney']);
});
