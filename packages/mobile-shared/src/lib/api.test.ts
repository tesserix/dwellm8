import { apiFromEnv, ApiError, DwellmApi, setTokenSource } from './api';

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

  it('signs a request with the app-wide token source when the client has none', async () => {
    const fetchMock = mockFetch({ settlements: [] });
    global.fetch = fetchMock as unknown as typeof fetch;
    setTokenSource(async () => 'tok_session');

    await new DwellmApi({ baseUrl: 'https://api.example.test' }).opsSettlements();

    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok_session');
    setTokenSource(null);
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

  it('opsMerchants reads every connected account', async () => {
    const fetchMock = mockFetch({ accounts: [{ provider: 'cashfree', may_collect: true }] });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsMerchants();

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/merchant`,
      expect.objectContaining({ method: 'GET' }),
    );
    expect(out).toEqual([{ provider: 'cashfree', may_collect: true }]);
  });

  it('opsConnectMerchant sends the account once and keeps nothing back', async () => {
    const fetchMock = mockFetch({ provider: 'cashfree', state: 'submitted', settlement_masked: 'XXXXXXXXXX4321' });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsConnectMerchant({
      provider: 'cashfree', business_name: 'Menon Properties', business_type: 'proprietorship',
      pan: 'ABCDE1234F', account_number: '50100123454321', account_holder: 'Menon Properties',
      ifsc: 'HDFC0001234',
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/ops/merchant`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body).account_number).toBe('50100123454321');
    expect(out.settlement_masked).toBe('XXXXXXXXXX4321');
  });

  it('opsRefreshMerchant asks the provider again', async () => {
    const fetchMock = mockFetch({ provider: 'cashfree', state: 'verified', may_collect: true });
    global.fetch = fetchMock as unknown as typeof fetch;

    await new DwellmApi({ baseUrl }).opsRefreshMerchant('cashfree');

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/merchant/cashfree/refresh`,
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('opsSettlements reads the queue of what is owed', async () => {
    const fetchMock = mockFetch({ settlements: [{ id: 's1', owner_amount_minor: 2767036, overdue: true }] });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsSettlements();

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/settlements`,
      expect.objectContaining({ method: 'GET' }),
    );
    expect(out[0].overdue).toBe(true);
  });

  it('opsReleaseSettlement names who is being paid', async () => {
    const fetchMock = mockFetch({ id: 's1', state: 'instructed' });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsReleaseSettlement('s1', 'BENE-1');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/ops/settlements/s1/release`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body).beneficiary_ref).toBe('BENE-1');
    expect(out.state).toBe('instructed');
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

describe('DwellmApi — first sign-in', () => {
  const baseUrl = 'https://api.example.test';

  it('me reads the signed-in person', async () => {
    const fetchMock = mockFetch({ party_id: 'pty-1', email: 'ritika@example.test' });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).me();

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/me`, expect.objectContaining({ method: 'GET' }),
    );
    expect(out?.party_id).toBe('pty-1');
  });

  it('me is null when the sign-in has no account yet, rather than throwing', async () => {
    global.fetch = mockFetch({ error: 'this sign-in has no account yet' }, 404) as unknown as typeof fetch;
    expect(await new DwellmApi({ baseUrl }).me()).toBeNull();
  });

  it('onboard names the firm the manager is creating', async () => {
    const fetchMock = mockFetch({ organisation_id: 'org-1', party_id: 'pty-1', role: 'owner', created: true }, 201);
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).onboard('Menon Properties');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/onboarding`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body)).toEqual({ organisation_name: 'Menon Properties' });
    expect(out.created).toBe(true);
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

describe('DwellmApi — email verification (#282)', () => {
  const baseUrl = 'https://api.test';

  it('sends a code to the address the manager signed up with', async () => {
    const fetchMock = mockFetch({ email: 'pm@example.test', verified: false, resend_after_seconds: 60 }, 202);
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).sendEmailCode();

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/verification/email`);
    expect(init.method).toBe('POST');
    expect(out.resend_after_seconds).toBe(60);
  });

  it('confirms the code the manager typed', async () => {
    const fetchMock = mockFetch({ email: 'pm@example.test', verified: true });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).confirmEmailCode('123456');

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/verification/email/confirm`);
    expect(JSON.parse(init.body)).toEqual({ code: '123456' });
    expect(out.verified).toBe(true);
  });

  it('reads whether the address is already proved, and how long the button stays dead', async () => {
    const fetchMock = mockFetch({ email: 'pm@example.test', verified: false, resend_after_seconds: 41 });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).emailVerification();

    expect(fetchMock.mock.calls[0][0]).toBe(`${baseUrl}/v1/verification/email`);
    expect(fetchMock.mock.calls[0][1].method).toBe('GET');
    expect(out.resend_after_seconds).toBe(41);
  });
});

describe('DwellmApi — the firm’s statutory registration (#282)', () => {
  const baseUrl = 'https://api.test';

  it('reads the checklist, defaulting every list so a screen can map over it', async () => {
    global.fetch = mockFetch({ legal_name: 'Menon Estates', constitution: 'llp', state: 'draft' }) as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsRegistration();

    expect(out.constitution).toBe('llp');
    expect(out.outstanding).toEqual([]);
    expect(out.required).toEqual([]);
    expect(out.authorities).toEqual([]);
    expect(out.fields).toEqual([]);
  });

  it('sends the PAN whole — the server masks it, and no screen stores it', async () => {
    const fetchMock = mockFetch({ legal_name: 'Menon Estates', pan_masked: 'XXXXXX234F' });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsSaveRegistration({
      legal_name: 'Menon Estates', constitution: 'llp', pan: 'ABCDE1234F', registrar_id: 'AAA-1234',
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/ops/registration`);
    expect(init.method).toBe('PUT');
    expect(JSON.parse(init.body).pan).toBe('ABCDE1234F');
    expect(out.pan_masked).toBe('XXXXXX234F');
  });

  it('files one state’s agent registration with its validity window', async () => {
    const fetchMock = mockFetch({ legal_name: 'Menon Estates', authorities: [{ id: 'r1' }] });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsFileRegistration({
      state_code: 'KL', number: 'K-RERA/AG/123', valid_from: '2026-04-01', valid_to: '2031-03-31',
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/ops/registration/authorities`);
    expect(JSON.parse(init.body)).toEqual({
      authority: 'rera', state_code: 'KL', number: 'K-RERA/AG/123',
      valid_from: '2026-04-01', valid_to: '2031-03-31',
    });
    expect(out.authorities).toHaveLength(1);
  });

  it('records a document that has already reached the bucket', async () => {
    const fetchMock = mockFetch({ legal_name: 'Menon Estates', outstanding: [] });
    global.fetch = fetchMock as unknown as typeof fetch;

    await new DwellmApi({ baseUrl }).opsRecordDocument({
      kind: 'llp_agreement', object_path: 'firm/llp.pdf', filename: 'llp.pdf', content_type: 'application/pdf',
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/ops/registration/documents`);
    expect(JSON.parse(init.body).kind).toBe('llp_agreement');
    expect(JSON.parse(init.body).expires_on).toBe('');
  });
});
