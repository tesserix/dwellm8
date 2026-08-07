import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useOwnershipEvidence } from './ownership';

// What proves the owner may let the property at all (#339): the deed, or the
// power of attorney under which somebody else acts for the owner.

const mockRead = jest.fn();
const mockFile = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL
    ? { opsPropertyDocuments: mockRead, opsRecordPropertyDocument: mockFile }
    : null),
}));

const nothingOnFile = {
  documents: [],
  ownership: {
    proven: false,
    held: [],
    missing: ['title_deed', 'power_of_attorney'],
    advisory: ['encumbrance_certificate'],
  },
};

const deedOnFile = {
  documents: [{
    id: 'd1', kind: 'title_deed', filename: 'deed.pdf', content_type: 'application/pdf',
    uploaded_by: 'manager@firm', created_at: '2026-08-01T10:00:00+05:30',
  }],
  ownership: { proven: true, held: ['title_deed'], missing: [], advisory: ['encumbrance_certificate'] },
};

describe('useOwnershipEvidence', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockRead.mockReset().mockResolvedValue(nothingOnFile);
    mockFile.mockReset().mockResolvedValue(deedOnFile);
  });

  it('says a property with nothing on file is not proven, and what is wanted', async () => {
    const { result } = await renderHook(() => useOwnershipEvidence('p1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockRead).toHaveBeenCalledWith('p1');
    expect(result.current.proven).toBe(false);
    expect(result.current.missing).toEqual(['title_deed', 'power_of_attorney']);
  });

  // Filing the deed answers the question, so the screen must show the answer
  // it just produced rather than the one it opened with.
  it('re-reads what is proven after a document is filed', async () => {
    const { result } = await renderHook(() => useOwnershipEvidence('p1'));
    await waitFor(() => expect(result.current.loading).toBe(false));

    await act(async () => {
      await result.current.file({
        kind: 'title_deed', object_path: 'org/o1/documents/x.pdf',
        filename: 'deed.pdf', content_type: 'application/pdf',
      });
    });

    expect(mockFile).toHaveBeenCalledWith('p1', expect.objectContaining({ kind: 'title_deed' }));
    expect(result.current.proven).toBe(true);
    expect(result.current.documents).toHaveLength(1);
  });

  it('reports an unreadable property rather than showing it as unproven', async () => {
    mockRead.mockRejectedValue(new Error('no such property'));
    const { result } = await renderHook(() => useOwnershipEvidence('p1'));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('no such property');
    expect(result.current.proven).toBe(false);
  });

  it('asks nothing when no property is named', async () => {
    const { result } = await renderHook(() => useOwnershipEvidence(undefined));

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockRead).not.toHaveBeenCalled();
  });
});
