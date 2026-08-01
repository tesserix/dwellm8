package authz

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The combined contract of dwellm8#154: the graph and row-level security are
// independent refusals, proven against a real OpenFGA server (CI runs one)
// and the real schema — removing either layer must leave the other denying.

func combined(t *testing.T) (*Client, tenancy.Pool, tenancy.PlatformPool) {
	t.Helper()
	fga := os.Getenv("TEST_AUTHZ_URL")
	dsn, plat := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if fga == "" || dsn == "" || plat == "" {
		t.Skip("TEST_AUTHZ_URL, TEST_DATABASE_URL and TEST_PLATFORM_DATABASE_URL are not all set")
	}
	c := NewClient(fga)
	if err := c.Bootstrap(context.Background(), "combined-harness"); err != nil {
		t.Fatalf("bootstrapping against a real OpenFGA: %v", err)
	}
	req, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(req.Close)
	p, err := pgxpool.New(context.Background(), plat)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return c, req, tenancy.NewPlatformPool(p)
}

// Removing row-level security from the picture leaves the graph denying: a
// principal with no tuple gets deny for an object whose rows their session
// could perfectly well reach.
func TestWithoutATupleTheGraphDeniesWhateverRLSWouldAllow(t *testing.T) {
	c, _, _ := combined(t)
	ctx := context.Background()

	allowed, err := c.Check(ctx, "user:insider", "can_view", "agreement:l-combined")
	if err != nil || allowed {
		t.Fatalf("no tuple must be deny, got allowed=%v err=%v", allowed, err)
	}

	if err := c.WriteTuples(ctx, []Tuple{
		{User: "user:insider", Relation: "tenant", Object: "agreement:l-combined"}}, nil); err != nil {
		t.Fatalf("writing the tuple: %v", err)
	}
	allowed, err = c.Check(ctx, "user:insider", "can_view", "agreement:l-combined")
	if err != nil || !allowed {
		t.Fatalf("the tuple must allow, got allowed=%v err=%v", allowed, err)
	}
}

// Removing the graph from the picture leaves RLS denying: a tuple that says
// "mallory may view org A's account" moves nothing in PostgreSQL, because SQL
// never consults tuples — a session scoped to org B reads zero of org A's
// rows however generous the graph became.
func TestATupleMovesNothingInPostgreSQL(t *testing.T) {
	c, pool, plat := combined(t)
	ctx := context.Background()
	isolationtest.SeedPropertyTree(t, plat)

	owner := "aaaaaaaa-1111-2222-3333-444444444444"
	err := tenancy.Platform(ctx, plat, "test: seeding an org A payout account",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO payout_accounts (tenant_id, owner_party_id, masked_account, ifsc, account_fp)
				VALUES ($1, $2, 'XX9999', 'HDFC0009999', 'fp-combined')
				ON CONFLICT DO NOTHING`, isolationtest.OrgOwner.String(), owner)
			return err
		})
	if err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// The graph is as wrong as it can be: mallory holds a tuple onto the org A
	// object, written moments ago.
	if err := c.WriteTuples(ctx, []Tuple{
		{User: "user:mallory", Relation: "owner", Object: "organisation:" + isolationtest.OrgOwner.String()}}, nil); err != nil {
		t.Fatalf("writing the hostile tuple: %v", err)
	}

	var visible int
	err = tenancy.Scoped(tenancy.With(ctx, isolationtest.OrgB), pool,
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*) FROM payout_accounts WHERE owner_party_id = $1`, owner).Scan(&visible)
		})
	if err != nil {
		t.Fatalf("reading as org B: %v", err)
	}
	if visible != 0 {
		t.Fatalf("a tuple widened SQL's reach: org B sees %d of org A's payout accounts", visible)
	}
}

// The conditions evaluate on a real server too: an expired mandate is deny
// with the same tuples that allowed an hour earlier. The fake in client_test
// cannot prove this — only a real evaluator runs CEL.
func TestConditionsEvaluateOnTheRealServer(t *testing.T) {
	c, _, _ := combined(t)
	ctx := context.Background()

	writeConditioned := func(object string, until string) error {
		body := map[string]any{
			"authorization_model_id": c.modelID,
			"writes": map[string]any{"tuple_keys": []map[string]any{{
				"user": "organisation:firm-combined", "relation": "managed_by", "object": object,
				"condition": map[string]any{"name": "mandate_active", "context": map[string]string{
					"valid_from": "2026-01-01T00:00:00Z", "valid_until": until}},
			}}},
		}
		var out struct{}
		return c.do(ctx, "POST", c.BaseURL+"/stores/"+c.storeID+"/write", body, &out)
	}
	if err := writeConditioned("property:p-live", "2999-01-01T00:00:00Z"); err != nil {
		t.Fatalf("writing the live mandate: %v", err)
	}
	if err := writeConditioned("property:p-lapsed", "2026-06-01T00:00:00Z"); err != nil {
		t.Fatalf("writing the lapsed mandate: %v", err)
	}
	if err := c.WriteTuples(ctx, []Tuple{
		{User: "user:kiran-combined", Relation: "manager", Object: "organisation:firm-combined"}}, nil); err != nil {
		t.Fatal(err)
	}

	for object, want := range map[string]bool{"property:p-live": true, "property:p-lapsed": false} {
		allowed, err := c.Check(ctx, "user:kiran-combined", "can_manage", object)
		if err != nil || allowed != want {
			t.Errorf("%s: allowed=%v err=%v, want %v", object, allowed, err, want)
		}
	}
}
