// Package e2e also holds the one thing that has to reach both modules at once:
// the billing run.
//
// It is a seam rather than a module. Invoicing needs the lease module to say
// what is billable and the money module to post it, and ADR-0001 §3 forbids
// either reaching into the other — so the coordination lives above both, in the
// same place as the test that exercises it and in the same place `cmd/` would
// otherwise have grown it.
package e2e

import (
	"context"
	"fmt"
	"log/slog"

	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Leases is the slice of the lease module a billing run needs.
type Leases interface {
	Billable(ctx context.Context, l leasedomain.Lease, through effective.Date) ([]leaseservice.Charge, error)
}

// Tenancies lists what to bill.
type Tenancies interface {
	Billable(ctx context.Context, limit int) ([]leasedomain.Lease, error)
}

// Biller raises the invoices.
type Biller interface {
	Bill(ctx context.Context, charges []leaseservice.Charge) (moneyservice.Run, error)
}

// BillingRun is one pass: every billable tenancy, every charge falling due
// inside the horizon, invoiced once.
type BillingRun struct {
	tenancies Tenancies
	leases    Leases
	biller    Biller
	log       *slog.Logger
}

// NewBillingRun wires the two modules together.
func NewBillingRun(t Tenancies, l Leases, b Biller, log *slog.Logger) *BillingRun {
	return &BillingRun{tenancies: t, leases: l, biller: b, log: log}
}

// Result is what a run did.
type Result struct {
	Tenancies int
	moneyservice.Run
}

// Run bills every tenancy up to a horizon.
//
// `through` is "N days ahead" from the story: invoices exist before the money is
// due so the reminder ladder has something to point at, rather than being raised
// on the due date and reminded about after it is late.
//
// A tenancy that fails does not stop the run. One lease with no recorded owner
// would otherwise leave every tenancy behind it uninvoiced — a month's rent
// unbilled because of one bad row, discovered by the owners it did not reach.
func (r *BillingRun) Run(ctx context.Context, through effective.Date, limit int) (Result, error) {
	tenancies, err := r.tenancies.Billable(ctx, limit)
	if err != nil {
		return Result{}, fmt.Errorf("billing run: %w", err)
	}

	var out Result
	for _, l := range tenancies {
		out.Tenancies++
		charges, err := r.leases.Billable(ctx, l, through)
		if err != nil {
			out.Failed++
			r.log.Error("finding what a tenancy owes", "lease", l.ID, "error", err)
			continue
		}
		if len(charges) == 0 {
			continue
		}
		run, err := r.biller.Bill(ctx, charges)
		if err != nil {
			out.Failed++
			r.log.Error("billing a tenancy", "lease", l.ID, "error", err)
			continue
		}
		out.Raised += run.Raised
		out.Duplicate += run.Duplicate
		out.Failed += run.Failed
		out.TotalMinor += run.TotalMinor
	}

	r.log.Info("billing run complete",
		"tenancies", out.Tenancies, "raised", out.Raised,
		"already invoiced", out.Duplicate, "failed", out.Failed,
		"total", out.TotalMinor, "through", through)
	return out, nil
}
