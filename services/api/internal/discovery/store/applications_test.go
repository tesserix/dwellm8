package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Applications against PostgreSQL (#142): the verification gate, open-dedupe,
// the second acceptance blocked naming the first, decline with its reason
// and retention clock, and the sweep honouring exactly that clock.

func applicationsFixture(t *testing.T) (*store.Applications, *store.Prospects, string) {
	t.Helper()
	pool, plat := pools(t)
	listings := store.NewListings(pool)
	apps := store.NewApplications(pool, plat)
	u := unit(t, plat, "AP")
	id, err := listings.Create(owner(), draft(u, fmt.Sprintf("Applyton-%d", time.Now().UnixNano())))
	if err != nil {
		t.Fatalf("creating the listing: %v", err)
	}
	if err := listings.Move(owner(), id, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	return apps, store.NewProspects(plat), id
}

func TestApplicationStory(t *testing.T) {
	apps, prospects, listing := applicationsFixture(t)
	moveIn := time.Now().AddDate(0, 1, 0).Truncate(24 * time.Hour)

	// Applying is contact: a stranger without a verified phone is refused.
	_, hash := token(t)
	stranger, _ := prospects.Ensure(context.Background(), hash)
	if _, err := apps.Apply(context.Background(), listing, stranger.ID, moveIn, 11, nil, ""); !errors.Is(err, store.ErrNotVerified) {
		t.Fatalf("unverified application = %v", err)
	}

	// A verified applicant applies once; applying again is the same row.
	alice := verifiedGuest(t, prospects)
	offer := int64(31_000_00)
	a, err := apps.Apply(context.Background(), listing, alice.ID, moveIn, 11, &offer, "family of three")
	if err != nil {
		t.Fatalf("applying: %v", err)
	}
	again, err := apps.Apply(context.Background(), listing, alice.ID, moveIn, 11, nil, "")
	if err != nil || again.ID != a.ID {
		t.Fatalf("re-apply = %q, %v; want the same application %s", again.ID, err, a.ID)
	}

	// A rival applies too; the owner reviews both.
	bela := verifiedGuest(t, prospects)
	b, err := apps.Apply(context.Background(), listing, bela.ID, moveIn, 11, nil, "")
	if err != nil {
		t.Fatalf("rival applying: %v", err)
	}
	queue, err := apps.ForOwner(owner(), "submitted")
	if err != nil || len(queue) < 2 {
		t.Fatalf("queue = %d, %v; want both applications", len(queue), err)
	}

	// Accept Alice — and the second acceptance is blocked naming the first.
	if err := apps.Review(owner(), a.ID); err != nil {
		t.Fatalf("review: %v", err)
	}
	lease := "aaaaaaaa-1111-2222-3333-444444444444"
	if err := apps.Accept(owner(), a.ID, lease); err != nil {
		t.Fatalf("accepting: %v", err)
	}
	err = apps.Accept(owner(), b.ID, "bbbbbbbb-1111-2222-3333-444444444444")
	if !errors.Is(err, store.ErrAlreadyAccepted) {
		t.Fatalf("second acceptance = %v, want ErrAlreadyAccepted", err)
	}
	if err == nil || !contains(err.Error(), a.ID) {
		t.Fatalf("the refusal does not name the first acceptance: %v", err)
	}

	// Declining the rival needs a reason and stamps the retention clock.
	if err := apps.Decline(owner(), b.ID, "another applicant accepted"); err != nil {
		t.Fatalf("declining: %v", err)
	}
	got, _ := apps.Get(owner(), b.ID)
	if got.State != "declined" || got.DeclineReason == "" {
		t.Fatalf("declined row = %+v", got)
	}
}

func TestRetentionSweepHonoursTheClock(t *testing.T) {
	apps, prospects, listing := applicationsFixture(t)
	pool, plat := pools(t)
	_ = pool
	moveIn := time.Now().AddDate(0, 2, 0).Truncate(24 * time.Hour)

	p := verifiedGuest(t, prospects)
	a, err := apps.Apply(context.Background(), listing, p.ID, moveIn, 11, nil, "")
	if err != nil {
		t.Fatalf("applying: %v", err)
	}
	if err := apps.Decline(owner(), a.ID, "no pets policy"); err != nil {
		t.Fatalf("declining: %v", err)
	}

	// Fresh decline: the sweep must NOT touch it.
	if _, err := apps.SweepRetention(context.Background()); err != nil {
		t.Fatalf("sweeping: %v", err)
	}
	if _, err := apps.Get(owner(), a.ID); err != nil {
		t.Fatalf("a fresh decline was purged early: %v", err)
	}

	// Clock passed: the sweep deletes it, and only platform past-due deletes
	// pass the policy at all.
	if err := tenancy.Platform(context.Background(), plat, "test: lapsing retention",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE listing_applications SET retain_until = CURRENT_DATE - 1 WHERE id = $1`, a.ID)
			return err
		}); err != nil {
		t.Fatalf("lapsing: %v", err)
	}
	n, err := apps.SweepRetention(context.Background())
	if err != nil || n < 1 {
		t.Fatalf("sweep = %d, %v; want the lapsed row purged", n, err)
	}
	if _, err := apps.Get(owner(), a.ID); !errors.Is(err, store.ErrNoApplication) {
		t.Fatalf("the purged row still reads: %v", err)
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
