package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
)

// The platform fee. Issue #234, ADR-0031.
//
// Two steps, deliberately separate. Quote runs before the provider is called,
// because the split has to be on the order; Post runs at capture, because a fee
// on money that never arrived is income we did not earn.

// Fees prices a collection and posts what it earned.
type Fees struct {
	rules   *store.Fees
	payouts *store.PayoutAccounts
	ledger  *store.Ledger
	log     *slog.Logger
}

// NewFees wires the service.
func NewFees(r *store.Fees, p *store.PayoutAccounts, l *store.Ledger, log *slog.Logger) *Fees {
	return &Fees{rules: r, payouts: p, ledger: l, log: log}
}

// Accrual says why a fee could not be retained at capture.
type Accrual string

const (
	// AccrualNone is the ordinary case: the aggregator retained it.
	AccrualNone Accrual = ""
	// AccrualNoVendor is a bearer the aggregator has never been told about, or
	// one whose payout account is inside its cool-off.
	AccrualNoVendor Accrual = "no_vendor"
	// AccrualOffline is cash, NEFT or IMPS paid straight to the bearer. The
	// majority of Indian rent, and money this platform never touches.
	AccrualOffline Accrual = "offline"
	// AccrualNothingToCharge is a rate of zero or an amount too small to round.
	AccrualNothingToCharge Accrual = "nothing_to_charge"
)

// Quote is what a collection will earn and how it will be collected.
type Quote struct {
	Fee domain.Fee
	// Split is nil when the fee accrues instead of being retained.
	Split   *provider.Split
	Accrual Accrual
}

// Retained reports whether the aggregator will hold the fee back at capture.
func (q Quote) Retained() bool { return q.Split != nil }

// Quote prices a collection against the rule in force and decides how the fee
// will be collected.
//
// It never fails the collection. A missing vendor or an offline method is an
// accrual, not a refusal — refusing to take a tenant's rent because we could not
// arrange our own fee would be the wrong trade every time.
func (f *Fees) Quote(ctx context.Context, gross domain.Minor, bearer string, method collect.Method, on time.Time) (Quote, error) {
	schedule, err := f.rules.Schedule(ctx, on)
	if err != nil {
		return Quote{}, err
	}
	fee, err := schedule.Charge(gross)
	if err != nil {
		return Quote{}, err
	}
	if fee.Zero() {
		return Quote{Fee: fee, Accrual: AccrualNothingToCharge}, nil
	}
	if method.IsOffline() {
		return Quote{Fee: fee, Accrual: AccrualOffline}, nil
	}

	vendor, err := f.payouts.Vendor(ctx, bearer)
	switch {
	case errors.Is(err, store.ErrNoVendor), errors.Is(err, store.ErrNoAccount):
		f.log.Info("the fee will accrue", "bearer", bearer, "reason", AccrualNoVendor)
		return Quote{Fee: fee, Accrual: AccrualNoVendor}, nil
	case err != nil:
		return Quote{}, fmt.Errorf("money: resolving the split vendor: %w", err)
	}
	return Quote{Fee: fee, Split: &provider.Split{VendorID: vendor, Amount: fee.Vendor}}, nil
}

// Post writes the fee to the ledger.
//
// Called at capture and only at capture, keyed on the payment, so a redelivered
// confirmation posts one fee however many times it arrives. Whether the money
// was retained or accrued does not change the posting — it changes only who is
// holding the cash — which is why there is one path here and not two.
func (f *Fees) Post(ctx context.Context, q Quote, p collect.Payment, bearer string, at time.Time) error {
	if q.Fee.Zero() {
		return nil
	}
	entry, err := domain.PlatformFee(q.Fee.Net, q.Fee.Tax,
		domain.Place{Property: p.Property, Unit: p.Unit}, bearer,
		domain.Source{
			Kind: "platform_fee", ID: p.ID, Lease: p.Lease,
			IdempotencyKey: "platform_fee:" + p.ID,
			OccurredOn:     at,
			Memo:           feeMemo(q),
		})
	if err != nil {
		return err
	}
	posted, err := f.ledger.Post(ctx, entry)
	if err != nil {
		return fmt.Errorf("money: posting the platform fee for %s: %w", p.ID, err)
	}
	if !posted.Duplicate {
		f.log.Info("platform fee posted", "payment", p.ID, "fee", q.Fee.Net,
			"tax", q.Fee.Tax, "retained", q.Retained(), "rule", q.Fee.RuleID)
	}
	return nil
}

// feeMemo says on the posting how the fee was collected, so an accrual is
// visible on the statement rather than inferred from a missing settlement leg.
func feeMemo(q Quote) string {
	if q.Retained() {
		return "platform fee, retained at capture"
	}
	return "platform fee, accrued (" + string(q.Accrual) + ")"
}
