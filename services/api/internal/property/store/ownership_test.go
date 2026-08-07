package store_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// Who owns the building. Nothing else in the product answered this, so every
// tenancy onboarded through the ops surface was unbillable: the billing run
// refuses a lease with no owner to credit the rent to, and refuses it every
// half hour, for ever (#302).

func ownershipPools(t *testing.T) (tenancy.Pool, tenancy.PlatformPool) {
	t.Helper()
	dsn, plat := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" || plat == "" {
		t.Skip("TEST_DATABASE_URL and TEST_PLATFORM_DATABASE_URL are not set")
	}
	req, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(req.Close)
	p, err := pgxpool.New(context.Background(), plat)
	if err != nil {
		t.Fatalf("connecting as platform: %v", err)
	}
	t.Cleanup(p.Close)
	return req, tenancy.NewPlatformPool(p)
}

// liveRowsFor counts what the billing run's own query would find for one owner.
// Scoped to the party because the isolation fixtures commit, and a property
// shared with them would make this assert on somebody else's rows.
func liveRowsFor(t *testing.T, plat tenancy.PlatformPool, propertyID, party string, on effective.Date) int {
	t.Helper()
	var n int
	err := tenancy.Platform(context.Background(), plat, "reading ownership",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*)
				  FROM property_ownership
				 WHERE property_id = $1 AND owner_party_id = $2::uuid
				   AND retired_at IS NULL AND validity @> $3::date`,
				propertyID, party, on.Time()).Scan(&n)
		})
	if err != nil {
		t.Fatalf("reading ownership: %v", err)
	}
	return n
}

// owner_party_id carries no foreign key, so a fixed uuid is enough to assert on.
const ownerParty = "a9999999-0000-0000-0000-000000000001"

func TestRecordOwnership(t *testing.T) {
	req, plat := ownershipPools(t)
	isolationtest.SeedPropertyTree(t, plat)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	s := store.New(req)
	from := effective.DateOf(time.Now(), time.UTC)

	t.Run("records who the rent is credited to", func(t *testing.T) {
		property := isolationtest.PropertyOther
		owner := ownerParty

		if err := s.RecordOwnership(ctx, property, owner, from); err != nil {
			t.Fatalf("recording ownership: %v", err)
		}

		if got := liveRowsFor(t, plat, property, owner, from); got != 1 {
			t.Fatalf("rows the billing run would find = %d, want 1", got)
		}
	})

	t.Run("a retried onboarding does not record the same owner twice", func(t *testing.T) {
		property := isolationtest.PropertyGranted
		owner := ownerParty

		for range 3 {
			if err := s.RecordOwnership(ctx, property, owner, from); err != nil {
				t.Fatalf("recording ownership: %v", err)
			}
		}

		if got := liveRowsFor(t, plat, property, owner, from); got != 1 {
			t.Fatalf("live ownership rows = %d, want exactly one", got)
		}
	})
}
