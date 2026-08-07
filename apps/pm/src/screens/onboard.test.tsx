import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react-native';
import Onboard from '../../app/onboard';

// The once-over calls itself the look before it becomes the record, and the
// deposit — the largest number in the transaction — was not on it (#317).

jest.mock('expo-router', () => ({ useRouter: () => ({ back: jest.fn(), replace: jest.fn() }) }));

jest.mock('../data/firm', () => ({ useFirmContact: () => ({ name: '', phone: '' }) }));

let mockApi: unknown = null;
jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return { ...actual, apiFromEnv: () => mockApi };
});

const mockPhotograph = jest.fn();
jest.mock('../data/capture', () => ({
  ...jest.requireActual('../data/capture'),
  photographDocument: () => mockPhotograph(),
}));

beforeEach(() => { mockApi = null; mockPhotograph.mockReset(); });

const fill = (label: string | RegExp, text: string) =>
  fireEvent.changeText(screen.getByLabelText(label), text);
// The button, not the label inside it: pressing the Text bubbles past a
// disabled Pressable, which no thumb on a real screen can do.
const next = () => fireEvent.press(screen.getByLabelText('Next'));

async function walkToTheOnceOver() {
  await fill("Owner's name", 'Anjali Menon');
  await fill('Mobile number', '+919999000001');
  await next();

  // Who they are, for tax (#318). An Indian resident with no PAN is a complete
  // answer — 206AA's 20% is a rate, not an unfinished form — so it walks past.
  await next();

  await fill('Property name', 'Menon Palm Court');
  await fill('Address', 'Panampilly Nagar Avenue');
  await fill('Locality', 'Panampilly Nagar');
  await fill('City', 'Ernakulam');
  await fill(/State code/i, 'KL');
  await fill(/PIN code/i, '682036');
  await next();

  await fill('Unit codes', 'G1, G2, 101');
  await fill(/Carpet area/i, '1150');
  await next();

  await fireEvent(screen.getByLabelText('Set up the first tenancy'), 'valueChange', true);
  await fill("Tenant's name", 'Rahul Varghese');
  await fill("Tenant's mobile", '+919999000002');
  await fill(/Which unit/i, 'G1');
  await fill(/Monthly rent/i, '38000');
  await fill(/Deposit/i, '114000');
  await next();

  await waitFor(() => expect(screen.getByText('One look before it becomes the record.')).toBeTruthy());
}

describe('Onboard — the once-over', () => {
  it('shows the deposit that is about to become the record', async () => {
    await render(<Onboard />);
    await walkToTheOnceOver();

    expect(screen.getByText(/₹1,14,000/)).toBeTruthy();
  });

  it('still shows the rent and the term beside it', async () => {
    await render(<Onboard />);
    await walkToTheOnceOver();

    expect(screen.getByText(/₹38,000/)).toBeTruthy();
    expect(screen.getByText(/→/)).toBeTruthy();
  });
});

// Who they are, for tax (#318). Non-residency opens rule 37BC(2)'s five
// particulars, and the step says plainly how many are still missing — which is
// the difference between the treaty rate and 206AA's 20% floor.
describe('Onboard — who they are', () => {
  const toTheIdentityStep = async () => {
    await fill("Owner's name", 'Anjali Menon');
    await fill('Mobile number', '+919999000001');
    await next();
  };

  it('asks a non-resident for the particulars that keep them out of 206AA', async () => {
    await render(<Onboard />);
    await toTheIdentityStep();

    expect(screen.queryByLabelText(/Foreign tax identification/i)).toBeNull();
    await fireEvent(screen.getByLabelText('The owner lives in India'), 'valueChange', false);

    expect(screen.getByLabelText(/Foreign tax identification/i)).toBeTruthy();
    expect(screen.getByLabelText(/Tax residency certificate number/i)).toBeTruthy();
    expect(screen.getByText(/Still missing/)).toBeTruthy();
  });

  it('does not walk on until it knows which country', async () => {
    await render(<Onboard />);
    await toTheIdentityStep();
    await fireEvent(screen.getByLabelText('The owner lives in India'), 'valueChange', false);

    await next();
    expect(screen.getByText(/Living abroad/)).toBeTruthy();

    await fill(/Country of residence/i, 'AE');
    await next();
    await waitFor(() => expect(screen.getByLabelText('Property name')).toBeTruthy());
  });
});

// The copy behind the identity (#318). An owner abroad signs and dates a
// photocopy; what the copy is worth depends on that, so the wizard asks rather
// than assuming, and holds the photograph until the party exists to file against.
describe('Onboard — the copies', () => {
  const aPassportShot = { base64: 'aGk=', uri: 'file:///tmp/p.jpg', contentType: 'image/jpeg' };

  const toTheIdentityStep = async () => {
    await fill("Owner's name", 'Anjali Menon');
    await fill('Mobile number', '+919999000001');
    await next();
  };

  it('keeps the photographed passport, attested, for filing later', async () => {
    mockApi = {
      opsPortfolios: jest.fn().mockResolvedValue([]),
      opsScanPassport: jest.fn().mockResolvedValue({
        surname: 'MENON', given_names: 'ANJALI', number: 'L898902C3',
        nationality: 'IND', date_of_birth: '1988-04-11', expires_on: '2029-05-01',
      }),
    };
    mockPhotograph.mockResolvedValue(aPassportShot);

    await render(<Onboard />);
    await toTheIdentityStep();
    await fireEvent(screen.getByLabelText('The owner lives in India'), 'valueChange', false);
    await fireEvent.press(screen.getByLabelText('Photograph the passport'));

    await waitFor(() => expect(screen.getByText(/The passport will be filed/)).toBeTruthy());
  });

  it('asks how the copy was attested rather than assuming it', async () => {
    await render(<Onboard />);
    await toTheIdentityStep();

    const row = screen.getByLabelText('The owner signed and dated the copy');
    expect(row.props.value).toBe(true);
    await fireEvent(row, 'valueChange', false);
    expect(screen.getByLabelText('The owner signed and dated the copy').props.value).toBe(false);
  });
});
