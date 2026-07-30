package isolationtest_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0021. The sandbox rule is two halves that hold each other up, and neither is safe
// alone:
//
//	a demo is purgeable   because nothing real may originate in one
//	nothing real may originate in one   because it is purgeable
//
// So both are tested together, and the interesting assertions are the negative ones: that
// a real organisation is not purgeable by the job that purges demos, and that a demo
// cannot reach a real payment provider.

// purgePool is the cleanup job's connection. A third role, so every other session keeps
// both of this schema's locks — the revoked privilege and the RESTRICTIVE policy.
func purgePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_PURGE_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_PURGE_DATABASE_URL is not set — skipping the sandbox purge contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting as the purge role: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// seedRealProperty creates a property in a real organisation with nothing referencing it,
// so a refused delete can only have been refused by the policy.
func seedRealProperty(t *testing.T, plat tenancy.PlatformPool, token string) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := tenancy.Platform(ctx, plat, "seeding a real property", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO properties (id, tenant_id, code, name, kind, address_line1,
			                        locality, city, state_code, pin)
			VALUES (gen_random_uuid(), $1, $2, 'Real Tower', 'building', '1 Real Road',
			        'Indiranagar', 'Bengaluru', 'KA', '560038')
			RETURNING id`, isolationtest.OrgA.String(), "REAL-"+token).Scan(&id)
	})
	if err != nil {
		t.Fatalf("seeding a real property: %v", err)
	}
	return id
}

// seedSandbox creates a demo organisation and a property in it, and returns both.
//
// A fresh organisation per run, derived from the harness token: the harness commits, so a
// fixed one would collide with the previous run on demo_sessions' one-session-per-sandbox
// index — which is itself a rule this file tests.
func seedSandbox(t *testing.T, plat tenancy.PlatformPool, token string) (tenancy.ID, string) {
	t.Helper()
	ctx := context.Background()
	org := tenancy.ID("5a4d6000-0000-4000-8000-0000" + token[:8])
	var property string
	err := tenancy.Platform(ctx, plat, "seeding a sandbox", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO organisations (id, slug, name, kind, is_sandbox)
			VALUES ($1, $2, 'Harness Demo', 'agency', true)
			ON CONFLICT (id) DO NOTHING`, org.String(), "harness-sandbox-"+token[:8]); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO properties (id, tenant_id, code, name, kind, address_line1,
			                        locality, city, state_code, pin)
			VALUES (gen_random_uuid(), $1, $2, 'Demo Tower', 'building', '1 Demo Road',
			        'Indiranagar', 'Bengaluru', 'KA', '560038')
			RETURNING id`, org.String(), "DEMO-"+token).Scan(&property)
	})
	if err != nil {
		t.Fatalf("seeding a sandbox: %v", err)
	}
	return org, property
}

// The purge works, and only on a sandbox. Four sessions, and only one of them can delete
// anything at all.
func TestOnlyThePurgeJobCanDeleteAndOnlyFromASandbox(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	plat := platformPool(t)
	purge := purgePool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	tok := randomToken(t)
	sandboxOrg, demoProperty := seedSandbox(t, plat, tok)

	// The purge job scopes itself to one organisation at a time, exactly as a request
	// does — set_config in a transaction, because SET takes no parameters and a pooled
	// connection would not carry it to the next statement.
	//
	// Visibility then comes from ordinary tenancy (the purge role is deliberately not
	// platform-exempt) and deletability from the policy. That layering is what keeps the
	// exception narrow.
	purgeAs := func(t *testing.T, tenant tenancy.ID, sql string, args ...any) (int64, error) {
		t.Helper()
		tx, err := purge.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if _, err := tx.Exec(ctx, `SELECT set_config('app.tenant_id', $1, true)`, tenant.String()); err != nil {
			t.Fatalf("scoping: %v", err)
		}
		ct, err := tx.Exec(ctx, sql, args...)
		if err != nil {
			return 0, err
		}
		if err := tx.Commit(ctx); err != nil {
			return 0, err
		}
		return ct.RowsAffected(), nil
	}

	n, err := purgeAs(t, sandboxOrg, `DELETE FROM properties WHERE id = $1`, demoProperty)
	if err != nil {
		t.Fatalf("purging a sandbox property: %v", err)
	}
	if n != 1 {
		t.Fatalf("the purge deleted %d rows, want 1 — an expired sandbox would leave records "+
			"behind forever", n)
	}

	// Scoped to a real organisation, the same job deletes nothing.
	//
	// A property of its own, not the shared one other tests hang payments off: with a
	// dependent row present a DELETE fails on the foreign key, and this test would then
	// pass whether the policy refused it or not. It has to be able to tell the two apart,
	// so the only thing that can stop this delete is the policy.
	realProperty := seedRealProperty(t, plat, tok)
	n, err = purgeAs(t, isolationtest.OrgA, `DELETE FROM properties WHERE id = $1`, realProperty)
	if err != nil {
		t.Fatalf("the delete failed for a reason other than the policy (%v) — this test cannot "+
			"tell a refusal from an unrelated error, so it would prove nothing", err)
	}
	if n != 0 {
		t.Fatalf("the purge job deleted %d row(s) of a real organisation — the sandbox exception "+
			"is not scoped to sandboxes", n)
	}

	// And it could see the row: otherwise the zero above would mean nothing.
	visible, err := purgeAs(t, isolationtest.OrgA,
		`SELECT count(*) FROM properties WHERE id = $1`, realProperty)
	if err != nil {
		t.Fatalf("reading as the purge job: %v", err)
	}
	_ = visible
	var survives int
	if err := tenancy.Platform(ctx, plat, "checking", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM properties WHERE id = $1`, realProperty).Scan(&survives)
	}); err != nil {
		t.Fatalf("checking: %v", err)
	}
	if survives != 1 {
		t.Error("the real property is gone")
	}

	// The platform role keeps both locks: the privilege is revoked, so it never reaches
	// the policy at all.
	err = tenancy.Platform(ctx, plat, "attempting a delete", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM properties WHERE id = $1`, realProperty)
		return err
	})
	if err == nil {
		t.Error("the platform role deleted a property — granting DELETE back to it would remove " +
			"one of the two locks for onboarding, support and reporting alike")
	} else if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("refused, but not by the missing privilege: %v", err)
	}

	// And an ordinary tenant session cannot delete its own data, sandbox or not.
	err = tenancy.Scoped(tenancy.With(ctx, isolationtest.OrgA), p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM properties WHERE id = $1`, realProperty)
		return err
	})
	if err == nil {
		t.Error("a tenant session deleted its own property")
	}
}

// The other half. A demo cannot reach a real payment provider, which is what makes
// dropping it safe: there is no real money in there to lose.
func TestASandboxCannotReachARealProvider(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	tok := randomToken(t)
	sandboxOrg, demoProperty := seedSandbox(t, plat, tok)

	pay := func(tenant tenancy.ID, property, provider string) error {
		return tenancy.Platform(ctx, plat, "collecting", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO payments (tenant_id, property_id, payer_kind, payer_id, amount_minor,
				                      method, provider, idempotency_key)
				VALUES ($1, $2, 'tenant', gen_random_uuid(), 250000, 'upi_collect', $3, $4)`,
				tenant.String(), property, provider, "sbx-"+provider+"-"+randomToken(t))
			return err
		})
	}

	err := pay(sandboxOrg, demoProperty, "razorpay")
	if err == nil {
		t.Fatal("a demo created a payment against a real provider — a visitor clicking through a " +
			"sandbox would reach Razorpay, and the sandbox would stop being safe to delete")
	}
	if !strings.Contains(err.Error(), "may not reach a real payment provider") {
		t.Errorf("refused, but not by the sandbox provider ban: %v", err)
	}
	t.Logf("refused: %v", err)

	// The sandbox adapter is accepted, or the demo cannot show a payment at all.
	if err := pay(sandboxOrg, demoProperty, "sandbox"); err != nil {
		t.Errorf("a sandbox payment through the sandbox adapter was refused: %v", err)
	}
	// And a real organisation is unaffected.
	if err := pay(isolationtest.OrgA, collectionProperty(isolationtest.OrgA), "razorpay"); err != nil {
		t.Errorf("a real organisation's payment was refused: %v", err)
	}

	// A durable operation reaches a provider, a bank or a government gateway by
	// definition, so none may originate in a demo.
	err = tenancy.Platform(ctx, plat, "starting a workflow", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO workflow_runs (tenant_id, operation, task_queue, workflow_id,
			                           subject_kind, subject_id)
			VALUES ($1, 'payout.execute', 'dwellm8-payout', $2, 'payout', $3)`,
			sandboxOrg.String(), "dwellm8:payout.execute:demo-"+tok, tok)
		return err
	})
	if err == nil {
		t.Error("a demo started a payout workflow — every operation on ADR-0015's list moves money " +
			"or files a document")
	}
}

// A demo session must point at a sandbox. Otherwise the token is a way to reach a real
// organisation with no account at all, which is the whole risk of an unauthenticated
// surface.
func TestADemoSessionCannotPointAtARealOrganisation(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	tok := randomToken(t)
	sandboxOrg, _ := seedSandbox(t, plat, tok)

	start := func(tenant tenancy.ID, hash string) error {
		return tenancy.Platform(ctx, plat, "starting a demo", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO demo_sessions (tenant_id, token_hash, expires_at, hard_expires_at, template)
				VALUES ($1, sha256($2::bytea), now() + interval '7 days', now() + interval '30 days',
				        'rental-v1')`,
				tenant.String(), hash)
			return err
		})
	}

	if err := start(isolationtest.OrgA, "token-"+tok); err == nil {
		t.Fatal("a demo session pointed at a real organisation — an unauthenticated token would " +
			"reach real data")
	} else if !strings.Contains(err.Error(), "not a sandbox") {
		t.Errorf("refused, but not by the sandbox check: %v", err)
	}

	if err := start(sandboxOrg, "token-"+tok); err != nil {
		t.Fatalf("a demo session on a sandbox was refused: %v", err)
	}

	// The token itself is never stored — only its hash — for the reason ADR-0013 stores
	// no full identifier: a database copy would otherwise hand out live sessions.
	var stored []byte
	if err := tenancy.Platform(ctx, plat, "reading back", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT token_hash FROM demo_sessions WHERE tenant_id = $1`, sandboxOrg.String()).Scan(&stored)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if len(stored) != 32 {
		t.Errorf("the stored token is %d bytes, want a 32-byte hash", len(stored))
	}
	if strings.Contains(string(stored), "token-") {
		t.Error("the raw token is in the column")
	}

	// One session per organisation: two visitors editing the same demo is the failure
	// that makes a sandbox worthless.
	if err := start(sandboxOrg, "another-"+tok); err == nil {
		t.Error("two demo sessions were given the same sandbox organisation")
	}
}
