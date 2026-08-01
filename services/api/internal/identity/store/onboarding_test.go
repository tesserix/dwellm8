package store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// A retried onboarding is the same onboarding: the second call returns the
// organisation the first created, and creates nothing. Issue #31 — a
// double-tap on a slow connection must not split one landlord's books in two.
func TestOnboardingTwiceReturnsTheSameOrganisation(t *testing.T) {
	s, _ := principals(t)
	ctx := context.Background()
	id := uid()

	first, created, err := s.Onboard(ctx, store.Onboarding{
		Principal: signIn(auth.SurfaceOwn, id), OrganisationName: "Twice", Slug: "t1-" + id,
	}.Fill())
	if err != nil || !created {
		t.Fatalf("first onboarding: created=%v err=%v", created, err)
	}

	second, created, err := s.Onboard(ctx, store.Onboarding{
		Principal: signIn(auth.SurfaceOwn, id), OrganisationName: "Twice Again", Slug: "t2-" + id,
	}.Fill())
	if err != nil || created {
		t.Fatalf("second onboarding: created=%v err=%v — a retry must not create", created, err)
	}
	if second.Memberships[0].TenantID != first.Memberships[0].TenantID {
		t.Fatalf("the retry got a different organisation: %s then %s",
			first.Memberships[0].TenantID, second.Memberships[0].TenantID)
	}
}

// The birth is published in the same transaction, because the authz projector
// turns it into the organisation's first edge.
func TestOnboardingPublishesTheBirth(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	id := uid()

	p, _, err := s.Onboard(ctx, store.Onboarding{
		Principal: signIn(auth.SurfaceOwn, id), OrganisationName: "Published", Slug: "p-" + id,
	}.Fill())
	if err != nil {
		t.Fatalf("onboarding: %v", err)
	}
	org, _ := p.Organisation()

	var n int
	err = tenancy.Platform(ctx, plat, "test: reading the outbox",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*) FROM outbox
				 WHERE type = 'identity.organisation.created' AND subject_id = $1`,
				string(org)).Scan(&n)
		})
	if err != nil || n != 1 {
		t.Fatalf("the birth must be in the outbox exactly once: n=%d err=%v", n, err)
	}
}
