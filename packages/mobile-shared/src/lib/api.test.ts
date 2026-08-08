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

  it('opsProperty asks for the one property and carries its units (#251)', async () => {
    const fetchMock = mockFetch({
      property: { id: 'p1', name: 'Menon Residency' },
      units: [{ id: 'u1', code: '101', occupancy: 'occupied', tenant: 'Priya Nair' }],
    });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsProperty('p1');

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/properties/p1`,
      expect.objectContaining({ method: 'GET' }),
    );
    expect(out.property.name).toBe('Menon Residency');
    expect(out.units[0].tenant).toBe('Priya Nair');
  });

  it('opsProperty gives an empty unit list rather than undefined', async () => {
    global.fetch = mockFetch({ property: { id: 'p1' } }) as unknown as typeof fetch;
    const out = await new DwellmApi({ baseUrl }).opsProperty('p1');
    expect(out.units).toEqual([]);
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

  it('opsPosition reads one tenancy rather than the whole arrears list (#306)', async () => {
    const fetchMock = mockFetch({ lease_id: 'l1', due_amount_minor: 250000 });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsPosition('l1');

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/tenancies/l1/position`,
      expect.objectContaining({ method: 'GET' }),
    );
    expect(out.due_amount_minor).toBe(250000);
  });

  it('opsRecordCollection posts the receipt against the one tenancy (#297)', async () => {
    const fetchMock = mockFetch({
      payment_id: 'pay-1', lease_id: 'l1', status: 'captured',
      amount_minor: 250000, method: 'offline_cash', due_amount_minor: 0, advance_amount_minor: 0,
    });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsRecordCollection('l1', {
      amount_minor: 250000, method: 'offline_cash', reference: 'receipt book 41',
      idempotency_key: 'k1',
    });

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/tenancies/l1/collections`,
      expect.objectContaining({
        method: 'POST',
        body: JSON.stringify({
          amount_minor: 250000, method: 'offline_cash',
          reference: 'receipt book 41', idempotency_key: 'k1',
        }),
      }),
    );
    expect(out.status).toBe('captured');
  });

  it('opsRecordCollection sends cash and no reference when none is given', async () => {
    const fetchMock = mockFetch({ payment_id: 'pay-2', status: 'captured' });
    global.fetch = fetchMock as unknown as typeof fetch;

    await new DwellmApi({ baseUrl }).opsRecordCollection('l1', {
      amount_minor: 100, idempotency_key: 'k2',
    });

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/tenancies/l1/collections`,
      expect.objectContaining({
        body: JSON.stringify({
          amount_minor: 100, method: 'offline_cash', reference: '', idempotency_key: 'k2',
        }),
      }),
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

  it('says in plain English what a status means when the server explained nothing (#343)', async () => {
    // A manager reading "request failed (404)" learns nothing they can act on.
    const cases: Array<[number, string]> = [
      [401, 'Your session has expired. Sign in again.'],
      [403, 'You do not have access to this.'],
      [404, 'This is not available on this server yet.'],
      [429, 'Too many requests just now. Try again shortly.'],
      [500, 'The server had a problem. Try again.'],
    ];
    for (const [status, expected] of cases) {
      global.fetch = jest.fn().mockResolvedValue({
        ok: false, status, headers: { get: () => null }, text: async () => '',
      }) as unknown as typeof fetch;
      const api = new DwellmApi({ baseUrl });
      await expect(api.opsToday()).rejects.toMatchObject({ status, message: expected });
    }
  });

  it('says a session has expired in a sentence, whatever the server called it (#349)', async () => {
    // The server answers 401 with "sign in again", which a screen rendered
    // where its data should have been. Expiry is the client's own business.
    global.fetch = mockFetch({ error: 'sign in again' }, 401) as unknown as typeof fetch;
    await expect(new DwellmApi({ baseUrl }).opsToday()).rejects.toMatchObject({
      status: 401, message: 'Your session has expired. Sign in again.',
    });
  });

  it('a refusal that names a field carries it, so the form can say it at the field (#287)', async () => {
    global.fetch = mockFetch(
      { error: 'that GSTIN is not a GSTIN', field: 'gstin' }, 422) as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl });
    await expect(api.opsToday()).rejects.toMatchObject({ status: 422, field: 'gstin' });
  });

  it('a refusal that says when to come back carries the wait, not just the words', async () => {
    // A screen that has to regex-scrape "try again in 43 seconds" out of a
    // sentence breaks the day somebody rewords the sentence.
    global.fetch = jest.fn().mockResolvedValue({
      ok: false,
      status: 429,
      headers: { get: (h: string) => (h.toLowerCase() === 'retry-after' ? '1800' : null) },
      text: async () => JSON.stringify({ error: 'too many codes have been sent — try again later' }),
    }) as unknown as typeof fetch;

    const api = new DwellmApi({ baseUrl });
    await expect(api.sendEmailCode()).rejects.toMatchObject({
      status: 429,
      retryAfterSeconds: 1800,
      message: 'too many codes have been sent — try again later',
    });
  });

  it('falls back to the body when there is no Retry-After header', async () => {
    global.fetch = mockFetch({ error: 'a code was just sent', resend_after_seconds: 42 }, 429) as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl });
    await expect(api.sendEmailCode()).rejects.toMatchObject({ retryAfterSeconds: 42 });
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

describe('DwellmApi — address lookup', () => {
  const baseUrl = 'https://api.example.test';

  it('searches addresses through GET /v1/places/autocomplete with the term encoded', async () => {
    const fetchMock = mockFetch({
      suggestions: [{ description: 'Chandra Arcade, Kochi', line1: '12 Kadavanthra Road',
        city: 'Kochi', state_code: 'KL', pin_code: '682020' }],
    });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).searchAddresses('chandra arcade, kochi');

    expect(fetchMock.mock.calls[0][0])
      .toBe(`${baseUrl}/v1/places/autocomplete?q=chandra%20arcade%2C%20kochi`);
    expect(out[0].state_code).toBe('KL');
  });

  it('returns an empty list rather than undefined when nothing matched', async () => {
    global.fetch = mockFetch({}) as unknown as typeof fetch;
    expect(await new DwellmApi({ baseUrl }).searchAddresses('zzzz')).toEqual([]);
  });

  it('does not call the geocoder for a term too short to match anything', async () => {
    const fetchMock = mockFetch({ suggestions: [] });
    global.fetch = fetchMock as unknown as typeof fetch;
    expect(await new DwellmApi({ baseUrl }).searchAddresses('ko')).toEqual([]);
    expect(fetchMock).not.toHaveBeenCalled();
  });
});

describe('apiFromEnv — which token the client actually sends', () => {
  afterEach(() => setTokenSource(null));

  it('uses the session-wide refreshing source when no getToken is passed (#283)', async () => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    const fetchMock = mockFetch({});
    global.fetch = fetchMock as unknown as typeof fetch;

    let issued = 'tok_stale';
    setTokenSource(async () => issued);
    const api = apiFromEnv()!;

    await api.opsToday();
    issued = 'tok_fresh';
    await api.opsToday();

    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok_stale');
    expect(fetchMock.mock.calls[1][1].headers.Authorization).toBe('Bearer tok_fresh');
  });

  it('an explicit getToken pins the client to it, refresh or no refresh (#283)', async () => {
    process.env.EXPO_PUBLIC_API_URL = 'https://api.example.test';
    const fetchMock = mockFetch({});
    global.fetch = fetchMock as unknown as typeof fetch;

    setTokenSource(async () => 'tok_fresh');
    // This is the trap: a screen that captures the token it was rendered with
    // keeps sending it after it has expired.
    await apiFromEnv(async () => 'tok_captured')!.opsToday();

    expect(fetchMock.mock.calls[0][1].headers.Authorization).toBe('Bearer tok_captured');
  });
});

describe('DwellmApi — the firm’s own team (#353)', () => {
  const baseUrl = 'https://api.example.test';

  it('opsTeam reads roles, people and who holds what in one call', async () => {
    const fetchMock = mockFetch({
      roles: [{ id: 'r1', name: 'Field Executive', permissions: ['property.read'], property_limit: 6 }],
      team: [{ id: 's1', full_name: 'Asha Nair', state: 'active', property_limit: 6, held: 2 }],
      assignments: [{ id: 'a1', staff_id: 's1', property_id: 'p1', property_name: 'Menon Residency' }],
    });
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsTeam();

    expect(fetchMock).toHaveBeenCalledWith(
      `${baseUrl}/v1/ops/staff`, expect.objectContaining({ method: 'GET' }));
    expect(out.team[0].full_name).toBe('Asha Nair');
    expect(out.team[0].held).toBe(2);
    expect(out.assignments[0].property_name).toBe('Menon Residency');
  });

  it('opsTeam gives empty lists rather than undefined when the firm has nobody', async () => {
    global.fetch = mockFetch({}) as unknown as typeof fetch;
    const out = await new DwellmApi({ baseUrl }).opsTeam();
    expect(out).toEqual({ roles: [], team: [], assignments: [] });
  });

  it('opsEmployStaff sends the PAN once and reads back only the mask', async () => {
    const fetchMock = mockFetch({
      member: { id: 's1', full_name: 'Asha Nair', pan_masked: 'XXXXXX234F', state: 'active' },
    }, 201);
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsEmployStaff({
      full_name: 'Asha Nair', phone: '+919876500001', role_id: 'r1',
      employment_type: 'full_time', joined_on: '2026-01-05',
      pan: 'ABCDE1234F', salary_minor: 4500000, pay_frequency: 'monthly',
    });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/ops/staff`);
    expect(init.method).toBe('POST');
    expect(JSON.parse(init.body).pan).toBe('ABCDE1234F');
    expect(out.pan_masked).toBe('XXXXXX234F');
  });

  it('opsSaveStaffRole names a job and the load it carries', async () => {
    const fetchMock = mockFetch({ role: { id: 'r1', name: 'Field Executive', property_limit: 6 } }, 201);
    global.fetch = fetchMock as unknown as typeof fetch;

    const out = await new DwellmApi({ baseUrl }).opsSaveStaffRole({
      name: 'Field Executive', permissions: ['property.read'], property_limit: 6,
    });

    expect(fetchMock.mock.calls[0][0]).toBe(`${baseUrl}/v1/ops/staff/roles`);
    expect(out.property_limit).toBe(6);
  });

  it('opsUpdateStaff dates an exit rather than deleting the record', async () => {
    const fetchMock = mockFetch({ updated: true });
    global.fetch = fetchMock as unknown as typeof fetch;

    await new DwellmApi({ baseUrl }).opsUpdateStaff('s1', { state: 'exited', exited_on: '2026-03-31' });

    const [url, init] = fetchMock.mock.calls[0];
    expect(url).toBe(`${baseUrl}/v1/ops/staff/s1`);
    expect(init.method).toBe('PATCH');
    expect(JSON.parse(init.body).exited_on).toBe('2026-03-31');
  });

  it('opsAssignProperty and opsReleaseAssignment move one building', async () => {
    const fetchMock = mockFetch({ assignment: { id: 'a1', staff_id: 's1', property_id: 'p1' } }, 201);
    global.fetch = fetchMock as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl });

    await api.opsAssignProperty('s1', 'p1');
    expect(fetchMock.mock.calls[0][0]).toBe(`${baseUrl}/v1/ops/staff/s1/assignments`);
    expect(JSON.parse(fetchMock.mock.calls[0][1].body).property_id).toBe('p1');

    global.fetch = mockFetch({ released: true }) as unknown as typeof fetch;
    await api.opsReleaseAssignment('a1');
    expect((global.fetch as jest.Mock).mock.calls[0][0])
      .toBe(`${baseUrl}/v1/ops/staff/assignments/a1`);
    expect((global.fetch as jest.Mock).mock.calls[0][1].method).toBe('DELETE');
  });

  it('the rota is read and written as a whole week', async () => {
    const week = [{ weekday: 1, starts_at: '09:00', ends_at: '18:00' }];
    const fetchMock = mockFetch({ shifts: week });
    global.fetch = fetchMock as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl });

    const got = await api.opsRota('s1');
    expect(fetchMock.mock.calls[0][0]).toBe(`${baseUrl}/v1/ops/staff/s1/shifts`);
    expect(got).toEqual(week);

    await api.opsSetRota('s1', week);
    expect(fetchMock.mock.calls[1][1].method).toBe('PUT');
    expect(JSON.parse(fetchMock.mock.calls[1][1].body).shifts).toEqual(week);
  });

  it('an empty rota comes back as an empty week, not undefined', async () => {
    global.fetch = mockFetch({}) as unknown as typeof fetch;
    expect(await new DwellmApi({ baseUrl }).opsRota('s1')).toEqual([]);
  });
});

describe('DwellmApi — taking things off the book (#356)', () => {
  const baseUrl = 'https://api.example.test';

  it('retiring a building, a home and a bed each DELETE their own record', async () => {
    const fetchMock = mockFetch({}, 204);
    global.fetch = fetchMock as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl });

    await api.opsRetireProperty('p1', 'sold');
    await api.opsRetireUnit('u1', '');
    await api.opsRetireBed('b1', 'room knocked through');

    expect(fetchMock.mock.calls.map((c) => [c[0], (c[1] as RequestInit).method])).toEqual([
      [`${baseUrl}/v1/ops/properties/p1?reason=sold`, 'DELETE'],
      [`${baseUrl}/v1/ops/units/u1`, 'DELETE'],
      [`${baseUrl}/v1/ops/beds/b1?reason=room%20knocked%20through`, 'DELETE'],
    ]);
  });

  it('resigning a mandate names the grant, and closing the firm is its own act', async () => {
    const fetchMock = mockFetch({}, 204);
    global.fetch = fetchMock as unknown as typeof fetch;
    const api = new DwellmApi({ baseUrl });

    await api.opsResignMandate('g1');
    await api.opsCloseFirm('stopped letting');

    expect(fetchMock.mock.calls.map((c) => [c[0], (c[1] as RequestInit).method])).toEqual([
      [`${baseUrl}/v1/ops/portfolios/g1`, 'DELETE'],
      [`${baseUrl}/v1/ops/firm/closure?reason=stopped%20letting`, 'POST'],
    ]);
  });

  it('gives back the guard’s own sentence when the tenant is still there', async () => {
    global.fetch = mockFetch(
      { error: 'a home somebody is living in cannot be retired' }, 422,
    ) as unknown as typeof fetch;

    await expect(new DwellmApi({ baseUrl }).opsRetireUnit('u1', ''))
      .rejects.toThrow('a home somebody is living in cannot be retired');
  });
});
