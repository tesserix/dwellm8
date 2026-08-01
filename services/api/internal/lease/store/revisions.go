package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Effective-dated rent revision — issue #36, ADR-0008's worked example made an
// API. A revision is two rows: the old rent closed at the effective date, the
// new one opened from it. An as-of query for any earlier day still answers
// with the old figure, which is what makes March's invoice defensible in
// April.

// ErrBackdated is a revision effective before the rent currently in force
// began. That is a correction (retire-and-replace with `corrects`), a
// different act with a different audit trail, and this call refuses to
// impersonate it.
var ErrBackdated = errors.New("lease: a revision cannot begin before the current rent did — a correction is a different act")

// ErrRevisionExists is a second revision from the same day.
var ErrRevisionExists = errors.New("lease: the rent already changes on that date")

// Revision is what a rent change takes.
type Revision struct {
	AmountMinor int64
	DueDay      int
	From        effective.Date
}

// ReviseRent closes the rent in force at the effective date and opens the new
// one, in one transaction with the fact on the outbox. The exclusion
// constraint (rent_schedule_no_overlap) stands behind everything this method
// checks.
func (s *Leases) ReviseRent(ctx context.Context, leaseID string, r Revision) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}

	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state string
		err := tx.QueryRow(ctx,
			`SELECT state FROM leases WHERE id = $1 FOR UPDATE`, leaseID).Scan(&state)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoLease
		}
		if err != nil {
			return err
		}
		switch state {
		case "draft", "pending_signature", "active", "in_notice":
		default:
			return fmt.Errorf("%w: a %s tenancy's rent is history, not something to revise",
				ErrNoLease, state)
		}

		// The rent in force on the effective date, locked.
		var (
			currentID   string
			currentFrom time.Time
			currentTo   *time.Time
			oldAmount   int64
		)
		err = tx.QueryRow(ctx, `
			SELECT id::text, valid_from, valid_to, amount_minor
			  FROM rent_schedule
			 WHERE lease_id = $1 AND retired_at IS NULL AND validity @> $2::date
			 FOR UPDATE`, leaseID, r.From.Time()).Scan(&currentID, &currentFrom, &currentTo, &oldAmount)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: no rent in force on %s", ErrNoLease, r.From)
		}
		if err != nil {
			return err
		}
		cf := effective.Day(currentFrom.Year(), currentFrom.Month(), currentFrom.Day())
		if !cf.Time().Before(r.From.Time()) {
			if cf == r.From {
				return ErrRevisionExists
			}
			return ErrBackdated
		}

		// Close the old, open the new — the closed row keeps the old row's own
		// end date, so a fixed-term schedule stays fixed-term.
		if _, err := tx.Exec(ctx, `
			UPDATE rent_schedule SET valid_to = $2
			 WHERE id = $1`, currentID, r.From.Time()); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, due_day, valid_from, valid_to)
			VALUES ($1, $2, $3, $4, $5::date, $6)`,
			tenant.String(), leaseID, r.AmountMinor, r.DueDay, r.From.Time(), currentTo); err != nil {
			return err
		}

		env, err := events.New("lease.rent.revised", tenant.String(),
			events.Subject{Kind: "lease", ID: leaseID},
			events.Actor{Kind: events.ActorSystem},
			struct {
				FromMinor int64  `json:"from_amount_minor"`
				ToMinor   int64  `json:"to_amount_minor"`
				Effective string `json:"effective_on"`
			}{oldAmount, r.AmountMinor, r.From.String()})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}
