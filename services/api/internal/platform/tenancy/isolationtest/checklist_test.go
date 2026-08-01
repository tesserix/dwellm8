package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0032. A checklist says what a firm did in somebody's flat and what it did not,
// which is the record a deposit dispute turns on. Four tables, and the interesting
// one is checklist_templates: it is the first table in the schema that deliberately
// shows one organisation rows belonging to another — the platform library — so the
// question is whether the hole is exactly that shape.

func TestChecklistTemplatesIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "checklist_templates",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO checklist_templates (tenant_id, process, name, version, published_at)
				VALUES ($1, 'move_out', $2, (SELECT coalesce(max(version), 0) + 1 FROM checklist_templates
				          WHERE tenant_id = $1 AND process = 'move_out'), now())`,
				tenant.String(), token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM checklist_templates WHERE name LIKE $1 || '-%'`, token).Scan(&n)
			return n, err
		},
	})
}

func TestChecklistTemplateStepsIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "checklist_template_steps",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			var template string
			if err := tx.QueryRow(ctx, `
				INSERT INTO checklist_templates (tenant_id, process, name, version, published_at)
				VALUES ($1, 'move_in', $2, (SELECT coalesce(max(version), 0) + 1 FROM checklist_templates
				          WHERE tenant_id = $1 AND process = 'move_in'), now())
				RETURNING id::text`,
				tenant.String(), "steps-"+token+"-"+string(tenant)[:1]).Scan(&template); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO checklist_template_steps
				    (tenant_id, template_id, code, title, position, owner_role, depends_on)
				VALUES ($1, $2, 'keys', $3, 1, 'manager', '{}')`,
				tenant.String(), template, token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM checklist_template_steps WHERE title LIKE $1 || '-%'`, token).Scan(&n)
			return n, err
		},
	})
}

func TestChecklistsIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "checklists",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			template, property, err := checklistFixtures(ctx, tx, tenant, token)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO checklists (tenant_id, process, template_id, template_version,
				                        property_id, anchor_on, abandoned_reason)
				VALUES ($1, 'owner_onboarding', $2, 1, $3, current_date, $4)`,
				tenant.String(), template, property, token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM checklists WHERE abandoned_reason LIKE $1 || '-%'`, token).Scan(&n)
			return n, err
		},
	})
}

func TestChecklistTasksIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "checklist_tasks",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			template, property, err := checklistFixtures(ctx, tx, tenant, token)
			if err != nil {
				return err
			}
			var checklist string
			if err := tx.QueryRow(ctx, `
				INSERT INTO checklists (tenant_id, process, template_id, template_version,
				                        property_id, anchor_on)
				VALUES ($1, 'owner_onboarding', $2, 1, $3, current_date)
				RETURNING id::text`, tenant.String(), template, property).Scan(&checklist); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO checklist_tasks (tenant_id, checklist_id, step_code, title, position,
				                             owner_role, due_on, depends_on)
				VALUES ($1, $2, 'keys', $3, 1, 'manager', current_date, '{}')`,
				tenant.String(), checklist, token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM checklist_tasks WHERE title LIKE $1 || '-%'`, token).Scan(&n)
			return n, err
		},
	})
}

// checklistFixtures writes the template and finds the property a checklist needs.
// Owner onboarding names no unit, which is deliberate: it is the shape that would
// pass NULL to is_delegated_unit(), and assertion 6 is about exactly that.
func checklistFixtures(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) (template, property string, err error) {
	if err = tx.QueryRow(ctx, `
		INSERT INTO checklist_templates (tenant_id, process, name, version, published_at)
		VALUES ($1, 'owner_onboarding', $2, (SELECT coalesce(max(version), 0) + 1 FROM checklist_templates
		          WHERE tenant_id = $1 AND process = 'owner_onboarding'), now())
		RETURNING id::text`,
		tenant.String(), "fixture-"+token+"-"+string(tenant)[:1]).Scan(&template); err != nil {
		return "", "", err
	}
	// Whichever property this organisation has. The harness's two organisations both
	// own one; a checklist must name one, because a row that names no property cannot
	// be judged at unit granularity.
	if err = tx.QueryRow(ctx, `
		INSERT INTO properties (tenant_id, code, name, kind, address_line1, locality, city, state_code, pin)
		VALUES ($1, $2, 'Checklist Fixture', 'building', '1 Road', 'GK', 'Delhi', 'DL', '110048')
		RETURNING id::text`,
		tenant.String(), "CHK-"+token[:6]+string(tenant)[:1]).Scan(&property); err != nil {
		return "", "", err
	}
	return template, property, nil
}

// The renter deny, ADR-0029. A checklist is operations work: a tenant's move-out
// steps reach them as notifications, never as rows they can read or tick.
func TestARenterSessionSeesNoChecklist(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	ctx := tenancy.With(context.Background(), isolationtest.OrgA)
	var wrote string
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO checklist_templates (tenant_id, process, name, version, published_at)
			VALUES ($1, 'move_out', 'renter deny fixture',
			        (SELECT coalesce(max(version), 0) + 1 FROM checklist_templates
			          WHERE tenant_id = $1 AND process = 'move_out'), now())
			RETURNING id::text`, isolationtest.OrgA.String()).Scan(&wrote)
	})
	if err != nil {
		t.Fatalf("seeding a template: %v", err)
	}

	renter := tenancy.WithResident(ctx, tenancy.ResidentID("d1111111-0000-0000-0000-000000000001"))
	for _, table := range []string{
		"checklist_templates", "checklist_template_steps", "checklists", "checklist_tasks",
	} {
		var n int
		err := tenancy.Scoped(renter, p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
		})
		if err != nil {
			t.Fatalf("counting %s as a renter: %v", table, err)
		}
		if n != 0 {
			t.Errorf("a renter session sees %d rows in %s — that is every other tenant of the "+
				"same landlord, which looks exactly like the product working", n, table)
		}
	}
}

// The platform library is readable by any organisation and writable by none of them.
// This is the one deliberate hole in ADR-0003's shape, so it is asserted rather than
// assumed: an organisation that could publish a default would put a step into every
// other organisation's resolution order.
func TestTheDefaultLibraryIsReadableAndNotWritable(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	var library int
	err := tenancy.Scoped(tenancy.With(context.Background(), isolationtest.OrgA), p,
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM checklist_templates WHERE is_default`).Scan(&library)
		})
	if err != nil {
		t.Fatalf("reading the library: %v", err)
	}
	if library == 0 {
		t.Fatal("an organisation cannot see the default templates, so one that has configured " +
			"nothing has nothing to fire")
	}

	err = tenancy.Scoped(tenancy.With(context.Background(), isolationtest.OrgA), p,
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO checklist_templates (tenant_id, is_default, process, name, version, published_at)
				VALUES ($1, true, 'move_out', 'a default of my own', 99, now())`,
				isolationtest.OrgA.String())
			return err
		})
	if err == nil {
		t.Error("an organisation published a default template — every other organisation would " +
			"then resolve it")
	}

	// And an unscoped session is an anonymous visitor, which the library is not for.
	tx, err := p.BeginTx(context.Background(), pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var unscoped int
	if err := tx.QueryRow(context.Background(),
		`SELECT count(*) FROM checklist_templates`).Scan(&unscoped); err != nil {
		t.Fatalf("counting with no tenant set: %v", err)
	}
	if unscoped != 0 {
		t.Errorf("a session with no organisation sees %d templates — the default library is not "+
			"a thing the internet gets to enumerate", unscoped)
	}
}
