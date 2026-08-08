package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Taking things back off the book (#356). Nothing here deletes: a building's
// leases, its ledger and its documents still have to be readable years after
// the manager stops letting it. The database refuses to retire anything
// somebody is still living in, and that refusal arrives as ErrNotAllowed so the
// manager reads why rather than a 500.

// RetireProperty takes a building off the book.
func (s *Properties) RetireProperty(ctx context.Context, id, reason string) error {
	return s.retire(ctx, `
		UPDATE properties SET state = 'inactive', retired_reason = nullif($2, ''), updated_at = now()
		 WHERE id = $1::uuid AND state = 'active'`, id, reason, ErrNoProperty, "building")
}

// RetireUnit takes one home out of the building's list.
func (s *Properties) RetireUnit(ctx context.Context, id, reason string) error {
	return s.retire(ctx, `
		UPDATE units SET state = 'inactive', retired_reason = nullif($2, ''), updated_at = now()
		 WHERE id = $1::uuid AND state = 'active'`, id, reason, ErrNoUnit, "home")
}

// RetireBed takes a bed off the board.
func (s *Properties) RetireBed(ctx context.Context, id, reason string) error {
	return s.retire(ctx, `
		UPDATE beds SET state = 'retired', retired_reason = nullif($2, ''), updated_at = now()
		 WHERE id = $1::uuid AND state <> 'retired'`, id, reason, ErrNoBed, "bed")
}

func (s *Properties) retire(ctx context.Context, sql, id, reason string, missing error, what string) error {
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, id, reason)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return missing
		}
		return nil
	})
	switch {
	case err == nil:
		return nil
	case errors.Is(err, missing):
		return err
	case isCheck(err):
		// The guard's own words: "a home somebody is living in cannot be retired".
		return fmt.Errorf("%w: %s", ErrNotAllowed, checkMessage(err))
	}
	return fmt.Errorf("property: retiring %s %s: %w", what, id, err)
}
