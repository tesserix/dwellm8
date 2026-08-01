package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0033. The run log is the answer to "why did this tenant get a message on a
// Sunday", and the settings are what one organisation decided about its own
// tenants. Neither is anybody else's business, and the approval queue is the one
// that would be actively dangerous: a firm that could see another's pending
// approvals could see what they are about to waive and for whom.

func TestAutomationSettingsIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "automation_settings",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			// The automation key is unique per organisation, so the token goes in
			// it: two runs of the harness must not collide on the same key.
			_, err := tx.Exec(ctx, `
				INSERT INTO automation_settings (tenant_id, automation, enabled, params)
				VALUES ($1, $2, false, '{}'::jsonb)`,
				tenant.String(), "harness_"+token[:8]+"_"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM automation_settings WHERE automation LIKE 'harness\_' || $1 || '\_%'`,
				token[:8]).Scan(&n)
			return n, err
		},
	})
}

func TestAutomationRunsIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "automation_runs",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO automation_runs (tenant_id, automation, subject_kind, subject_id,
				                             outcome, action, idempotency_key)
				VALUES ($1, 'arrears_ladder', 'lease', gen_random_uuid(), 'acted', $2, $3)`,
				tenant.String(), token, token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM automation_runs WHERE action = $1`, token).Scan(&n)
			return n, err
		},
	})
}

func TestAutomationApprovalsIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "automation_approvals",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO automation_approvals (tenant_id, automation, subject_kind, subject_id,
				                                  action, amount_minor, ceiling_minor, idempotency_key)
				VALUES ($1, 'arrears_ladder', 'lease', gen_random_uuid(), $2, 50000, 10000, $3)`,
				tenant.String(), token, token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM automation_approvals WHERE action = $1`, token).Scan(&n)
			return n, err
		},
	})
}

// A renter has no business in an operations automation at all. ADR-0029's deny,
// written in the chapter rather than left to the generator that runs before it.
func TestARenterSessionSeesNoAutomation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	ctx := tenancy.With(context.Background(), isolationtest.OrgA)
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO automation_runs (tenant_id, automation, subject_kind, subject_id,
			                             outcome, action, idempotency_key)
			VALUES ($1, 'arrears_ladder', 'lease', gen_random_uuid(), 'acted',
			        'renter deny fixture', 'renter-deny-' || gen_random_uuid()::text)`,
			isolationtest.OrgA.String())
		return err
	})
	if err != nil {
		t.Fatalf("seeding a run: %v", err)
	}

	renter := tenancy.WithResident(ctx, tenancy.ResidentID("d1111111-0000-0000-0000-000000000001"))
	for _, table := range []string{"automation_settings", "automation_runs", "automation_approvals"} {
		var n int
		err := tenancy.Scoped(renter, p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		})
		if err != nil {
			t.Fatalf("counting %s as a renter: %v", table, err)
		}
		if n != 0 {
			t.Errorf("a renter session sees %d rows in %s — including what their landlord is "+
				"about to waive and for whom", n, table)
		}
	}
}
