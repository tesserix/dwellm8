package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The morning tiles count the whole roster. Reading it as a page of leases in
// identifier order made the numbers a firm's first 500 tenancies rather than
// its tenancies (#306).
func TestRosterCountsEveryLiveTenancyAndTheRentInForce(t *testing.T) {
	req, plat := pools(t)
	leases := store.New(req)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	on := effective.Day(2026, 6, 1)

	before, err := leases.Roster(ctx, on)
	if err != nil {
		t.Fatalf("reading the roster: %v", err)
	}

	seedLiveTenancy(t, plat, "2026-01-01", "2026-12-31", 31_000_00)
	seedLiveTenancy(t, plat, "2026-11-01", "2027-10-31", 44_000_00)

	after, err := leases.Roster(ctx, on)
	if err != nil {
		t.Fatalf("reading the roster: %v", err)
	}

	if got, want := after.Active, before.Active+1; got != want {
		t.Errorf("%d tenancies in force, want %d", got, want)
	}
	if got, want := after.Starting, before.Starting+1; got != want {
		t.Errorf("%d tenancies not yet begun, want %d", got, want)
	}
	if got, want := after.RentRollMinor, before.RentRollMinor+31_000_00; got != want {
		t.Errorf("rent roll %d, want %d — a tenancy that starts in November is not November's rent in June", got, want)
	}

	// The count is over the table, not over a page of it.
	var live int
	if err := tenancy.Platform(context.Background(), plat, "counting the roster",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*) FROM leases
				 WHERE tenant_id = $1 AND state IN ('active', 'in_notice')`,
				isolationtest.OrgOwner.String()).Scan(&live)
		}); err != nil {
		t.Fatalf("counting leases: %v", err)
	}
	if got := after.Active + after.Starting; got != live {
		t.Errorf("the roster reports %d live tenancies, the table holds %d", got, live)
	}
}

// seedLiveTenancy puts an active tenancy on a unit of its own, with a rent
// schedule in force for its whole term.
func seedLiveTenancy(t *testing.T, plat tenancy.PlatformPool, from, to string, rentMinor int64) {
	t.Helper()
	_, unitID := unit(t, plat, "RST-"+token())
	err := tenancy.Platform(context.Background(), plat, "seeding a roster tenancy",
		func(ctx context.Context, tx pgx.Tx) error {
			var lease string
			if err := tx.QueryRow(ctx, `
				INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES (gen_random_uuid(), $1, $2, $3, 'active', $4::date, $5::date)
				RETURNING id`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unitID, from, to,
			).Scan(&lease); err != nil {
				return err
			}
			if err := isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgOwner.String(), lease, from); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO rent_schedule (id, tenant_id, lease_id, amount_minor, due_day, valid_from, valid_to)
				VALUES (gen_random_uuid(), $1, $2, $3, 5, $4::date, $5::date)`,
				isolationtest.OrgOwner.String(), lease, rentMinor, from, to)
			return err
		})
	if err != nil {
		t.Fatalf("seeding a tenancy: %v", err)
	}
}
