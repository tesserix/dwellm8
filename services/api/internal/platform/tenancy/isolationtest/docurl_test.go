package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// dwellm8#212. Both docUrl tables are lease evidence reached from an
// unauthenticated fetch — the grant's organisation opens the window, so the
// window had better be real. Isolation is what keeps one landlord's signing
// history out of another's evidence pack.

func TestDocURLRevocationsIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "esign_docurl_revocations",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO esign_docurl_revocations (tenant_id, txn_id, reason)
				VALUES ($1, $2, 'completed')`,
				tenant.String(), token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `
				SELECT count(*) FROM esign_docurl_revocations WHERE txn_id LIKE $1 || '-%'`,
				token).Scan(&n)
			return n, err
		},
	})
}

func TestDocURLAccessLogIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "esign_docurl_access_log",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO esign_docurl_access_log
				       (tenant_id, txn_id, document_ref, source_ip, user_agent, outcome)
				VALUES ($1, $2, 'lease/doc-1', '203.0.113.7', 'isolation-test', 'served')`,
				tenant.String(), token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `
				SELECT count(*) FROM esign_docurl_access_log WHERE txn_id = $1`,
				token).Scan(&n)
			return n, err
		},
	})
}
