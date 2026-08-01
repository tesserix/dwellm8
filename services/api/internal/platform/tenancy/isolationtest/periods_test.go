package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// dwellm8#190. A close is one organisation's lock on its own books — another
// organisation must neither see it nor, worse, be locked by it.
//
// The month is 2019 so no other test's close state can collide: closes are
// per-tenant, and OrgA/OrgB post no ledger entries anywhere.

func TestLedgerPeriodClosesIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	months := map[tenancy.ID]string{
		isolationtest.OrgA: "2019-01-01",
		isolationtest.OrgB: "2019-02-01",
	}
	isolationtest.Run(t, p, isolationtest.Table{
		Name: "ledger_period_closes",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO ledger_period_closes (tenant_id, period_month, action, actor, reason)
				VALUES ($1, $2::date, 'reopened', $3, 'isolation test reopen')`,
				tenant.String(), months[tenant], token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `
				SELECT count(*) FROM ledger_period_closes WHERE actor = $1`, token).Scan(&n)
			return n, err
		},
	})
}
