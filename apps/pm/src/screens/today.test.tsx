import React from 'react';
import { render } from '@testing-library/react-native';
import Today from '../../app/(tabs)/index';

// In live mode the screen shows the manager's own figures or none at all.
// Payouts, jobs and occupancy have no endpoint yet, so they read zero rather
// than the demonstration set's — a manager acting on somebody else's ₹12.8L
// is worse off than one shown nothing (#284, #296).

jest.mock('expo-router', () => ({ useRouter: () => ({ push: jest.fn() }) }));

const mockRoster = {
  mode: 'live' as const, loading: false, reload: jest.fn(),
  billedPaise: 2500000, outstandingPaise: 0, arrearsCount: 0, activeTenancies: 1,
  payoutsPending: 0, payoutsPaise: 0, openTickets: 0, breachingSla: 0,
  visitsDone: 0, inspectionsToday: 0, occupancyPct: 0, vacantUnits: 0,
};

jest.mock('../data/source', () => ({
  useOpsTodayData: () => mockRoster,
  useOpsWho: () => ({ firmName: 'Rout Estates', firstName: 'Samyak' }),
  useOpsWorklist: () => ({ mode: 'live', loading: false, tasks: [] }),
}));

const mockApprovals = { loading: false, data: [] as any[] };
jest.mock('../data/worklists', () => ({
  ...jest.requireActual('../data/worklists'),
  useOpsApprovals: () => mockApprovals,
}));

describe('Today, in live mode', () => {
  beforeEach(() => { mockApprovals.data = []; });

  it('shows no payout, job or occupancy figure it was not given', async () => {
    const { queryByText, getByText } = await render(<Today />);

    expect(queryByText(/12\.8L/)).toBeNull();
    expect(queryByText(/11 open jobs/)).toBeNull();
    expect(queryByText(/94%/)).toBeNull();
    expect(getByText(/payouts due · ₹0/)).toBeTruthy();
    expect(getByText(/open jobs · 0 breaching/)).toBeTruthy();
  });

  it('shows what an automation is asking to spend, and its ceiling', async () => {
    mockApprovals.data = [{
      id: 'ap-1', automation: 'Approve repair quotes', subject_kind: 'ticket',
      subject_id: 'TK-9', action: 'approve_quote',
      amount_minor: 4500000, ceiling_minor: 2500000, requested_at: '2026-08-06T09:00:00Z',
    }];
    const { getByText, queryByText } = await render(<Today />);

    expect(getByText(/Approve repair quotes/)).toBeTruthy();
    expect(getByText(/45,000/)).toBeTruthy();
    // The demonstration set's own approvals must not reach a live screen.
    expect(queryByText(/Sanitary riser/)).toBeNull();
  });

  it('says nothing is waiting rather than borrowing the demonstration set', async () => {
    const { queryByText } = await render(<Today />);
    expect(queryByText(/Waiting on someone else/)).toBeNull();
  });
});
