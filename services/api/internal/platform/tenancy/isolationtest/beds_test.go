package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Beds in a hostel or a PG (#299). A bed is what is let, so it answers to the
// same isolation contract as the unit it sits in.

func TestBedsIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "beds",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			unit, err := leaseUnit(ctx, tx, tenant, token)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO beds (tenant_id, property_id, unit_id, label, rent_amount_minor)
				SELECT $1, u.property_id, u.id, $2, 1200000 FROM units u WHERE u.id = $3::uuid`,
				tenant.String(), "BED-"+token, unit)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM beds WHERE label = $1`, "BED-"+token).Scan(&n)
			return n, err
		},
	})
}

func TestARenterSessionSeesNoBed(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	property, unit := seedLeaseUnit(t, plat)

	ctx := tenancy.With(context.Background(), isolationtest.OrgA)
	if err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO beds (tenant_id, property_id, unit_id, label, rent_amount_minor)
			VALUES ($1, $2, $3, 'renter-deny-fixture', 900000)`,
			isolationtest.OrgA.String(), property, unit)
		return err
	}); err != nil {
		t.Fatalf("seeding a bed: %v", err)
	}

	renter := tenancy.WithResident(ctx, tenancy.ResidentID("d1111111-0000-0000-0000-000000000001"))
	var n int
	if err := tenancy.Scoped(renter, p, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM beds`).Scan(&n)
	}); err != nil {
		t.Fatalf("counting beds as a renter: %v", err)
	}
	if n != 0 {
		t.Errorf("a renter session sees %d beds — that is every other occupant of the hostel", n)
	}
}

// An occupied bed with nobody billed for it is rent that never appears.
func TestABedCannotBeOccupiedWithoutALease(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	property, unit := seedLeaseUnit(t, plat)

	ctx := tenancy.With(context.Background(), isolationtest.OrgA)
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO beds (tenant_id, property_id, unit_id, label, rent_amount_minor, state)
			VALUES ($1, $2, $3, 'no-lease-fixture', 900000, 'occupied')`,
			isolationtest.OrgA.String(), property, unit)
		return err
	})
	if err == nil {
		t.Fatal("a bed was marked occupied with no lease behind it — nobody would be billed for it")
	}
}
