package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Leaving: a firm putting an owner down, and a firm closing itself (#356).
// Neither erases anything. What a firm was permitted to do, and when, is the
// owner's record of who held their keys, so a mandate is revoked rather than
// deleted and an organisation is closed rather than removed.

// ErrNotAllowed is the schema refusing to leave somebody stranded — a firm
// still holding a mandate, or still letting a flat, may not close.
var ErrNotAllowed = errors.New("identity: that cannot be closed yet")

// ResignMandate ends the firm's own mandate over an owner. Scoped to the
// grantee on purpose: a firm may put down what it holds and nothing else.
func (s *Principals) ResignMandate(ctx context.Context, firmOrgID, grantID string) error {
	return tenancy.Platform(ctx, s.platform, "resigning a mandate",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE delegation_grants SET revoked_at = now()
				 WHERE id = $1::uuid AND grantee_org_id = $2::uuid AND revoked_at IS NULL`,
				grantID, firmOrgID)
			if err != nil {
				return fmt.Errorf("resigning mandate %s: %w", grantID, err)
			}
			if tag.RowsAffected() == 0 {
				return ErrNoMandate
			}
			return nil
		})
}

// CloseFirm closes the manager's own organisation. The schema decides whether
// it may: a firm with a live tenancy or a standing mandate is refused, because
// closing it would leave a tenant paying rent to nobody.
func (s *Principals) CloseFirm(ctx context.Context, firmOrgID, reason string) error {
	err := tenancy.Platform(ctx, s.platform, "closing a firm",
		func(ctx context.Context, tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `
				UPDATE organisations SET state = 'closed', closed_reason = nullif($2, '')
				 WHERE id = $1::uuid AND state <> 'closed'`, firmOrgID, reason)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return ErrNoOrganisation
			}
			return nil
		})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23514" {
		// The guard's own words, so the manager reads why rather than a code.
		return fmt.Errorf("%w: %s", ErrNotAllowed, pg.Message)
	}
	return err
}
