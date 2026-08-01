package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// dwellm8#196. A note is a colleague's aside about a tenancy — exactly the
// text that must never cross a landlord boundary.

func TestActivityNotesIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "activity_notes",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO activity_notes (tenant_id, subject_kind, subject_id, author, body, visibility)
				VALUES ($1, 'lease', $2, 'isolation-test', 'the tap drips', 'org')`,
				tenant.String(), token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `
				SELECT count(*) FROM activity_notes WHERE subject_id = $1`, token).Scan(&n)
			return n, err
		},
	})
}
