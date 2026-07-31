package store_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Onboarding against PostgreSQL, because the property worth testing is the
// transaction: a committed organisation always has somebody who can reach it.

func principals(t *testing.T) (*store.Principals, tenancy.PlatformPool) {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	plat := tenancy.NewPlatformPool(p)
	return store.New(plat), plat
}

func uid() string { return "uid-" + time.Now().Format("150405.000000") }

func signIn(surface auth.Surface, id string) auth.Principal {
	return auth.Principal{
		UID: id, Surface: surface, Phone: "+919876500000",
		SignInProvider: "phone",
	}
}

// The story's primary scenario: a first sign-in becomes a person, an
// organisation and a membership, together.
func TestAFirstSignInBecomesAnOrganisation(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	id := uid()

	p, err := s.Onboard(ctx, store.Onboarding{
		Principal:        signIn(auth.SurfaceOwn, id),
		OrganisationName: "Menon Properties",
		Slug:             "menon-" + id,
	}.Fill())
	if err != nil {
		t.Fatalf("onboarding: %v", err)
	}
	if len(p.Memberships) != 1 || p.Memberships[0].Role != "owner" {
		t.Fatalf("onboarded with memberships %+v", p.Memberships)
	}

	// And the sign-in now resolves to exactly that organisation.
	back, err := s.Lookup(ctx, signIn(auth.SurfaceOwn, id))
	if err != nil {
		t.Fatalf("looking up: %v", err)
	}
	org, single := back.Organisation()
	if !single || org != p.Memberships[0].TenantID {
		t.Errorf("the sign-in resolved to %q (single=%v)", org, single)
	}
	if back.PartyID != p.PartyID {
		t.Error("the same sign-in produced two different people")
	}

	// The organisation is reachable: it has a member. That is the invariant the
	// single transaction exists to hold.
	var members int
	if err := tenancy.Platform(ctx, plat, "counting members",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM organisation_members WHERE tenant_id = $1::uuid`,
				string(org)).Scan(&members)
		}); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if members != 1 {
		t.Errorf("the organisation has %d members — an organisation nobody can reach is "+
			"invisible to its owner and to support", members)
	}
}

// A failure part-way leaves nothing: no orphan principal, and no organisation
// with nobody in it.
func TestAFailedOnboardingLeavesNothingBehind(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	id := uid()

	// Take the slug first, so the organisation insert collides.
	taken := "taken-" + id
	if err := tenancy.Platform(ctx, plat, "taking a slug",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`INSERT INTO organisations (slug, name, kind) VALUES ($1, 'Existing', 'owner')`, taken)
			return err
		}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	if _, err := s.Onboard(ctx, store.Onboarding{
		Principal:        signIn(auth.SurfaceOwn, id),
		OrganisationName: "Second", Slug: taken,
	}.Fill()); err == nil {
		t.Fatal("two organisations took the same slug")
	}

	// The principal written before the failure is gone with it.
	if _, err := s.Lookup(ctx, signIn(auth.SurfaceOwn, id)); !errors.Is(err, store.ErrUnknownPrincipal) {
		t.Errorf("a principal survived the failed onboarding it was part of: %v", err)
	}
}

// The same uid in two surfaces is two people. This is the property the whole
// pool-per-app design rests on, so it is asserted rather than assumed.
func TestTheSameUidInTwoPoolsIsTwoPeople(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	id := uid()

	own, err := s.Onboard(ctx, store.Onboarding{
		Principal: signIn(auth.SurfaceOwn, id), OrganisationName: "Landlord",
		Slug: "own-" + id,
	}.Fill())
	if err != nil {
		t.Fatalf("onboarding into own: %v", err)
	}
	ops, err := s.Onboard(ctx, store.Onboarding{
		Principal: signIn(auth.SurfaceOps, id), OrganisationName: "Agency",
		Slug: "ops-" + id,
	}.Fill())
	if err != nil {
		t.Fatalf("onboarding into ops: %v", err)
	}

	if own.PartyID == ops.PartyID {
		t.Error("the same uid in two pools produced one person — a uid is unique within a " +
			"pool and not across them, and collapsing them merges two user bases")
	}
	if own.Memberships[0].TenantID == ops.Memberships[0].TenantID {
		t.Error("two pools produced one organisation")
	}
}

// Which surfaces create organisations, and which do not.
func TestOnlySomeSurfacesCreateOrganisations(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()

	for _, c := range []struct {
		surface auth.Surface
		kind    string
		creates bool
	}{
		{auth.SurfaceOwn, "owner", true},
		{auth.SurfaceOps, "agency", true},
		{auth.SurfacePro, "vendor", true},
		{auth.SurfaceLive, "", false},
		{auth.SurfaceFind, "", false},
	} {
		t.Run(string(c.surface), func(t *testing.T) {
			kind, creates := store.KindFor(c.surface)
			if creates != c.creates || kind != c.kind {
				t.Fatalf("KindFor(%s) = %q, %v", c.surface, kind, creates)
			}
			if creates {
				return
			}
			id := uid()
			_, err := s.Onboard(ctx, store.Onboarding{
				Principal: signIn(c.surface, id), OrganisationName: "X", Slug: "x-" + id,
			}.Fill())
			if !errors.Is(err, store.ErrOnboarding) {
				t.Errorf("the %s app created an organisation: %v — a tenant belongs to their "+
					"landlord's and a prospect to nobody", c.surface, err)
			}
		})
	}
}

// Staff are outside every organisation, and onboarding one into a customer's
// would make the product owner a member of it.
func TestStaffDoNotOnboard(t *testing.T) {
	s, _ := principals(t)
	id := uid()

	_, err := s.Onboard(context.Background(), store.Onboarding{
		Principal:        auth.Principal{UID: id, Staff: true},
		OrganisationName: "Dwellm8", Slug: "dwellm8-" + id, Kind: "platform", Role: "owner",
	})
	if err == nil {
		t.Fatal("a staff principal was onboarded into an organisation")
	}
}

// A person in two organisations must choose. Picking the first would be the
// platform deciding silently which of their two hats they are wearing.
func TestSeveralMembershipsIsNotASingleAnswer(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	id := uid()

	first, err := s.Onboard(ctx, store.Onboarding{
		Principal: signIn(auth.SurfaceOps, id), OrganisationName: "Firm A", Slug: "a-" + id,
	}.Fill())
	if err != nil {
		t.Fatalf("onboarding: %v", err)
	}

	// The same person invited into a second organisation.
	if err := tenancy.Platform(ctx, plat, "inviting into a second organisation",
		func(ctx context.Context, tx pgx.Tx) error {
			var other string
			if err := tx.QueryRow(ctx, `
				INSERT INTO organisations (slug, name, kind) VALUES ($1, 'Firm B', 'agency')
				RETURNING id::text`, "b-"+id).Scan(&other); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO organisation_members (tenant_id, party_id, role)
				VALUES ($1::uuid, $2::uuid, 'manager')`, other, first.PartyID)
			return err
		}); err != nil {
		t.Fatalf("seeding the second: %v", err)
	}

	person, err := s.Lookup(ctx, signIn(auth.SurfaceOps, id))
	if err != nil {
		t.Fatalf("looking up: %v", err)
	}
	if len(person.Memberships) != 2 {
		t.Fatalf("the person has %d memberships", len(person.Memberships))
	}
	if _, single := person.Organisation(); single {
		t.Error("a person in two organisations resolved to one without being asked which")
	}
}

// A disabled principal stops resolving. Disabling is a timestamp rather than a
// delete, because the tenancies and the audit rows point at them.
func TestADisabledPrincipalDoesNotResolve(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	id := uid()

	if _, err := s.Onboard(ctx, store.Onboarding{
		Principal: signIn(auth.SurfaceOwn, id), OrganisationName: "Disabled", Slug: "d-" + id,
	}.Fill()); err != nil {
		t.Fatalf("onboarding: %v", err)
	}
	if err := tenancy.Platform(ctx, plat, "disabling", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE identity_principals SET disabled_at = now() WHERE gip_uid = $1`, id)
		return err
	}); err != nil {
		t.Fatalf("disabling: %v", err)
	}

	if _, err := s.Lookup(ctx, signIn(auth.SurfaceOwn, id)); !errors.Is(err, store.ErrUnknownPrincipal) {
		t.Errorf("a disabled principal still resolves: %v", err)
	}
}
