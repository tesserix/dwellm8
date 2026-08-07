import React from 'react';
import { render, fireEvent, waitFor } from '@testing-library/react-native';
import Collect from '../../app/(tabs)/collect';

// Collections has two tabs. Both read the arrears list, which since #306 holds
// only the tenancies that owe something — so "Whole roster" showed a firm whose
// tenancies are all square that it manages nothing at all (#313).

const mockArrears = jest.fn();
const mockTenancies = jest.fn();

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));

jest.mock('@dwellm8/mobile-shared', () => {
  const actual = jest.requireActual('@dwellm8/mobile-shared');
  return {
    ...actual,
    apiFromEnv: () => ({ opsArrears: mockArrears, opsTenancies: mockTenancies }),
  };
});

const row = (id: string, unit: string, due: number) => ({
  lease_id: id, unit, property: 'Kadavanthra Heights', locality: 'Kadavanthra',
  phone: '+919847012345', rent_amount_minor: 2500000, due_amount_minor: due,
  as_of: '2026-08-07',
});

describe('Collections', () => {
  beforeEach(() => {
    mockArrears.mockReset().mockResolvedValue([row('l1', 'A-302', 2500000)]);
    mockTenancies.mockReset().mockResolvedValue([
      row('l1', 'A-302', 2500000),
      row('l2', 'B-101', 0),
    ]);
  });

  it('shows a tenancy that owes nothing on the whole roster', async () => {
    const { getByText, queryByText } = await render(<Collect />);
    await waitFor(() => expect(queryByText('B-101, Kadavanthra Heights — ₹0')).toBeNull());

    fireEvent.press(getByText('Whole roster'));

    await waitFor(() => expect(getByText(/B-101/)).toBeTruthy());
    expect(getByText('Clear')).toBeTruthy();
  });

  it('keeps the arrears tab to the tenancies that owe', async () => {
    const { getByText, queryByText } = await render(<Collect />);

    await waitFor(() => expect(getByText(/A-302/)).toBeTruthy());
    expect(queryByText(/B-101/)).toBeNull();
    expect(mockTenancies).not.toHaveBeenCalled();
  });
});
