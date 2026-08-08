package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// Taking a building, a home or a bed back off the book (#356). A manager who
// onboarded the wrong tower had no way to undo it, and the one thing retirement
// must never do is strand a tenant who is still living there.

const (
	retireOrg      = "55555555-5555-5555-5555-555555555555"
	retireProperty = "b5555555-0000-0000-0000-000000000001"
	retireEmpty    = "b5555555-0000-0000-0000-000000000002"
	retireUnitLet  = "c5555555-0000-0000-0000-000000000001"
	retireUnitFree = "c5555555-0000-0000-0000-000000000002"
	retireUnitBeds = "c5555555-0000-0000-0000-000000000003"
)

// seedRetirementTree writes a tenant of its own rather than borrowing the
// delegation fixtures: these tests retire what they seed, and the shared tree
// is counted by tests that expect all of it to still be there.
func seedRetirementTree(t *testing.T, plat tenancy.PlatformPool) (bedLet, bedFree string) {
	t.Helper()
	err := tenancy.Platform(context.Background(), plat, "seeding a tree to retire",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO organisations (id, slug, name, kind)
				VALUES ($1, 'harness-retire', 'Harness Retirement', 'agency')
				ON CONFLICT (id) DO NOTHING`, retireOrg); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO properties (id, tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin) VALUES
				  ($1, $3, 'RETIRE-1', 'Retire Towers', 'building',
				   '1 Retire Road', 'Kadavanthra', 'Kochi', 'KL', '682020'),
				  ($2, $3, 'RETIRE-2', 'Retire Annexe', 'building',
				   '2 Retire Road', 'Kadavanthra', 'Kochi', 'KL', '682020')
				ON CONFLICT (id) DO NOTHING`, retireProperty, retireEmpty, retireOrg); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft) VALUES
				  ($1, $4, $5, 'flat', 'R-101', 1, 600.00),
				  ($2, $4, $5, 'flat', 'R-102', 1, 600.00),
				  ($3, $4, $6, 'room', 'R-201', 2, 300.00)
				ON CONFLICT (id) DO NOTHING`,
				retireUnitLet, retireUnitFree, retireUnitBeds, retireOrg, retireProperty, retireEmpty); err != nil {
				return err
			}

			var lease string
			if err := tx.QueryRow(ctx, `
				INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
				SELECT $1, $2, $3, 'active', '2026-01-01'::date, '2027-01-01'::date
				 WHERE NOT EXISTS (SELECT 1 FROM leases WHERE unit_id = $3::uuid)
				RETURNING id`, retireOrg, retireProperty, retireUnitLet).Scan(&lease); err != nil {
				if !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			} else if err := isolationtest.SeedLeaseTaxFacts(ctx, tx, retireOrg, lease, "2026-01-01"); err != nil {
				return err
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO beds (tenant_id, property_id, unit_id, label, rent_amount_minor, state, lease_id) VALUES
				  ($1, $2, $3, 'B-1', 500000, 'occupied', (SELECT id FROM leases WHERE unit_id = $4::uuid LIMIT 1)),
				  ($1, $2, $3, 'B-2', 500000, 'vacant', NULL)
				ON CONFLICT (unit_id, label) DO NOTHING`,
				retireOrg, retireEmpty, retireUnitBeds, retireUnitLet); err != nil {
				return err
			}
			// The suite commits, so a second run starts where the last one
			// finished: put back what these tests retire.
			if _, err := tx.Exec(ctx, `
				UPDATE properties SET state = 'active', retired_at = NULL WHERE id = $1::uuid;`,
				retireProperty); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE units SET state = 'active', retired_at = NULL
				 WHERE id = ANY($1::uuid[])`,
				[]string{retireUnitLet, retireUnitFree, retireUnitBeds}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE beds SET state = 'vacant', retired_at = NULL
				 WHERE unit_id = $1::uuid AND label = 'B-2'`, retireUnitBeds); err != nil {
				return err
			}

			return tx.QueryRow(ctx, `
				SELECT max(id::text) FILTER (WHERE label = 'B-1'),
				       max(id::text) FILTER (WHERE label = 'B-2')
				  FROM beds WHERE unit_id = $1::uuid`, retireUnitBeds).Scan(&bedLet, &bedFree)
		})
	if err != nil {
		t.Fatalf("seeding the retirement tree: %v", err)
	}
	return bedLet, bedFree
}

func TestRetiringWhatSomebodyLivesIn(t *testing.T) {
	req, plat := ownershipPools(t)
	bedLet, bedFree := seedRetirementTree(t, plat)
	ctx := tenancy.With(context.Background(), tenancy.ID(retireOrg))
	s := store.New(req)

	t.Run("a home somebody is living in cannot be retired", func(t *testing.T) {
		err := s.RetireUnit(ctx, retireUnitLet, "onboarded by mistake")
		if !errors.Is(err, store.ErrNotAllowed) {
			t.Fatalf("retiring a let flat = %v, want ErrNotAllowed", err)
		}
	})

	t.Run("a building with a live tenancy cannot be retired", func(t *testing.T) {
		err := s.RetireProperty(ctx, retireProperty, "sold")
		if !errors.Is(err, store.ErrNotAllowed) {
			t.Fatalf("retiring a let building = %v, want ErrNotAllowed", err)
		}
	})

	t.Run("a bed somebody sleeps in cannot be retired", func(t *testing.T) {
		err := s.RetireBed(ctx, bedLet, "room knocked through")
		if !errors.Is(err, store.ErrNotAllowed) {
			t.Fatalf("retiring an occupied bed = %v, want ErrNotAllowed", err)
		}
	})

	t.Run("a vacant bed leaves the board", func(t *testing.T) {
		if err := s.RetireBed(ctx, bedFree, "room knocked through"); err != nil {
			t.Fatalf("retiring a vacant bed: %v", err)
		}
		beds, err := s.Beds(ctx, retireEmpty)
		if err != nil {
			t.Fatalf("reading the board: %v", err)
		}
		for _, b := range beds {
			if b.ID == bedFree {
				t.Fatal("a retired bed is still on the board")
			}
		}
	})

	t.Run("an empty home leaves the list", func(t *testing.T) {
		if err := s.RetireUnit(ctx, retireUnitFree, "never let"); err != nil {
			t.Fatalf("retiring a vacant flat: %v", err)
		}
		units, err := s.Units(ctx, retireProperty)
		if err != nil {
			t.Fatalf("reading units: %v", err)
		}
		for _, u := range units {
			if u.ID == retireUnitFree {
				t.Fatal("a retired flat is still listed")
			}
		}
	})

	t.Run("no such bed is not a server error", func(t *testing.T) {
		err := s.RetireBed(ctx, "00000000-0000-0000-0000-0000000000ff", "")
		if !errors.Is(err, store.ErrNoBed) {
			t.Fatalf("retiring a bed that does not exist = %v, want ErrNoBed", err)
		}
	})
}
