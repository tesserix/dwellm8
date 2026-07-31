// Package store writes and reads leases, their parties, their rent schedule and
// the tax facts a tenancy cannot start without.
//
// One transaction per lease. A lease with no tenant on it, or a tenancy that
// went live with no TDS section, is not a state this table may be left in for
// even a moment — and a caller that wrote them in four calls would leave exactly
// that state on the fourth failure.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Leases is the lease module's store.
type Leases struct{ pool tenancy.Pool }

// New takes the request pool: a lease is always somebody's, so never the
// platform pool.
func New(p tenancy.Pool) *Leases { return &Leases{pool: p} }

// ErrDoubleLet is a lease that would let a flat already let over the same days.
// Distinguishable because the caller's answer is to show the conflicting
// tenancy, not to retry.
var ErrDoubleLet = errors.New("lease: the unit is already let over those days")

// ErrNoLease is no such lease — including one hidden by policy, which is the
// same answer to a caller and deliberately so.
var ErrNoLease = errors.New("lease: no such lease")

// Created is what Create wrote.
type Created struct {
	ID    string
	Lease domain.Lease
	Terms domain.Terms
	Event domain.Event
}

// Create writes the lease, its parties, its opening rent schedule and its tax
// facts in one transaction.
//
// The lease is written in draft. Activation is a separate call, because that is
// where the no-double-let constraint and the tax gate bite and where an event
// with consequences is published.
func (s *Leases) Create(ctx context.Context, d domain.Draft) (Created, error) {
	l, terms, ev, err := d.Create()
	if err != nil {
		return Created{}, err
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return Created{}, tenancy.ErrNoTenant
	}

	var id string
	err = tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to,
			                    notice_days, lock_in_until, renews_lease_id)
			VALUES ($1, $2, $3, 'draft', $4::date, $5, $6, $7, $8)
			RETURNING id`,
			tenant.String(), d.Property, d.Unit,
			d.Term.From().Time(), nullDate(d.Term.To()),
			d.NoticeDays, nullDate(d.LockInUntil), nullText(d.Renews))
		if err := row.Scan(&id); err != nil {
			return fmt.Errorf("writing the lease: %w", err)
		}

		for _, p := range d.Parties {
			if _, err := tx.Exec(ctx, `
				INSERT INTO lease_parties (tenant_id, lease_id, party_id, role, valid_from)
				VALUES ($1, $2, $3, $4, $5::date)`,
				tenant.String(), id, p.PartyID, string(p.Role), d.Term.From().Time()); err != nil {
				return fmt.Errorf("writing %s as %s: %w", p.Name, p.Role, err)
			}
		}

		// The opening rent. A revision later is a new row that closes this one
		// (ADR-0008), never an update — which is why this is an insert of a
		// schedule rather than a column on the lease.
		if _, err := tx.Exec(ctx, `
			INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, due_day, valid_from, valid_to)
			VALUES ($1, $2, $3, $4, $5::date, $6)`,
			tenant.String(), id, terms.RentMinor, int(terms.DueDay),
			d.Term.From().Time(), nullDate(d.Term.To())); err != nil {
			return fmt.Errorf("writing the rent schedule: %w", err)
		}

		return writeTaxFacts(ctx, tx, tenant.String(), id, d.Tax)
	})
	if err != nil {
		return Created{}, err
	}

	l.ID = id
	return Created{ID: id, Lease: l, Terms: terms, Event: ev}, nil
}

// writeTaxFacts writes each set of facts as its own effective-dated row.
func writeTaxFacts(ctx context.Context, tx pgx.Tx, tenant, lease string, h tds.History) error {
	for _, rec := range h.Timeline.Live() {
		f := rec.Value
		if _, err := tx.Exec(ctx, `
			INSERT INTO lease_tax_facts (tenant_id, lease_id, deductor_class, landlord_residency,
			                             source, acknowledged_on, acknowledged_by,
			                             valid_from, valid_to)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9)`,
			tenant, lease, string(f.Deductor), string(f.Residency), f.Source,
			nullDate(f.AcknowledgedOn), nullText(f.AcknowledgedBy),
			rec.Range.From().Time(), nullDate(rec.Range.To())); err != nil {
			return fmt.Errorf("writing the tax facts effective %s: %w", f.From, err)
		}
	}
	return nil
}

// Activate moves a lease to active, and is where the two guards the database
// owns actually fire: the no-double-let exclusion and the deferred tax-path
// trigger.
//
// Both are refusals the caller must be able to tell apart — one names another
// tenancy, the other names a missing form — so the constraint violations are
// mapped rather than passed up as an opaque 23P01.
func (s *Leases) Activate(ctx context.Context, id string, by domain.Actor) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}

	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'active'
			 WHERE id = $1 AND tenant_id = $2 AND state = 'pending_signature'`,
			id, tenant.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoLease
		}
		return nil
	})

	switch {
	case err == nil:
		return nil
	case isExclusionViolation(err, "leases_no_double_let"):
		conflict, lookupErr := s.conflicting(ctx, id)
		if lookupErr != nil || conflict == "" {
			return fmt.Errorf("%w: %v", ErrDoubleLet, err)
		}
		return fmt.Errorf("%w: lease %s already lets that unit over those days",
			ErrDoubleLet, conflict)
	default:
		return err
	}
}

// conflicting finds the tenancy that blocked an activation, so the refusal can
// name it rather than describe it.
func (s *Leases) conflicting(ctx context.Context, id string) (string, error) {
	var other string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT other.id
			  FROM leases mine
			  JOIN leases other
			    ON other.unit_id = mine.unit_id
			   AND other.id <> mine.id
			   AND other.validity && mine.validity
			   AND other.state IN ('active', 'in_notice', 'renewed', 'terminated', 'settled')
			 WHERE mine.id = $1
			 LIMIT 1`, id).Scan(&other)
	})
	if err != nil {
		return "", err
	}
	return other, nil
}

// Terms reads the rent in force for a lease on a date.
func (s *Leases) Terms(ctx context.Context, id string, on effective.Date) (domain.Terms, error) {
	var (
		amount int64
		dueDay int
	)
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT amount_minor, due_day
			  FROM rent_schedule
			 WHERE lease_id = $1 AND retired_at IS NULL AND validity @> $2::date`,
			id, on.Time()).Scan(&amount, &dueDay)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Terms{}, fmt.Errorf("%w: no rent in force on %s", ErrNoLease, on)
	}
	if err != nil {
		return domain.Terms{}, err
	}
	return domain.Terms{
		RentMinor: amount, DueDay: domain.DueDay(dueDay), Cycle: domain.Monthly,
	}, nil
}

func nullDate(d effective.Date) any {
	if d.Zero() {
		return nil
	}
	return d.Time()
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isExclusionViolation(err error, constraint string) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	// 23P01 is exclusion_violation. The name is checked as well as the code
	// because a schema with two exclusion constraints on one table would
	// otherwise report the wrong one.
	return pg.Code == "23P01" && strings.Contains(pg.ConstraintName, constraint)
}
