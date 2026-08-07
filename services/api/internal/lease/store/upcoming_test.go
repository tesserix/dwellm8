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

// What falls due next, over the whole book in one query (#337).

func TestUpcomingCarriesTheNextRentOnEachLiveTenancy(t *testing.T) {
	req, plat := pools(t)
	leases := store.New(req)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)

	soon := seedTenancyDueOn(t, plat, "2026-01-01", "2026-12-31", 31_000_00, 10)
	// Rent due on the 28th, asked about a window that closes on the 20th.
	late := seedTenancyDueOn(t, plat, "2026-01-01", "2026-12-31", 44_000_00, 28)

	due, err := leases.Upcoming(ctx, effective.Day(2026, 6, 1), effective.Day(2026, 6, 20), 500)
	if err != nil {
		t.Fatalf("reading what falls due: %v", err)
	}

	got, ok := find(due, soon)
	if !ok {
		t.Fatalf("the tenancy due on the 10th is not in the window 1–20 June")
	}
	if want := effective.Day(2026, 6, 10); !got.DueOn.Equal(want) {
		t.Errorf("falls due %s, want %s", got.DueOn, want)
	}
	if got.AmountMinor != 31_000_00 {
		t.Errorf("rent %d, want 3100000", got.AmountMinor)
	}
	if got.PropertyName == "" || got.UnitCode == "" {
		t.Errorf("a reminder names where it is: property %q unit %q", got.PropertyName, got.UnitCode)
	}
	if _, ok := find(due, late); ok {
		t.Error("a rent falling due on the 28th is not a reminder for a window closing on the 20th")
	}
}

// One row per tenancy, whatever the window's length: a manager wants the next
// thing to happen, not every month of the year listed under the same flat.
func TestUpcomingCarriesTheNextChargeOnly(t *testing.T) {
	req, plat := pools(t)
	leases := store.New(req)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)

	lease := seedTenancyDueOn(t, plat, "2026-01-01", "2026-12-31", 31_000_00, 10)

	due, err := leases.Upcoming(ctx, effective.Day(2026, 6, 1), effective.Day(2026, 9, 30), 500)
	if err != nil {
		t.Fatalf("reading what falls due: %v", err)
	}

	seen := 0
	for _, d := range due {
		if d.LeaseID == lease {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("the tenancy appears %d times over four months, want once", seen)
	}
	got, _ := find(due, lease)
	if want := effective.Day(2026, 6, 10); !got.DueOn.Equal(want) {
		t.Errorf("falls due %s, want the soonest, %s", got.DueOn, want)
	}
}

// The rent in force on the day it falls due, not the one the tenancy opened
// with — a revision effective in June is what June's reminder is for.
func TestUpcomingUsesTheRentInForceOnTheDueDate(t *testing.T) {
	req, plat := pools(t)
	leases := store.New(req)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)

	lease := seedTenancyDueOn(t, plat, "2026-01-01", "2026-12-31", 31_000_00, 10)
	err := tenancy.Platform(context.Background(), plat, "revising the rent",
		func(ctx context.Context, tx pgx.Tx) error {
			// Closed then reopened: one rent at a time is an exclusion constraint,
			// not a convention.
			if _, err := tx.Exec(ctx, `
				UPDATE rent_schedule SET valid_to = date '2026-06-01' WHERE lease_id = $1::uuid`,
				lease); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, due_day, valid_from, valid_to)
				VALUES ($1, $2, 34_000_00, 10, date '2026-06-01', date '2026-12-31')`,
				isolationtest.OrgOwner.String(), lease)
			return err
		})
	if err != nil {
		t.Fatalf("revising the rent: %v", err)
	}

	due, err := leases.Upcoming(ctx, effective.Day(2026, 6, 1), effective.Day(2026, 6, 30), 500)
	if err != nil {
		t.Fatalf("reading what falls due: %v", err)
	}
	got, ok := find(due, lease)
	if !ok {
		t.Fatal("the revised tenancy fell out of the window")
	}
	if got.AmountMinor != 34_000_00 {
		t.Errorf("rent %d, want 3400000 — the schedule in force in June", got.AmountMinor)
	}
}

// A due day of 31 means the last day of the month, whatever that month is.
func TestUpcomingClampsTheDueDayToTheMonthsLength(t *testing.T) {
	req, plat := pools(t)
	leases := store.New(req)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)

	lease := seedTenancyDueOn(t, plat, "2026-01-01", "2026-12-31", 31_000_00, 31)

	due, err := leases.Upcoming(ctx, effective.Day(2026, 6, 1), effective.Day(2026, 6, 30), 500)
	if err != nil {
		t.Fatalf("reading what falls due: %v", err)
	}
	got, ok := find(due, lease)
	if !ok {
		t.Fatal("a tenancy due on the 31st has nothing due in June — the 30th is the last day")
	}
	if want := effective.Day(2026, 6, 30); !got.DueOn.Equal(want) {
		t.Errorf("falls due %s, want %s", got.DueOn, want)
	}
}

// Nothing falls due after the term ends.
func TestUpcomingStopsAtTheEndOfTheTerm(t *testing.T) {
	req, plat := pools(t)
	leases := store.New(req)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)

	lease := seedTenancyDueOn(t, plat, "2026-01-01", "2026-06-05", 31_000_00, 10)

	due, err := leases.Upcoming(ctx, effective.Day(2026, 6, 6), effective.Day(2026, 7, 31), 500)
	if err != nil {
		t.Fatalf("reading what falls due: %v", err)
	}
	if _, ok := find(due, lease); ok {
		t.Error("a tenancy whose term ended on 5 June still has rent falling due in July")
	}
}

func find(due []store.Due, leaseID string) (store.Due, bool) {
	for _, d := range due {
		if d.LeaseID == leaseID {
			return d, true
		}
	}
	return store.Due{}, false
}

// seedTenancyDueOn is seedLiveTenancy with the day of the month named, and the
// lease returned so a test can find its own row.
func seedTenancyDueOn(t *testing.T, plat tenancy.PlatformPool, from, to string, rentMinor int64, dueDay int) string {
	t.Helper()
	_, unitID := unit(t, plat, "UPC-"+token())
	var lease string
	err := tenancy.Platform(context.Background(), plat, "seeding a tenancy with a due day",
		func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `
				INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES (gen_random_uuid(), $1, $2, $3, 'active', $4::date, $5::date)
				RETURNING id::text`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unitID, from, to,
			).Scan(&lease); err != nil {
				return err
			}
			if err := isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgOwner.String(), lease, from); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, due_day, valid_from, valid_to)
				VALUES ($1, $2, $3, $4, $5::date, $6::date)`,
				isolationtest.OrgOwner.String(), lease, rentMinor, dueDay, from, to)
			return err
		})
	if err != nil {
		t.Fatalf("seeding a tenancy: %v", err)
	}
	return lease
}
