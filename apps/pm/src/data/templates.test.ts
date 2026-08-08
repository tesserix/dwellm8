import { renderHook, waitFor, act } from '@testing-library/react-native';
import { useDocumentTemplates } from './templates';

// The agreements the firm issues (#341). There is no e-signing: a manager
// downloads the .docx, it is signed on paper, and the edited or executed copy
// is uploaded back as a new version.

const mockList = jest.fn();
const mockOne = jest.fn();

jest.mock('@dwellm8/mobile-shared', () => ({
  apiFromEnv: () => (process.env.EXPO_PUBLIC_API_URL
    ? { opsDocumentTemplates: mockList, opsDocumentTemplate: mockOne }
    : null),
}));

const library = [
  {
    id: 't1', kind: 'management_agreement', jurisdiction: 'IN',
    name: 'Property management agreement — India', version: 1, is_default: true,
    filename: 'pma.docx', content_type: 'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
    merge_fields: ['owner_name', 'manager_name', 'sale_notice_months'], uploaded_by: 'platform',
  },
  {
    id: 't2', kind: 'rent_agreement', jurisdiction: 'IN',
    name: 'Our own rent agreement', version: 2, is_default: false,
    filename: 'rent.docx', content_type: 'application/octet-stream',
    merge_fields: ['landlord_name', 'tenant_name'], uploaded_by: 'manager@firm',
  },
];

describe('useDocumentTemplates', () => {
  beforeEach(() => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    mockList.mockReset().mockResolvedValue(library);
    mockOne.mockReset().mockResolvedValue({ ...library[0], download_url: 'https://signed/pma.docx' });
  });

  it('lists what the firm issues today', async () => {
    const { result } = await renderHook(() => useDocumentTemplates());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(mockList).toHaveBeenCalledWith({});
    expect(result.current.templates).toHaveLength(2);
  });

  // Which template a manager is looking at matters: the platform's is the one
  // every firm gets, and their own is the one their lawyer wrote.
  it('says which instruments the firm has taken over from the library', async () => {
    const { result } = await renderHook(() => useDocumentTemplates());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.ownRevisions).toEqual(['rent_agreement']);
  });

  it('reports a failure rather than an empty library', async () => {
    mockList.mockRejectedValue(new Error('could not read the templates'));
    const { result } = await renderHook(() => useDocumentTemplates());

    await waitFor(() => expect(result.current.loading).toBe(false));
    expect(result.current.error).toBe('could not read the templates');
    expect(result.current.templates).toEqual([]);
  });
});
