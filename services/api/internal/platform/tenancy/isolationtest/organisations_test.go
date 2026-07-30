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
	p := pool(t)
	isolationtest.SchemaAudit(t, p)
}
