import { fileCopy, type Copy } from './papers';

// The copy an owner abroad produces (#318). It reaches the bucket first and is
// recorded second, so a record with nothing behind it cannot exist.

const aCopy = (over: Partial<Copy> = {}): Copy => ({
  kind: 'passport',
  uri: 'file:///tmp/passport.jpg',
  contentType: 'image/jpeg',
  selfAttested: true,
  number: 'L898902C3',
  issuingCountry: 'IN',
  expiresOn: '2029-05-01',
  ...over,
});

function anApi() {
  return {
    opsPartyUploadURL: jest.fn().mockResolvedValue({
      upload_url: 'https://bucket.example/put?sig=x',
      object_path: 'org/o1/documents/abc.jpg',
    }),
    opsRecordPartyDocument: jest.fn().mockResolvedValue({ documents: [] }),
  };
}

beforeEach(() => {
  globalThis.fetch = jest.fn(async (url: string) =>
    (String(url).startsWith('file:')
      ? { ok: true, blob: async () => 'bytes' }
      : { ok: true })) as unknown as typeof fetch;
});

it('puts the bytes where the API said, and records that path', async () => {
  const api = anApi();
  await fileCopy(api as never, 'party-1', aCopy(), '2026-08-07');

  expect(api.opsPartyUploadURL).toHaveBeenCalledWith('party-1', 'passport.jpg', 'image/jpeg');
  const [url, init] = (globalThis.fetch as jest.Mock).mock.calls[1];
  expect(url).toBe('https://bucket.example/put?sig=x');
  expect(init.method).toBe('PUT');
  expect(init.headers['Content-Type']).toBe('image/jpeg');
  expect(api.opsRecordPartyDocument).toHaveBeenCalledWith('party-1',
    expect.objectContaining({ object_path: 'org/o1/documents/abc.jpg', kind: 'passport' }));
});

it('dates the attestation, because an undated one attests nothing', async () => {
  const api = anApi();
  await fileCopy(api as never, 'party-1', aCopy(), '2026-08-07');

  expect(api.opsRecordPartyDocument).toHaveBeenCalledWith('party-1',
    expect.objectContaining({ attestation: 'self', attested_on: '2026-08-07' }));
});

it('sends no attestation date for a copy taken beside the original', async () => {
  const api = anApi();
  await fileCopy(api as never, 'party-1', aCopy({ selfAttested: false }), '2026-08-07');

  const [, sent] = api.opsRecordPartyDocument.mock.calls[0];
  expect(sent.attestation).toBe('none');
  expect(sent.attested_on).toBeUndefined();
});

it('does not record a copy that never reached the bucket', async () => {
  const api = anApi();
  globalThis.fetch = jest.fn(async (url: string) =>
    (String(url).startsWith('file:')
      ? { ok: true, blob: async () => 'bytes' }
      : { ok: false, status: 403 })) as unknown as typeof fetch;

  await expect(fileCopy(api as never, 'party-1', aCopy(), '2026-08-07')).rejects.toThrow();
  expect(api.opsRecordPartyDocument).not.toHaveBeenCalled();
});

it('names the file for the document, not for the camera roll', async () => {
  const api = anApi();
  await fileCopy(api as never, 'party-1', aCopy({ kind: 'pan_card', contentType: 'image/png' }),
    '2026-08-07');

  expect(api.opsPartyUploadURL).toHaveBeenCalledWith('party-1', 'pan_card.png', 'image/png');
});

it('sends the number once, to the endpoint that masks it', async () => {
  const api = anApi();
  await fileCopy(api as never, 'party-1', aCopy(), '2026-08-07');

  const [, sent] = api.opsRecordPartyDocument.mock.calls[0];
  expect(sent.number).toBe('L898902C3');
  expect(JSON.stringify((globalThis.fetch as jest.Mock).mock.calls)).not.toContain('L898902C3');
});
