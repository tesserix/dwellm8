package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// digits is a unique tail for a phone number and a slug: these tests commit,
// and an owner is looked up by number.
func digits() string { return fmt.Sprintf("%07d", time.Now().UnixNano()%10_000_000) }

// Putting an owner down, and closing the firm itself (#356). A manager who took
// an owner on had no way to stop managing for them, and no way to leave at all.

func TestAFirmResignsAMandate(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	firm := soloFirm(t, s)

	token := digits()
	owner, err := s.PreOnboardOwner(ctx, store.OwnerOnboarding{
		FirmOrgID: firm, OwnerName: "Anil Varma", Phone: "+91984" + token,
		OrgName: "Varma Holdings " + token, OrgSlug: "varma-" + token,
	})
	if err != nil {
		t.Fatalf("onboarding the owner: %v", err)
	}

	if err := s.ResignMandate(ctx, firm, owner.GrantID); err != nil {
		t.Fatalf("resigning: %v", err)
	}

	books, err := s.PortfoliosFor(ctx, firm)
	if err != nil {
		t.Fatalf("reading the portfolios: %v", err)
	}
	for _, b := range books {
		if b.OwnerOrgID == owner.OrgID {
			t.Fatal("the owner is still on the firm's book after the mandate ended")
		}
	}

	t.Run("a mandate held by somebody else is not the firm's to end", func(t *testing.T) {
		other := soloFirm(t, s)
		err := s.ResignMandate(ctx, other, owner.GrantID)
		if !errors.Is(err, store.ErrNoMandate) {
			t.Fatalf("resigning another firm's mandate = %v, want ErrNoMandate", err)
		}
	})

	t.Run("a mandate that already ended cannot be ended again", func(t *testing.T) {
		if err := s.ResignMandate(ctx, firm, owner.GrantID); !errors.Is(err, store.ErrNoMandate) {
			t.Fatalf("resigning a mandate already ended = %v, want ErrNoMandate", err)
		}
	})

	var revoked bool
	if err := tenancy.Platform(ctx, plat, "test: reading the grant",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT revoked_at IS NOT NULL FROM delegation_grants WHERE id = $1::uuid`,
				owner.GrantID).Scan(&revoked)
		}); err != nil {
		t.Fatalf("reading the grant: %v", err)
	}
	if !revoked {
		t.Fatal("the mandate row was erased rather than revoked — what a firm was permitted to do is the owner's record")
	}
}

func TestClosingTheFirm(t *testing.T) {
	s, plat := principals(t)
	ctx := context.Background()
	firm := soloFirm(t, s)

	token := digits()
	owner, err := s.PreOnboardOwner(ctx, store.OwnerOnboarding{
		FirmOrgID: firm, OwnerName: "Rekha Nair", Phone: "+91984" + token,
		OrgName: "Nair Estates " + token, OrgSlug: "nair-" + token,
	})
	if err != nil {
		t.Fatalf("onboarding the owner: %v", err)
	}

	if err := s.CloseFirm(ctx, firm, "done letting"); !errors.Is(err, store.ErrNotAllowed) {
		t.Fatalf("closing a firm still holding a mandate = %v, want ErrNotAllowed", err)
	}

	if err := s.ResignMandate(ctx, firm, owner.GrantID); err != nil {
		t.Fatalf("resigning: %v", err)
	}
	if err := s.CloseFirm(ctx, firm, "done letting"); err != nil {
		t.Fatalf("closing an empty firm: %v", err)
	}

	var state string
	var closed bool
	if err := tenancy.Platform(ctx, plat, "test: reading the firm",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT state, closed_at IS NOT NULL FROM organisations WHERE id = $1::uuid`,
				firm).Scan(&state, &closed)
		}); err != nil {
		t.Fatalf("reading the firm: %v", err)
	}
	if state != "closed" || !closed {
		t.Fatalf("the firm is %s, closed_at set = %v; want a closed firm with the date it happened", state, closed)
	}
}
