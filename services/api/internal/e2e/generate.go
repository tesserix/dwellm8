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
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Leases is the slice of the lease module a billing run needs.
type Leases interface {
	Billable(ctx context.Context, l leasedomain.Lease, through effective.Date) ([]leaseservice.Charge, error)
}

// Tenancies lists what to bill, within one organisation's session.
type Tenancies interface {
	Billable(ctx context.Context, limit int) ([]leasedomain.Lease, error)
}

// Organisations lists who to bill for.
//
// A billing run spans the platform and every read inside it does not. The
// alternative — running the whole thing as the platform role, which is exempt
// from row-level security — would put every organisation's leases in one query
// result and make a single wrong join an invoice in somebody else's ledger. So
// the run enumerates organisations once, privileged, and then does all of its
// work inside one organisation's session at a time.
type Organisations interface {
	Active(ctx context.Context) ([]tenancy.ID, error)
}

// Biller raises the invoices.
type Biller interface {
	Bill(ctx context.Context, charges []leaseservice.Charge) (moneyservice.Run, error)
}

// BillingRun is one pass: every billable tenancy, every charge falling due
// inside the horizon, invoiced once.
type BillingRun struct {
	orgs      Organisations
	tenancies Tenancies
	leases    Leases
	biller    Biller
	log       *slog.Logger
}

// NewBillingRun wires the two modules together.
func NewBillingRun(o Organisations, t Tenancies, l Leases, b Biller, log *slog.Logger) *BillingRun {
	return &BillingRun{orgs: o, tenancies: t, leases: l, biller: b, log: log}
}

// Result is what a run did.
type Result struct {
	Organisations int
	Tenancies     int
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
	orgs, err := r.orgs.Active(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("billing run: %w", err)
	}

	var out Result
	for _, org := range orgs {
		out.Organisations++
		// Every read and write below happens inside this organisation's session,
		// so row-level security is doing its job during a run that spans the
		// platform. One organisation's failure does not reach another's.
		one, err := r.billOne(tenancy.With(ctx, org), through, limit)
		if err != nil {
			out.Failed++
			r.log.Error("billing an organisation", "organisation", org, "error", err)
			continue
		}
		out.Tenancies += one.Tenancies
		out.Raised += one.Raised
		out.Duplicate += one.Duplicate
		out.Failed += one.Failed
		out.TotalMinor += one.TotalMinor
	}

	r.log.Info("billing run complete",
		"organisations", out.Organisations, "tenancies", out.Tenancies,
		"raised", out.Raised, "already invoiced", out.Duplicate,
		"failed", out.Failed, "total", out.TotalMinor, "through", through.String())
	return out, nil
}

// billOne bills one organisation, in its own session.
func (r *BillingRun) billOne(ctx context.Context, through effective.Date, limit int) (Result, error) {
	tenancies, err := r.tenancies.Billable(ctx, limit)
	if err != nil {
		return Result{}, err
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

	return out, nil
}
