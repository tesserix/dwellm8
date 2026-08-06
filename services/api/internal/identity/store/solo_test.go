package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The solo manager (#268): one person who owns the flats and manages them.
// There is no second organisation to mandate, and a grant to yourself would be
// a lie the switcher then has to explain.

func soloFirm(t *testing.T, s *store.Principals) string {
	t.Helper()
	id := uid()
	p, _, err := s.Onboard(context.Background(), store.Onboarding{
		Principal: signIn(auth.SurfaceOps, id), OrganisationName: "Solo " + id, Slug: "solo-" + id,
	}.Fill())
	if err != nil {
		t.Fatalf("onboarding the solo manager: %v", err)
	}
	org, _ := p.Organisation()
	return org.String()
}

func TestTheSoloManagerOwnsTheirOwnBooks(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	firm := soloFirm(t, s)

	out, err := s.PreOnboardOwner(ctx, store.OwnerOnboarding{
		FirmOrgID: firm, SelfOwned: true,
		OwnerName: "Meera Menon", Phone: "+919847012345", OrgName: "ignored",
	})
	if err != nil {
		t.Fatalf("self-onboarding: %v", err)
	}
	if out.OrgID != firm {
		t.Fatalf("the owner's books = %s; want the manager's own org %s", out.OrgID, firm)
	}
	if out.GrantID != "" {
		t.Fatalf("a mandate %s was minted over the manager's own books", out.GrantID)
	}
	if out.CreatedOrg {
		t.Fatal("a second organisation was created for a manager who owns the property")
	}

	var grants int
	if err := tenancy.Platform(ctx, plat, "test: counting self-mandates",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*) FROM delegation_grants
				 WHERE tenant_id = $1::uuid AND grantee_org_id = $1::uuid`, firm).Scan(&grants)
		}); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if grants != 0 {
		t.Fatalf("delegation_grants holds %d self-grants; a mandate to yourself is not a mandate", grants)
	}
}

// The switcher is the list of books this manager can open, and their own are
// the first of them.
func TestTheSoloManagersOwnBooksAppearInTheSwitcher(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	firm := soloFirm(t, s)

	if _, err := s.PreOnboardOwner(ctx, store.OwnerOnboarding{
		FirmOrgID: firm, SelfOwned: true,
		OwnerName: "Meera Menon", Phone: "+919847012345",
	}); err != nil {
		t.Fatalf("self-onboarding: %v", err)
	}

	books, err := s.PortfoliosFor(ctx, firm)
	if err != nil {
		t.Fatalf("listing portfolios: %v", err)
	}
	var own *store.ManagedPortfolio
	for i := range books {
		if books[i].OwnerOrgID == firm {
			own = &books[i]
		}
	}
	if own == nil {
		t.Fatalf("the switcher = %+v; want the manager's own books in it", books)
	}
	if own.GrantID != "" {
		t.Fatalf("own books carry grant %s; they are held, not mandated", own.GrantID)
	}
	if !own.SelfManaged {
		t.Fatal("own books are not marked self-managed, so the app cannot label them")
	}
}
