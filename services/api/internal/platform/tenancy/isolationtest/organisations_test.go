package isolationtest_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The harness proves itself against the two tables that exist today. Every
// module adds its own file like this one when it lands a table.
//
// Skipped without TEST_DATABASE_URL, because the properties under test are
// PostgreSQL's own and a mock would only assert that the mock works. CI sets
// it; a laptop run without a database still passes the rest of the suite.
func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the isolation harness")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	if err := p.Ping(context.Background()); err != nil {
		t.Fatalf("pinging: %v", err)
	}
	return p
}

// platformPool connects as dwellm8_platform, the role the policies exempt.
// Falls back to TEST_DATABASE_URL so a single-DSN setup still runs the
// non-seeding assertions.
func platformPool(t *testing.T) tenancy.PlatformPool {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set — skipping the seeded isolation harness")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting as the platform role: %v", err)
	}
	t.Cleanup(p.Close)
	return tenancy.NewPlatformPool(p)
}

func seedOrganisations(t *testing.T, p tenancy.PlatformPool) {
	t.Helper()
	ctx := context.Background()
	// Creating an organisation cannot be tenant-scoped: the policy compares
	// against a row that does not exist yet. ADR-0003 §3.
	err := tenancy.Platform(ctx, p, "seeding the isolation harness", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO organisations (id, slug, name, kind) VALUES
			  ($1, 'harness-a', 'Harness A', 'agency'),
			  ($2, 'harness-b', 'Harness B', 'agency')
			ON CONFLICT (id) DO NOTHING`, isolationtest.OrgA, isolationtest.OrgB)
		return err
	})
	if err != nil {
		t.Fatalf("seeding organisations: %v", err)
	}
}

func TestAuditEventsIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "audit_events",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO audit_events (tenant_id, actor_kind, module, action, subject_kind, subject_id)
				VALUES ($1, 'system', 'identity', 'harness.wrote', 'test', $2)`,
				tenant.String(), token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM audit_events WHERE action = 'harness.wrote' AND subject_id = $1`,
				token).Scan(&n)
			return n, err
		},
	})
}

func TestSchemaAudit(t *testing.T) {
	// Catches the table nobody thought to write an isolation test for.
	//
	// The first three exemptions are the chart of accounts and the posting
	// templates (ADR-0006 §2). They hold no customer data and are deliberately not
	// tenant-scoped: a landlord with a private idea of what "deposit_liability"
	// means is a landlord whose statements cannot be compared or audited. The
	// application cannot write them either — INSERT, UPDATE and DELETE are
	// revoked from dwellm8_app, so the schema file is their only author.
	//
	// The last two are ADR-0012's, and they are exempt for the opposite reason:
	// they hold nothing but money data and it belongs to no organisation. A
	// settlement batch is one payout from one aggregator account, spanning every
	// organisation that collected that day; a reconciliation run is a statement
	// about a provider-day. Neither has a tenant_id because neither has a tenant,
	// and both carry row-level security that admits only a platform session —
	// asserted from both sides in settlement_test.go, and required of every table
	// of this shape by the schema's assertion 12.
	//
	// This guard is why that pair had to be argued for rather than added: it fired
	// on both tables the moment they existed.
	// And ADR-0019's two, which are exempt for a third reason again: they hold data about
	// somebody who is not a customer of anybody. A prospect is browsing the whole site,
	// and attributing them to the first owner whose listing they opened would be wrong —
	// and would leak their interest in the others to that owner. So a prospect belongs to
	// no organisation, their shortlist likewise, and assertion 12 requires both to be
	// platform-write-only, which they are.
	//
	// An enquiry is the first row in that funnel that does belong to somebody, and it has
	// an ordinary tenant_id — which is the line this exemption stops at.
	// saved_searches (#144) and prospect_push_tokens (#126) are the shortlist's
	// shape exactly: a prospect's own criteria and reachability, attributable
	// to no organisation, platform-write-only.
	//
	// ADR-0023's two are the first three's shape exactly: a statutory rate is the
	// rule rather than the data, a tenant does not get a private idea of what the
	// TDS rate is, and the application cannot write one — assertion 18 fails the
	// bootstrap if the privilege ever comes back.
	//
	// identity_principals is exempt for the reason ADR-0027 §6 gives, and it is a
	// fifth shape: a person exists before they belong to anybody. Somebody signing
	// into Find is nobody's tenant, and a tenant_id would have to be invented at
	// the moment of sign-in — before the membership that would decide it.
	//
	// So row-level security has nothing to scope it by and the privilege is the
	// boundary instead: it is revoked from dwellm8_app at the foot of the schema,
	// after the blanket grant, and only dwellm8_identity holds it. Without that
	// revoke the table would be a list of every verified phone number on the
	// platform, readable by every module.
	// ADR-0031's is ADR-0023's shape again, and the exemption is the same
	// sentence: the platform fee is the rule rather than the data. There is one
	// price for the platform, a tenant does not get a private idea of what
	// Dwellm8 charges, and an organisation that could write this row would price
	// itself at zero effective last April. Assertion 18 now covers it too.
	p := pool(t)
	isolationtest.SchemaAudit(t, p,
		"ledger_accounts", "posting_templates", "posting_template_lines",
		"settlement_batches", "reconciliation_runs",
		"prospects", "prospect_shortlist", "saved_searches", "prospect_push_tokens",
		"statutory_rules", "statutory_rule_slabs",
		"platform_fee_rules",
		"identity_principals")
}
