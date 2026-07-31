package service

import (
	"context"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// What the money module needs in order to bill, in terms it can name.
//
// Charge is deliberately a plain struct of primitives rather than
// domain.Period: ADR-0001 §3 stops one module importing another's domain, and
// money reaching for `leasedomain.Period` would be exactly that import wearing a
// service package as a hat. The seam is here, and it is the one place the two
// vocabularies meet.
//
// It also carries no money beyond the *whole* period's rent. The proration is
// money's arithmetic (ADR-0007), and a lease module that pre-divided the rent
// would be a second rounding implementation with a different answer.

// Charge is one period of one tenancy, ready to be invoiced.
type Charge struct {
	LeaseID  string
	Property string
	Unit     string
	// Tenant and Owner are the parties the posting is kept per.
	Tenant string
	Owner  string

	// From and To bound what the charge is for, half-open.
	From, To time.Time
	// DueOn is the day it falls due.
	DueOn time.Time
	// FullAmountMinor is a whole cycle's rent. Not this period's — see Days.
	FullAmountMinor int64
	// Days and InPeriod are the proration fraction: equal for a whole period,
	// and the pair money.Prorate takes.
	Days, InPeriod int
	// Reference is what the tenant sees on the invoice.
	Reference string
}

// Partial reports whether this charge covers part of a cycle.
func (c Charge) Partial() bool { return c.Days != c.InPeriod }

// Key is the idempotency key for the invoice this becomes.
//
// One per lease per period start, so a scheduler that runs twice — a pod
// restart, an overlapping cron, a manual re-run after an incident — produces one
// invoice. The period start rather than the due date, because a due date can be
// revised and the period it belongs to cannot.
func (c Charge) Key() string {
	return fmt.Sprintf("invoice:%s:%s", c.LeaseID, c.From.Format("2006-01-02"))
}

// Billable returns every charge falling due up to a horizon, for one lease.
//
// The horizon is how far ahead invoices are raised — the story's "N days ahead",
// which exists so the reminder ladder has something to point at before the money
// is late rather than after.
func (s *Leases) Billable(ctx context.Context, l domain.Lease, through effective.Date) ([]Charge, error) {
	if !l.State.Billable() {
		// A draft bills nothing and a terminated tenancy stops. Checked here rather
		// than left to the schedule, because "generates no further invoices after
		// its final period" is a property of the state, and a schedule asked about
		// a terminated lease would still describe periods its occupancy ended
		// before.
		return nil, nil
	}

	terms, err := s.store.Terms(ctx, l.ID, l.Term.From())
	if err != nil {
		return nil, fmt.Errorf("lease %s: %w", l.ID, err)
	}
	periods, err := l.Schedule(terms, through)
	if err != nil {
		return nil, err
	}

	// As at the tenancy's start, not today: a lease beginning next month has
	// nobody on it today, and billing it ahead of time is the whole point of a
	// horizon.
	tenant, owner, err := s.store.Parties(ctx, l.ID, l.Term.From())
	if err != nil {
		return nil, err
	}

	out := make([]Charge, 0, len(periods))
	for _, p := range periods {
		if p.DueOn.After(through) {
			// Beyond the horizon. The schedule generates the period containing the
			// horizon, which is right for a schedule and wrong for a billing run:
			// raising an invoice nobody asked for yet is money on a statement
			// before it is owed.
			continue
		}
		out = append(out, Charge{
			LeaseID: l.ID, Property: l.Property, Unit: l.Unit,
			Tenant: tenant, Owner: owner,
			From: p.From.Time(), To: p.To.Time(), DueOn: p.DueOn.Time(),
			FullAmountMinor: terms.RentMinor,
			Days:            p.Days, InPeriod: p.InPeriod,
			Reference: reference(p.From.Time(), p.To.Time(), p.Days != p.InPeriod),
		})
	}
	return out, nil
}

// reference is what the tenant reads on the invoice.
//
// A part period names its days. Two invoices in one month is the ordinary case —
// a tenancy starting on the 1st with rent due on the 5th owes four days and then
// a month — and two lines both saying "Rent, July 2026" on the same statement is
// the kind of thing that generates a support ticket rather than a payment.
func reference(from, to time.Time, partial bool) string {
	if !partial {
		return fmt.Sprintf("Rent, %s", from.Format("January 2006"))
	}
	return fmt.Sprintf("Rent, %s to %s", from.Format("2 January"), to.Format("2 January 2006"))
}
