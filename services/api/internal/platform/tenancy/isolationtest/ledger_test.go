package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0006. The ledger is the subsystem that decides whether this product is
// trustworthy, so it gets the fullest contract in the harness: immutability,
// double entry at commit, derived balances, and what a management firm can see
// of an owner's money.
func TestLedger(t *testing.T) {
	p := pool(t)
	isolationtest.RunLedger(t, p, platformPool(t))
}

// And the ADR-0003 five-part contract for each ledger table, because the
// delegated branch of each policy widens what a session can see and the base
// property has to be re-asserted rather than assumed to have survived.
//
// Both inserts write a whole balanced entry: a single posting cannot be written
// at all, which is the point of the deferred balance trigger and is why this is
// not the one-INSERT shape every other table in the harness uses.
func TestLedgerIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	// No property on these postings: organisations A and B have no tree, and an
	// organisation-level posting is a real shape — a GST remittance is one.
	insertEntry := func(kind string) func(context.Context, pgx.Tx, tenancy.ID, string) error {
		return func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			var id string
			if err := tx.QueryRow(ctx, `
				INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind,
				                             source_id, idempotency_key, memo)
				VALUES ($1, $2, current_date, 'harness', $3, $3, $3) RETURNING id`,
				tenant.String(), kind, token).Scan(&id); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO ledger_postings (entry_id, tenant_id, account_code, side,
				                             amount_minor, party_kind, party_id, memo) VALUES
				  ($1, $2, 'gst_output', 'debit',  100000, 'statutory', $3, $4),
				  ($1, $2, 'bank',       'credit', 100000, 'platform',  $5, $4)`,
				id, tenant.String(), "00000000-0000-0000-0000-000000000101", token,
				"00000000-0000-0000-0000-0000000000d8")
			return err
		}
	}

	isolationtest.Run(t, p, isolationtest.Table{
		Name:   "journal_entries",
		Insert: insertEntry("gst_remittance"),
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `SELECT count(*) FROM journal_entries WHERE memo = $1`, token).Scan(&n)
			return n, err
		},
	})

	isolationtest.Run(t, p, isolationtest.Table{
		Name:   "ledger_postings",
		Insert: insertEntry("gst_remittance"),
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			// One entry, two postings: count the entries this run's postings
			// belong to, so the arithmetic in the harness stays "one row".
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(DISTINCT entry_id) FROM ledger_postings WHERE memo = $1`, token).Scan(&n)
			return n, err
		},
	})
}
