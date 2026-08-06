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

// Settlements is where one collection's division is written (#270). The payout
// run, the owner statement and the reconciliation all read this table rather
// than recomputing the split, so a rate that changes next April cannot restate
// what settled last March.
type Settlements struct{ pool tenancy.Pool }

func NewSettlements(pool tenancy.Pool) *Settlements { return &Settlements{pool: pool} }

// ErrNoInstruction is a payment nothing has divided yet.
var ErrNoInstruction = errors.New("no settlement instruction for that payment")

// SettlementState is where the owner's leg has got to.
type SettlementState string

const (
	SettlementPending    SettlementState = "pending"
	SettlementInstructed SettlementState = "instructed"
	SettlementSettled    SettlementState = "settled"
	SettlementFailed     SettlementState = "failed"
)

// Instruction is a division on the way in.
type Instruction struct {
	PaymentID  string
	LeaseID    string
	Currency   string
	Split      domain.Split
	Provider   string
	ExpectedOn time.Time
}

// Settlement is a division as stored.
type Settlement struct {
	ID          string
	PaymentID   string
	LeaseID     string
	Currency    string
	Split       domain.Split
	State       SettlementState
	Provider    string
	TransferRef string
	ExpectedOn  time.Time
	SettledOn   time.Time
	Reason      string
}

const settlementColumns = `id, payment_id, coalesce(lease_id::text, ''), currency,
	gross_minor, platform_minor, management_minor, tds_minor, owner_minor,
	coalesce(fee_rule_id, ''), state, coalesce(provider, ''), coalesce(transfer_ref, ''),
	expected_on, settled_on, coalesce(reason, '')`

func scanSettlement(row pgx.Row) (Settlement, error) {
	var s Settlement
	var expected, settled *time.Time
	err := row.Scan(&s.ID, &s.PaymentID, &s.LeaseID, &s.Currency,
		&s.Split.Gross, &s.Split.Platform, &s.Split.Management, &s.Split.TDS, &s.Split.Owner,
		&s.Split.RuleID, &s.State, &s.Provider, &s.TransferRef, &expected, &settled, &s.Reason)
	if err != nil {
		return Settlement{}, err
	}
	if expected != nil {
		s.ExpectedOn = *expected
	}
	if settled != nil {
		s.SettledOn = *settled
	}
	return s, nil
}

// Record divides a collection. Capture is retried and the division must not be,
// so a second call on the same payment returns the first division unchanged.
func (s *Settlements) Record(ctx context.Context, in Instruction) (Settlement, error) {
	if err := in.Split.Validate(); err != nil {
		return Settlement{}, err
	}
	var out Settlement
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tenant, _ := tenancy.From(ctx)
		row := tx.QueryRow(ctx, `
			INSERT INTO settlement_instructions (
				tenant_id, payment_id, lease_id, currency, gross_minor, platform_minor,
				management_minor, tds_minor, owner_minor, fee_rule_id, provider, expected_on)
			VALUES ($1, $2, nullif($3, '')::uuid, $4, $5, $6, $7, $8, $9,
				nullif($10, ''), nullif($11, ''), $12)
			ON CONFLICT (payment_id) DO UPDATE SET updated_at = now()
			RETURNING `+settlementColumns,
			tenant.String(), in.PaymentID, in.LeaseID, currencyOr(in.Currency),
			in.Split.Gross, in.Split.Platform, in.Split.Management, in.Split.TDS, in.Split.Owner,
			in.Split.RuleID, in.Provider, dateOrNil(in.ExpectedOn))
		var err error
		out, err = scanSettlement(row)
		return err
	})
	if err != nil {
		return Settlement{}, fmt.Errorf("record the division of %s: %w", in.PaymentID, err)
	}
	return out, nil
}

// ForPayment reads one division.
func (s *Settlements) ForPayment(ctx context.Context, paymentID string) (Settlement, error) {
	var out Settlement
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+settlementColumns+`
			  FROM settlement_instructions WHERE payment_id = $1`, paymentID)
		var err error
		out, err = scanSettlement(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoInstruction
		}
		return err
	})
	if err != nil {
		return Settlement{}, err
	}
	return out, nil
}

// ByID reads one division.
func (s *Settlements) ByID(ctx context.Context, id string) (Settlement, error) {
	var out Settlement
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `SELECT `+settlementColumns+`
			  FROM settlement_instructions WHERE id = $1`, id)
		var err error
		out, err = scanSettlement(row)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoInstruction
		}
		return err
	})
	if err != nil {
		return Settlement{}, err
	}
	return out, nil
}

// Due is the payout run's queue: everything owed and not yet settled by the
// date the schedule promised. Past that date it is the manager's exception list.
func (s *Settlements) Due(ctx context.Context, on time.Time) ([]Settlement, error) {
	var out []Settlement
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+settlementColumns+`
			  FROM settlement_instructions
			 WHERE state IN ('pending', 'instructed')
			   AND (expected_on IS NULL OR expected_on <= $1)
			 ORDER BY expected_on NULLS FIRST, created_at`, on)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			one, err := scanSettlement(rows)
			if err != nil {
				return err
			}
			out = append(out, one)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("list settlements due: %w", err)
	}
	return out, nil
}

// Instructed records that a transfer was sent to the provider.
func (s *Settlements) Instructed(ctx context.Context, id, transferRef string) error {
	return s.move(ctx, id, `state = 'instructed', transfer_ref = $2, reason = NULL`, transferRef)
}

// Settled records the provider's report agreeing the money landed.
func (s *Settlements) Settled(ctx context.Context, id, reference string, on time.Time) error {
	return s.move(ctx, id,
		`state = 'settled', transfer_ref = $2, settled_on = $3, reason = NULL`, reference, on)
}

// Failed records a payout the provider would not make, in its own words. The
// manager's exception list is this state.
func (s *Settlements) Failed(ctx context.Context, id, reason string) error {
	return s.move(ctx, id, `state = 'failed', reason = nullif($2, '')`, reason)
}

func (s *Settlements) move(ctx context.Context, id, set string, args ...any) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`UPDATE settlement_instructions SET `+set+`, updated_at = now() WHERE id = $1`,
			append([]any{id}, args...)...)
		if err != nil {
			return fmt.Errorf("move settlement %s: %w", id, err)
		}
		if tag.RowsAffected() == 0 {
			return ErrNoInstruction
		}
		return nil
	})
}

func currencyOr(c string) string {
	if c == "" {
		return "INR"
	}
	return c
}

func dateOrNil(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
