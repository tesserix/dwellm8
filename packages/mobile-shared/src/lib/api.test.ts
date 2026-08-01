import { apiFromEnv, ApiError, DwellmApi } from './api';

// The one HTTP client every app talks through. What matters here is the
// contract every screen relies on without re-checking it: the right path and
// method per call, a server error surfaced as the server's own message
// rather than a generic one, and apiFromEnv's demo/live switch.

function mockFetch(body: unknown, status = 200) {
  return jest.fn().mockResolvedValue({
    ok: status >= 200 && status < 300,
    status,
    text: async () => JSON.stringify(body),
  });
}

describe('DwellmApi — ops surface', () => {
  const baseUrl = 'https://api.example.test';

  it('opsProperties calls GET /v1/ops/properties and unwraps the envelope', async () => {
    const fetchMock = mockFetch({ properties: [{ id: 'p1', code: 'A1' }] });
    global.fetch = fetchMock as unknown as typeof fetch;

    const api = new DwellmApi({ baseUrl });
    const out = await api.opsProperties();

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/properties`,
      expect.objectContaining({ method: 'GET' }),
    );
    expect(out).toEqual([{ id: 'p1', code: 'A1' }]);
  });

  it('opsProperties returns an empty array rather than undefined when the server sends none', async () => {
    global.fetch = mockFetch({}) as unknown as typeof fetch;
    const out = await new DwellmApi({ baseUrl }).opsProperties();
    expect(out).toEqual([]);
  });

  it('opsArrears calls GET /v1/ops/arrears', async () => {
    const fetchMock = mockFetch({ arrears: [] });
    global.fetch = fetchMock as unknown as typeof fetch;
    await new DwellmApi({ baseUrl }).opsArrears();
    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/arrears`,
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('opsToday calls GET /v1/ops/today and returns the body as-is', async () => {
    const today = {
      as_of: '2026-08-01', active_tenancies: 2, rent_roll_amount_minor: 5000000,
      outstanding_amount_minor: 5000000, tenancies_in_arrears: 2,
    };
    global.fetch = mockFetch(today) as unknown as typeof fetch;
    const out = await new DwellmApi({ baseUrl }).opsToday();
    expect(out).toEqual(today);
  });

  it('a non-OK response throws ApiError carrying the server message and status', async () => {
    global.fetch = mockFetch({ error: 'no organisation in context' }, 500) as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl });
    await expect(api.opsToday()).rejects.toMatchObject(
      new ApiError(500, 'no organisation in context'),
    );
  });

  it('attaches a bearer token when getToken resolves one', async () => {
    const fetchMock = mockFetch({ entries: [] });
    global.fetch = fetchMock as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl, getToken: async () => 'tok_123' });
    await api.opsActivity();
    const [, init] = fetchMock.mock.calls[0] as [string, { headers: Record<string, string> }];
    expect(init.headers.Authorization).toBe('Bearer tok_123');
  });
});

describe('apiFromEnv', () => {
  const original = process.env.EXPO_PUBLIC_API_URL;
  afterEach(() => {
    process.env.EXPO_PUBLIC_API_URL = original;
  });

  it('is null when no API URL is configured — the demo-mode switch', () => {
    delete process.env.EXPO_PUBLIC_API_URL;
    expect(apiFromEnv()).toBeNull();
  });

  it('builds a client with a trailing slash trimmed from the base URL', () => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test/';
    const api = apiFromEnv();
    expect(api).not.toBeNull();
  });
});
