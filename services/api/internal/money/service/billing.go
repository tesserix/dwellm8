package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
)

// Invoicing. Issue #37.
//
// The lease module says which periods are billable and how much of each the
// tenant occupies; this turns that into money and posts it. The split is
// ADR-0007's: proration is a division, one primitive divides, and it lives here.
// A lease module that pre-divided the rent would be a second rounding
// implementation with its own answer to what half a paisa does.
//
// # Idempotent per period, not per run
//
// The story's failure scenario is a scheduler running twice after a pod
// restart. The key is (lease, period start), so the second run finds the entry
// the first one posted and writes nothing — the same guarantee ADR-0006 gives a
// redelivered webhook, reused rather than reinvented.

// Biller raises invoices from a lease's schedule.
type Biller struct {
	ledger *store.Ledger
	log    *slog.Logger
	now    func() time.Time
}

// NewBiller wires the invoicing service.
func NewBiller(l *store.Ledger, log *slog.Logger, now func() time.Time) *Biller {
	if now == nil {
		now = time.Now
	}
	return &Biller{ledger: l, log: log, now: now}
}

// Invoiced is one charge's outcome.
type Invoiced struct {
	Charge leaseservice.Charge
	// AmountMinor is what was actually billed, after proration.
	AmountMinor domain.Minor
	EntryID     string
	// Duplicate means the period had already been invoiced and nothing was
	// written. Not an error: it is what a second run of the same day looks like.
	Duplicate bool
}

// Amount is what one charge costs, prorated where the period is partial.
//
// Separated from the posting so the arithmetic is testable without a database,
// and because "what would this cost" is a question the lease screen asks before
// anything is owed.
func Amount(c leaseservice.Charge) (domain.Minor, error) {
	full := domain.Minor(c.FullAmountMinor)
	if err := full.Valid(); err != nil {
		return 0, err
	}
	if !c.Partial() {
		return full, nil
	}
	return domain.Prorate(full, c.Days, c.InPeriod)
}

// Raise invoices one charge.
//
// The posting is ADR-0006's invoice template: debit tenant_receivable, credit
// rent_income. No GST — that is MVP 3, and a zero tax line here would be a claim
// that the supply was considered and found exempt rather than not yet modelled.
func (b *Biller) Raise(ctx context.Context, c leaseservice.Charge) (Invoiced, error) {
	amount, err := Amount(c)
	if err != nil {
		return Invoiced{}, fmt.Errorf("money: %s: %w", c.Key(), err)
	}
	if amount <= 0 {
		// A period covering no days, or a rent of nothing. Refused rather than
		// posted: an invoice for zero is a document the tenant has to read and
		// dismiss, and a posting for zero is a line in the ledger that means
		// nothing happened.
		return Invoiced{}, fmt.Errorf("money: %s prorates to %s, which is not a charge",
			c.Key(), amount)
	}

	entry, err := domain.Invoice(amount, 0,
		domain.Place{Property: c.Property, Unit: c.Unit},
		c.Tenant, c.Owner,
		domain.Source{
			Kind: "lease_charge", ID: c.LeaseID, Lease: c.LeaseID,
			IdempotencyKey: c.Key(),
			OccurredOn:     c.DueOn,
			Memo:           c.Reference,
		})
	if err != nil {
		return Invoiced{}, err
	}

	posted, err := b.ledger.Post(ctx, entry)
	if err != nil {
		return Invoiced{}, err
	}
	return Invoiced{Charge: c, AmountMinor: amount, EntryID: posted.ID, Duplicate: posted.Duplicate}, nil
}

// Run is one billing pass over a set of charges.
type Run struct {
	Raised     int
	Duplicate  int
	Failed     int
	TotalMinor domain.Minor
}

// Bill raises every charge it is given and reports what it did.
//
// One failure does not stop the run, for the reason the polling sweep gives: a
// single lease with no recorded owner would otherwise leave every tenancy behind
// it uninvoiced, and a month's rent would go unbilled because of one bad row.
func (b *Biller) Bill(ctx context.Context, charges []leaseservice.Charge) (Run, error) {
	var out Run
	for _, c := range charges {
		inv, err := b.Raise(ctx, c)
		switch {
		case err != nil:
			out.Failed++
			b.log.Error("raising an invoice", "lease", c.LeaseID, "period", c.From, "error", err)
		case inv.Duplicate:
			out.Duplicate++
		default:
			out.Raised++
			out.TotalMinor += inv.AmountMinor
			b.log.Info("invoice raised",
				"lease", c.LeaseID, "period", c.From, "due", c.DueOn,
				"amount", inv.AmountMinor, "prorated", c.Partial())
		}
	}
	return out, nil
}

// ErrNothingToBill is a run with no charges, which is normal and worth
// distinguishing from a run that failed to find any.
var ErrNothingToBill = errors.New("money: no charges fell due")
