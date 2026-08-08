import React from 'react';
import { render, waitFor } from '@testing-library/react-native';
import Property from '../../app/property';

// #311 stopped the tiles counting a spoken-for unit as empty. The unit's own
// row still called it "Vacant", so a manager reading the list — which is where
// a flat gets offered from — was told it was free (#314).

const mockProperty = jest.fn();
const mockDocuments = jest.fn();

jest.mock('expo-router', () => ({
  useRouter: () => ({ back: jest.fn(), push: jest.fn() }),
  useLocalSearchParams: () => ({ id: 'p1' }),
}));

jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return { ...actual, apiFromEnv: () => ({ opsProperty: mockProperty, opsPropertyDocuments: mockDocuments }) };
});

const unit = (over: Record<string, unknown>) => ({
  id: 'u1', code: '101', kind: 'flat', floor: 1, occupancy: 'vacant', ...over,
});

describe('Property record', () => {
  beforeEach(() => {
    mockDocuments.mockReset().mockResolvedValue({
      documents: [],
      ownership: { proven: false, held: [], missing: ['title_deed', 'power_of_attorney'], advisory: [] },
    });
    mockProperty.mockReset().mockResolvedValue({
      property: {
        id: 'p1', name: 'Kadavanthra Heights', locality: 'Kadavanthra', city: 'Kochi',
        kind: 'building', reference: 'KVH', address_line1: '18 Chandra Nagar Road',
        units: 2, rent_amount_minor: 0,
      },
      units: [
        unit({ id: 'u1', code: '101', let_from: '2026-08-10', rent_amount_minor: 2500000 }),
        unit({ id: 'u2', code: '102' }),
      ],
    });
  });

  it('does not call a spoken-for unit vacant', async () => {
    const { getByText, queryAllByText } = await render(<Property />);
    await waitFor(() => expect(getByText('101')).toBeTruthy());

    // 102 is genuinely empty; 101 is not, so exactly one row says so.
    expect(queryAllByText('Vacant')).toHaveLength(1);
    expect(getByText(/From 2026-08-10/)).toBeTruthy();
  });

  it('gives the move-in date once, not in the pill and the line beneath it', async () => {
    const { queryAllByText } = await render(<Property />);
    await waitFor(() => expect(queryAllByText(/2026-08-10/).length).toBeGreaterThan(0));

    expect(queryAllByText(/2026-08-10/)).toHaveLength(1);
  });

  // A flat let on somebody's say-so has no answer when the real owner appears,
  // so the record says whether the deed is on file (#339).
  it('says the deed is still wanted when nothing proves ownership', async () => {
    const { getByText } = await render(<Property />);
    await waitFor(() => expect(getByText('Ownership')).toBeTruthy());

    expect(getByText(/deed or a power of attorney is still wanted/)).toBeTruthy();
  });

  // A renter decides on the description and what is near the building long
  // before anybody shows them a rent (#354).
  it('offers the description and what is nearby, saying when neither is written', async () => {
    const { getByText } = await render(<Property />);
    await waitFor(() => expect(getByText('Ownership')).toBeTruthy());

    expect(getByText('About this property')).toBeTruthy();
    expect(getByText(/Nothing written yet/)).toBeTruthy();
    expect(getByText("What's nearby")).toBeTruthy();
  });

  it('says what is already written about the building', async () => {
    mockProperty.mockResolvedValue({
      property: {
        id: 'p1', name: 'Kadavanthra Heights', locality: 'Kadavanthra', city: 'Kochi',
        kind: 'building', address_line1: '18 Chandra Nagar Road', unit_count: 2,
        about: 'A quiet block.', amenities: ['lift', 'gym'],
      },
      units: [],
    });
    const { getByText } = await render(<Property />);
    await waitFor(() => expect(getByText('About this property')).toBeTruthy());

    expect(getByText('2 amenities listed')).toBeTruthy();
  });
});
