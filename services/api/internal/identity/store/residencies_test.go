package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Which tenancies a renter is on, and which of them they are shown. ADR-0029 §3.
//
// Against a real database because the question is a join across three tables
// under two organisations, and the interesting half is what it leaves out.

func TestResidenciesSpanLandlordsAndExcludeDrafts(t *testing.T) {
	s, plat := principals(t)
	isolationtest.SeedResidentFixtures(t, plat)
	ctx := context.Background()

	// A draft naming the same renter. ADR-0010 permits two competing drafts on
	// one flat, so a draft is an offer rather than a tenancy — presenting one as
	// somewhere they live would show a prospect a document nobody has agreed to.
	draft := seedDraftFor(t, plat, isolationtest.ResidentPriya)

	got, err := s.Residencies(ctx, isolationtest.ResidentPriya)
	if err != nil {
		t.Fatalf("reading residencies: %v", err)
	}

	seen := map[string]tenancy.ID{}
	for _, r := range got {
		seen[r.LeaseID] = r.TenantID
	}
	if _, ok := seen[draft]; ok {
		t.Errorf("a draft lease appeared in a renter's tenancies — it is the landlord's working " +
			"document, and two competing drafts on one flat are legitimate")
	}
	if len(seen) != 2 {
		t.Fatalf("Priya is on %d tenancies, want 2 — one per landlord: %v", len(seen), seen)
	}
	if seen[isolationtest.LeasePriyaOwner] == seen[isolationtest.LeasePriyaOther] {
		t.Errorf("both tenancies resolved to one organisation — the two landlords are meant to be distinct")
	}

	// And the organisation name comes back, because it is the only thing a
	// renter is shown about the other party.
	for _, r := range got {
		if r.Organisation == "" {
			t.Errorf("lease %s came back with no landlord name — a tenant has to know who they pay", r.LeaseID)
		}
	}
}

// A renter who is on nothing gets an empty answer rather than an error. The
// caller turns that into "there is no tenancy on this number", which is a
// different thing from a refusal.
func TestARenterOnNoLeaseHasNoResidencies(t *testing.T) {
	s, _ := principals(t)
	got, err := s.Residencies(context.Background(), "d1111111-0000-0000-0000-0000000000ff")
	if err != nil {
		t.Fatalf("reading residencies for a stranger: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("a party on no lease has %d residencies", len(got))
	}
}

// A lookup for nobody is a bug in the caller, not an empty result set. It is
// refused before the database is asked.
func TestResidenciesRefusesAnEmptyParty(t *testing.T) {
	s, _ := principals(t)
	if _, err := s.Residencies(context.Background(), ""); err == nil {
		t.Fatalf("a lookup for no party succeeded")
	}
}

// The pre-registration, and the property the partial unique index exists for: a
// second landlord entering the same number resolves to the party already there.
func TestOneRenterKeepsOnePartyAcrossLandlords(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	phone := fmt.Sprintf("+9198%08d", time.Now().UnixNano()%100000000)

	first, err := s.EnsureResident(ctx, phone)
	if err != nil {
		t.Fatalf("pre-registering a renter: %v", err)
	}
	second, err := s.EnsureResident(ctx, phone)
	if err != nil {
		t.Fatalf("pre-registering the same renter again: %v", err)
	}
	if first != second {
		t.Fatalf("one number produced two parties (%s and %s) — the renter would sign in and find "+
			"one of their two flats", first, second)
	}

	// And the first sign-in claims that party rather than minting another, which
	// is what keeps their history theirs.
	claimed, err := s.ClaimResident(ctx, auth.Principal{
		UID: uid(), Surface: auth.SurfaceLive, Phone: phone, SignInProvider: "phone",
	})
	if err != nil {
		t.Fatalf("claiming the pre-registration: %v", err)
	}
	if claimed.PartyID != first {
		t.Fatalf("the sign-in resolved to party %s, want the pre-registered %s", claimed.PartyID, first)
	}
}

// A number nobody has ever put on a lease is not an account waiting to be
// created. A renter cannot onboard themselves — there is nothing for them to be
// a tenant of.
func TestAnUnknownNumberCannotClaimAnything(t *testing.T) {
	s, _ := principals(t)
	_, err := s.ClaimResident(context.Background(), auth.Principal{
		UID: uid(), Surface: auth.SurfaceLive,
		Phone: fmt.Sprintf("+9197%08d", time.Now().UnixNano()%100000000),
	})
	if !errors.Is(err, store.ErrUnknownPrincipal) {
		t.Fatalf("an unknown number claimed: %v", err)
	}
}

// A number that is not a number is refused before the database sees it, so the
// failure names the field rather than arriving as a constraint violation.
func TestAMalformedNumberIsRefused(t *testing.T) {
	s, _ := principals(t)
	for _, bad := range []string{"", "9876500001", "+91 98765 00001", "+91abcdefghij"} {
		if _, err := s.EnsureResident(context.Background(), bad); !errors.Is(err, store.ErrPhone) {
			t.Errorf("%q was accepted as a mobile number: %v", bad, err)
		}
	}
}

// seedDraftFor writes a draft lease naming a renter, on a unit of its own so the
// no-double-let constraint cannot refuse it.
func seedDraftFor(t *testing.T, plat tenancy.PlatformPool, party string) string {
	t.Helper()
	var lease string
	tok := fmt.Sprintf("%d", time.Now().UnixNano()%1000000)
	err := tenancy.Platform(context.Background(), plat, "seeding a draft for the residency test",
		func(ctx context.Context, tx pgx.Tx) error {
			var unit string
			if err := tx.QueryRow(ctx, `
				INSERT INTO units (tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft)
				VALUES ($1, $2, 'flat', $3, 9, 500.00)
				RETURNING id::text`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, "DRAFT-"+tok).Scan(&unit); err != nil {
				return fmt.Errorf("seeding the unit: %w", err)
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, 'draft', date '2027-01-01', date '2027-12-31')
				RETURNING id::text`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unit).Scan(&lease); err != nil {
				return fmt.Errorf("seeding the draft: %w", err)
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO lease_parties (tenant_id, lease_id, party_id, role, valid_from)
				VALUES ($1, $2, $3, 'tenant', date '2027-01-01')`,
				isolationtest.OrgOwner.String(), lease, party)
			return err
		})
	if err != nil {
		t.Fatalf("seeding a draft: %v", err)
	}
	return lease
}
