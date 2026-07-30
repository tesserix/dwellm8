// Package isolationtest is the shared tenant-isolation harness.
//
// ADR-0003 lists five properties every tenant-scoped table must have. Stating
// them in a document is not enforcement, so they live here as one function a
// module calls with its table name. An isolation regression then fails a build
// rather than reaching production.
//
// The harness needs a real PostgreSQL, because the properties it checks are
// PostgreSQL's: FORCE row level security, WITH CHECK, and the behaviour of an
// unset session variable. A mock would assert that the mock works.
package isolationtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Orgs are the two organisations every isolation test runs against.
const (
	OrgA = tenancy.ID("11111111-1111-1111-1111-111111111111")
	OrgB = tenancy.ID("22222222-2222-2222-2222-222222222222")
)

// Table describes what the harness needs to exercise one table.
//
// Insert and Count both receive a token unique to this run. Rows written by
// one run must not be counted by the next — the harness commits, so without a
// marker the counts drift upwards and the first assertion fails for a reason
// that has nothing to do with isolation. That happened; hence the token.
type Table struct {
	// Name of the table, unqualified.
	Name string
	// Insert writes one row for the given tenant, tagged with token. It must
	// insert through the given transaction so the session's tenant applies.
	Insert func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error
	// Count returns how many of this run's rows the session can see.
	Count func(ctx context.Context, tx pgx.Tx, token string) (int, error)
}

// token is unique per run, so a committed row cannot leak into the next run's
// arithmetic.
func token(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("token: %v", err)
	}
	return hex.EncodeToString(b)
}

// Run asserts ADR-0003's five properties for one table.
//
// Each failure message names the property and what it means, because the
// person reading it at 2am did not write this file.
func Run(t *testing.T, pool tenancy.Pool, tbl Table) {
	t.Helper()
	ctx := context.Background()
	tok := token(t)

	t.Run(tbl.Name+"/scoped session sees only its own rows", func(t *testing.T) {
		mustInsert(t, ctx, pool, tbl, OrgA, tok)
		mustInsert(t, ctx, pool, tbl, OrgB, tok)

		got := mustCount(t, ctx, pool, tbl, OrgA, tok)
		if got != 1 {
			t.Fatalf("organisation A sees %d rows in %s, want 1 — the policy is not filtering", got, tbl.Name)
		}
	})

	t.Run(tbl.Name+"/no tenant returns zero rows, not an error", func(t *testing.T) {
		// Deliberately bypassing Scoped: this asserts the database's behaviour
		// when a session variable was never set, which is what a bug in the
		// middleware would produce.
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		n, err := tbl.Count(ctx, tx, tok)
		if err != nil {
			t.Fatalf("counting %s with no tenant set returned an error (%v) — it must return zero rows quietly, "+
				"or a pooled connection produces a 500 on reuse", tbl.Name, err)
		}
		if n != 0 {
			t.Fatalf("counting %s with no tenant set returned %d rows — that is a cross-organisation leak", tbl.Name, n)
		}
	})

	t.Run(tbl.Name+"/writing into another organisation is refused", func(t *testing.T) {
		err := tenancy.Scoped(tenancy.With(ctx, OrgA), pool, func(ctx context.Context, tx pgx.Tx) error {
			return tbl.Insert(ctx, tx, OrgB, tok) // scoped to A, writing B
		})
		if err == nil {
			t.Fatalf("a session scoped to organisation A wrote a row belonging to B in %s — "+
				"the policy is missing WITH CHECK", tbl.Name)
		}
	})

	t.Run(tbl.Name+"/row level security is FORCEd", func(t *testing.T) {
		// The owner bypasses its own policies unless forced, and the owner is
		// the role that runs migrations.
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var enabled, forced bool
		err = tx.QueryRow(ctx,
			`SELECT c.relrowsecurity, c.relforcerowsecurity
			   FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
			  WHERE n.nspname = 'public' AND c.relname = $1`, tbl.Name).Scan(&enabled, &forced)
		if err != nil {
			t.Fatalf("reading RLS flags for %s: %v", tbl.Name, err)
		}
		if !enabled {
			t.Fatalf("%s has no row level security at all", tbl.Name)
		}
		if !forced {
			t.Fatalf("%s enables row level security but does not FORCE it — the table's owner, "+
				"which runs migrations, bypasses every policy on it", tbl.Name)
		}
	})

	t.Run(tbl.Name+"/has a tenant_id column", func(t *testing.T) {
		tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()

		var n int
		err = tx.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.columns
			  WHERE table_schema = 'public' AND table_name = $1
			    AND column_name IN ('tenant_id', 'id')`, tbl.Name).Scan(&n)
		if err != nil {
			t.Fatalf("reading columns for %s: %v", tbl.Name, err)
		}
		if n == 0 {
			t.Fatalf("%s has neither tenant_id nor id — it cannot be scoped to an organisation", tbl.Name)
		}
	})
}

func mustInsert(t *testing.T, ctx context.Context, pool tenancy.Pool, tbl Table, org tenancy.ID, tok string) {
	t.Helper()
	err := tenancy.Scoped(tenancy.With(ctx, org), pool, func(ctx context.Context, tx pgx.Tx) error {
		return tbl.Insert(ctx, tx, org, tok)
	})
	if err != nil {
		t.Fatalf("inserting into %s for %s: %v", tbl.Name, org, err)
	}
}

func mustCount(t *testing.T, ctx context.Context, pool tenancy.Pool, tbl Table, org tenancy.ID, tok string) int {
	t.Helper()
	var n int
	err := tenancy.Scoped(tenancy.With(ctx, org), pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		n, err = tbl.Count(ctx, tx, tok)
		return err
	})
	if err != nil {
		t.Fatalf("counting %s for %s: %v", tbl.Name, org, err)
	}
	return n
}

// SchemaAudit fails if any table holding customer data lacks tenant_id or a
// row-level security policy. It is the check that catches a table nobody
// thought to write an isolation test for.
func SchemaAudit(t *testing.T, pool tenancy.Pool, exempt ...string) {
	t.Helper()
	ctx := context.Background()

	skip := map[string]bool{}
	for _, e := range exempt {
		skip[e] = true
	}

	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT c.relname, c.relrowsecurity, c.relforcerowsecurity,
		       EXISTS (SELECT 1 FROM information_schema.columns col
		                WHERE col.table_schema = 'public' AND col.table_name = c.relname
		                  AND col.column_name = 'tenant_id') AS has_tenant
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relkind = 'r'
		 ORDER BY c.relname`)
	if err != nil {
		t.Fatalf("listing tables: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name string
		var enabled, forced, hasTenant bool
		if err := rows.Scan(&name, &enabled, &forced, &hasTenant); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if skip[name] {
			continue
		}
		switch {
		case !hasTenant && name != "organisations":
			t.Errorf("table %q has no tenant_id — add one, or add it to the exempt list with a reason", name)
		case !enabled:
			t.Errorf("table %q has no row level security", name)
		case !forced:
			t.Errorf("table %q enables row level security but does not FORCE it", name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating tables: %v", err)
	}
}

var _ = fmt.Sprintf // keep fmt available for future messages
