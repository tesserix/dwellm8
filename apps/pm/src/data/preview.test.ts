import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useTemplatePreview } from './preview';

// A template previews as a PDF, and the same PDF is what gets shared with an
// owner or a tenant to sign (#350).

const mockPreview = jest.fn();
const mockShare = jest.fn();
const mockWrite = jest.fn();
const mockCanShare = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL ? { opsTemplatePreview: mockPreview } : null),
}));

jest.mock('expo-file-system', () => ({
  Paths: { cache: { uri: 'file:///cache/' } },
  File: class {
    uri: string;
    constructor(dir: { uri: string }, name: string) { this.uri = dir.uri + name; }
    write(data: string, opts: unknown) { mockWrite(this.uri, data, opts); }
    create(opts: unknown) { mockWrite(this.uri, 'create', opts); }
  },
}));

jest.mock('expo-sharing', () => ({
  isAvailableAsync: () => mockCanShare(),
  shareAsync: (uri: string, opts: unknown) => mockShare(uri, opts),
}));

const preview = {
  kind: 'rent_agreement',
  name: 'Rent agreement, eleven months — India',
  filename: 'rent-agreement-preview.pdf',
  content_type: 'application/pdf',
  pdf_base64: 'JVBERi0xLjQK',
  download_url: 'https://signed.example/rent.pdf',
  expires_in_seconds: 600,
};

describe('useTemplatePreview', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockPreview.mockReset().mockResolvedValue(preview);
    mockShare.mockReset();
    mockWrite.mockReset();
    mockCanShare.mockReset().mockResolvedValue(true);
  });

  it('renders the instrument the manager tapped', async () => {
    const { result } = await renderHook(() => useTemplatePreview('t1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockPreview).toHaveBeenCalledWith('t1');
    expect(result.current.preview?.download_url).toBe('https://signed.example/rent.pdf');
  });

  // The PDF arrives in the response, so the page a manager reads is the file on
  // their own phone. Nothing to leak, and nothing that stops working in ten
  // minutes while they are still reading it.
  it('previews from the phone rather than from the link', async () => {
    const { result } = await renderHook(() => useTemplatePreview('t1'));

    await waitFor(() => expect(result.current.fileUri).toBe(
      'file:///cache/rent-agreement-preview.pdf'));
    expect(mockWrite).toHaveBeenCalledWith(
      'file:///cache/rent-agreement-preview.pdf', 'JVBERi0xLjQK', { encoding: 'base64' });
  });

  // The bucket the link points at is not something the app needs to read.
  it('still previews when the server could not mint a link', async () => {
    mockPreview.mockResolvedValue({ ...preview, download_url: undefined });
    const { result } = await renderHook(() => useTemplatePreview('t1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.fileUri).toBe('file:///cache/rent-agreement-preview.pdf');
    expect(result.current.error).toBeUndefined();
  });

  // The link is a bearer credential for one document. Saying how long it lives
  // is the difference between sharing it and pasting it into a group chat.
  it('says how long the shared link lives', async () => {
    const { result } = await renderHook(() => useTemplatePreview('t1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.linkMinutes).toBe(10);
  });

  it('writes the PDF and hands it to the share sheet', async () => {
    const { result } = await renderHook(() => useTemplatePreview('t1'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => { await result.current.share(); });

    expect(mockWrite).toHaveBeenCalledWith(
      'file:///cache/rent-agreement-preview.pdf', 'JVBERi0xLjQK', { encoding: 'base64' });
    expect(mockShare).toHaveBeenCalledWith('file:///cache/rent-agreement-preview.pdf',
      expect.objectContaining({ mimeType: 'application/pdf' }));
  });

  // A device with no share sheet must say so rather than appear to have sent it.
  it('reports when the device cannot share', async () => {
    mockCanShare.mockResolvedValue(false);
    const { result } = await renderHook(() => useTemplatePreview('t1'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => { await result.current.share(); });

    expect(mockShare).not.toHaveBeenCalled();
    expect(result.current.error).toMatch(/cannot share/i);
  });

  it('reports a failure rather than an empty page', async () => {
    mockPreview.mockRejectedValue(new Error('could not print the preview'));
    const { result } = await renderHook(() => useTemplatePreview('t1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('could not print the preview');
    expect(result.current.preview).toBeUndefined();
  });
});
