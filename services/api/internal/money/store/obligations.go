package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/tdsfiling"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Obligations tracks TDS deductions and the evidence that each step was done.
// ADR-0024 and ADR-0025; the deadlines themselves are tdsfiling's.
type Obligations struct{ pool tenancy.Pool }

// NewObligations takes the request pool. A deduction is always somebody's.
func NewObligations(p tenancy.Pool) *Obligations { return &Obligations{pool: p} }

// ErrNoObligation is no such deduction — including one hidden by policy.
var ErrNoObligation = errors.New("money: no such TDS obligation")

// Record writes an obligation and its steps in one transaction.
//
// Idempotent on (lease, section, period start): a retried run of the same period
// returns what is already there rather than raising a second deduction for the
// same rent, which would be visible only as a landlord being paid twice short.
func (o *Obligations) Record(ctx context.Context, ob tdsfiling.Obligation, rateBps int, rateRuleID string) (string, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}

	var id string
	err := tenancy.Scoped(ctx, o.pool, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			INSERT INTO tds_obligations (tenant_id, lease_id, section, period_from, period_to,
			                             paid_on, amount_minor, rate_bps, rate_rule_id)
			VALUES ($1, $2, $3, $4::date, $5::date, $6::date, $7, $8, $9)
			ON CONFLICT (tenant_id, lease_id, section, period_from) DO NOTHING
			RETURNING id`,
			tenant.String(), ob.LeaseID, string(ob.Section),
			ob.Period.From().Time(), ob.Period.To().Time(), ob.PaidOn.Time(),
			ob.AmountMinor, rateBps, nullUUID(rateRuleID)).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			// Already recorded. Return the existing one rather than a second
			// deduction against the same rent.
			return tx.QueryRow(ctx, `
				SELECT id FROM tds_obligations
				 WHERE tenant_id = $1 AND lease_id = $2 AND section = $3 AND period_from = $4::date`,
				tenant.String(), ob.LeaseID, string(ob.Section), ob.Period.From().Time()).Scan(&id)
		}
		if err != nil {
			return fmt.Errorf("recording the obligation: %w", err)
		}

		for _, step := range ob.Schedule {
			if _, err := tx.Exec(ctx, `
				INSERT INTO tds_obligation_steps (obligation_id, tenant_id, step, due_by, artefact)
				VALUES ($1, $2, $3, $4::date, $5)
				ON CONFLICT (obligation_id, step) DO NOTHING`,
				id, tenant.String(), string(step.Step), step.By.Time(), string(step.Artefact)); err != nil {
				return fmt.Errorf("recording the %s step: %w", step.Step, err)
			}
		}
		return nil
	})
	return id, err
}

// Evidence closes a step with the reference that proves it.
//
// The schema refuses a reference with no date and a date with no reference, so
// the half-recorded state this would otherwise produce cannot exist: a challan
// number with no date is not a deposit, and treating it as one is how an
// unfiled return looks filed.
func (o *Obligations) Evidence(ctx context.Context, obligationID string, step tdsfiling.Step, reference string, on effective.Date) error {
	if reference == "" || on.Zero() {
		return fmt.Errorf("%w: %s needs both a reference and a date", tdsfiling.ErrObligation, step)
	}
	return tenancy.Scoped(ctx, o.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE tds_obligation_steps
			   SET reference = $3, done_on = $4::date
			 WHERE obligation_id = $1 AND step = $2`,
			obligationID, string(step), reference, on.Time())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: no %s step on %s", ErrNoObligation, step, obligationID)
		}
		return nil
	})
}

// nullUUID passes an empty id as NULL rather than as a string PostgreSQL cannot
// cast to uuid.
func nullUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Outstanding is one step somebody still owes.
type Outstanding struct {
	ObligationID string
	LeaseID      string
	Section      tds.Section
	Step         tdsfiling.Step
	Artefact     tds.Artefact
	DueBy        effective.Date
	AmountMinor  int64
	// DaysLate is negative while the deadline is still ahead, which is what
	// makes one query serve both the reminder and the escalation.
	DaysLate int
}

// Due returns every outstanding step due on or before a horizon, soonest first.
//
// One query for the reminder and the escalation, because they differ only in how
// late the item is — and two queries would drift the day somebody changed one.
func (o *Obligations) Due(ctx context.Context, by effective.Date) ([]Outstanding, error) {
	var out []Outstanding
	err := tenancy.Scoped(ctx, o.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT s.obligation_id, o.lease_id, o.section, s.step, s.artefact, s.due_by,
			       o.amount_minor, ($1::date - s.due_by) AS days_late
			  FROM tds_obligation_steps s
			  JOIN tds_obligations o ON o.id = s.obligation_id
			 WHERE s.reference IS NULL
			   AND s.due_by <= $1::date
			 ORDER BY s.due_by, o.lease_id`, by.Time())
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				r                       Outstanding
				section, step, artefact string
				dueBy                   time.Time
			)
			if err := rows.Scan(&r.ObligationID, &r.LeaseID, &section, &step, &artefact,
				&dueBy, &r.AmountMinor, &r.DaysLate); err != nil {
				return err
			}
			r.Section, r.Step, r.Artefact = tds.Section(section), tdsfiling.Step(step), tds.Artefact(artefact)
			r.DueBy = effective.Day(dueBy.Year(), dueBy.Month(), dueBy.Day())
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}
