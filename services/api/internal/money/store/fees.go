package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Fees resolves what Dwellm8 charges, as of a date. ADR-0031.
type Fees struct{ pool tenancy.Pool }

// NewFees takes the request pool. The rule is reference data and readable by
// every role, but a fee is always charged inside somebody's transaction.
func NewFees(p tenancy.Pool) *Fees { return &Fees{pool: p} }

// ErrNoRule is a day with no price. Distinguishable because it is a
// configuration failure rather than a free collection: a fee that silently
// becomes zero is revenue lost without a trace.
var ErrNoRule = errors.New("money: no platform fee rule is in force")

// Schedule reads the rule in force on a date.
//
// At most one row can match — the exclusion constraint on the table says so — so
// this needs no ordering and no limit that could hide a second live rule.
func (f *Fees) Schedule(ctx context.Context, on time.Time) (domain.FeeSchedule, error) {
	var (
		s        domain.FeeSchedule
		rate     int
		taxRate  int
		minMinor int64
		maxMinor *int64
	)
	err := tenancy.Scoped(ctx, f.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id::text, rate_bps, tax_rate_bps, tax_inclusive, min_fee_minor, max_fee_minor
			  FROM platform_fee_rules
			 WHERE retired_at IS NULL AND validity @> $1::date`, on,
		).Scan(&s.RuleID, &rate, &taxRate, &s.TaxInclusive, &minMinor, &maxMinor)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.FeeSchedule{}, fmt.Errorf("%w on %s", ErrNoRule, on.Format(time.DateOnly))
	}
	if err != nil {
		return domain.FeeSchedule{}, fmt.Errorf("money: reading the fee rule: %w", err)
	}

	s.Rate, s.TaxRate, s.MinMinor = domain.Rate(rate), domain.Rate(taxRate), domain.Minor(minMinor)
	if maxMinor != nil {
		s.MaxMinor = domain.Minor(*maxMinor)
	}
	return s, nil
}
