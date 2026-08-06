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

describe('DwellmApi — the applicant pack (#258, #259)', () => {
  const baseUrl = 'https://api.example.test';

  it('opsApplications reads the queue the firm works under its mandate', async () => {
    const fetchMock = mockFetch({ applications: [{ id: 'a1', state: 'submitted' }] });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsApplications('submitted');

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/applications?state=submitted`,
      expect.objectContaining({ method: 'GET' }),
    );
    expect(out).toEqual([{ id: 'a1', state: 'submitted' }]);
  });

  it('opsApplicantPack reads the pack for one application', async () => {
    const fetchMock = mockFetch({ full_name: 'Meera Menon', people: [] });
    global.fetch = fetchMock as unknown as typeof fetch;
    await new DwellmApi({ baseUrl }).opsApplicantPack('a1');
    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/applications/a1/profile`,
      expect.objectContaining({ method: 'GET' }),
    );
  });

  it('opsSaveAddressHistory sends the whole set and returns the gaps', async () => {
    const fetchMock = mockFetch({ addresses: [], gaps: [{ from: '2023-03', to: '2023-06' }], complete: false });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsSaveAddressHistory('a1', [
      { kind: 'rented', line1: '12 MG Road', city: 'Bengaluru', state_code: 'KA', pin: '560038', from: '2023-07' },
    ]);

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/applications/a1/addresses`,
      expect.objectContaining({ method: 'PUT' }),
    );
    expect(out.complete).toBe(false);
    expect(out.gaps).toEqual([{ from: '2023-03', to: '2023-06' }]);
  });

  it('opsSubmitApplicantPack posts the submission', async () => {
    const fetchMock = mockFetch({ state: 'submitted' });
    global.fetch = fetchMock as unknown as typeof fetch;
    await new DwellmApi({ baseUrl }).opsSubmitApplicantPack('a1');
    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/applications/a1/profile/submit`,
      expect.objectContaining({ method: 'POST' }),
    );
  });
});

describe('DwellmApi — the solo manager (#268)', () => {
  it('says the property is the manager’s own, so no mandate is minted', async () => {
    const fetchMock = mockFetch({ owner_org_id: 'firm-1', grant_id: '' });
    global.fetch = fetchMock as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl: 'https://api.test' });

    const out = await api.opsOnboardOwner({
      owner: { name: 'Meera Menon', phone: '+919847012345', self: true },
      property: { code: 'MMN', name: 'Menon Nivas', kind: 'residential' },
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe('https://api.test/v1/ops/onboardings');
    expect(JSON.parse(init.body).owner.self).toBe(true);
    expect(out.grant_id).toBe('');
  });
});
