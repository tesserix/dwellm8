package store_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

// Listing moderation against PostgreSQL (#143): a report queues the listing,
// suspension pulls it from public view at once — shared links included — and
// reinstatement puts it back with the report cleared.

func moderationFixture(t *testing.T) (*store.Moderation, *store.Listings, *store.Public, string) {
	t.Helper()
	pool, plat := pools(t)
	listings := store.NewListings(pool)
	mod := store.NewModeration(pool, plat)
	u := unit(t, plat, "MO")
	id, err := listings.Create(owner(), draft(u, fmt.Sprintf("Modville-%d", time.Now().UnixNano())))
	if err != nil {
		t.Fatalf("creating the listing: %v", err)
	}
	if err := listings.Move(owner(), id, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	return mod, listings, store.NewPublic(pool), id
}

func TestModerationStory(t *testing.T) {
	mod, _, public, listing := moderationFixture(t)
	ctx := owner()

	// A stranger reports it twice: one queue entry, two counts.
	for range 2 {
		if err := mod.ReportListing(context.Background(), listing, "fraud"); err != nil {
			t.Fatalf("reporting: %v", err)
		}
	}
	queue, err := mod.Queue(ctx)
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	mine := flagged(queue, listing)
	if mine == nil || mine.ReportCount != 2 || mine.ReportedAt == nil {
		t.Fatalf("queue entry = %+v, want 2 reports with a timestamp", mine)
	}
	if other, _ := mod.Queue(otherOrg()); flagged(other, listing) != nil {
		t.Fatal("another organisation's queue holds my report")
	}

	// Suspended: gone from the public detail — the shared-link case — at once.
	if err := mod.Suspend(ctx, listing, "fraudulent advertisement",
		events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("suspending: %v", err)
	}
	if _, err := public.Detail(context.Background(), listing); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("a suspended listing still answers its shared link: %v", err)
	}
	// Suspension is not the owner's pause: resuming it is not a move.
	if err := mod.Suspend(ctx, listing, "twice", events.Actor{Kind: events.ActorSystem}); !errors.Is(err, domain.ErrTransition) {
		t.Fatalf("double suspension = %v, want ErrTransition", err)
	}

	// Reinstated: serving again, queue empty.
	if err := mod.Reinstate(ctx, listing, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("reinstating: %v", err)
	}
	if _, err := public.Detail(context.Background(), listing); err != nil {
		t.Fatalf("a reinstated listing is not serving: %v", err)
	}
	queue, _ = mod.Queue(ctx)
	if flagged(queue, listing) != nil {
		t.Fatal("a reinstated listing is still queued")
	}
}

func TestDismissReports(t *testing.T) {
	mod, _, public, listing := moderationFixture(t)
	if err := mod.ReportListing(context.Background(), listing, "other"); err != nil {
		t.Fatalf("reporting: %v", err)
	}
	if err := mod.DismissReports(owner(), listing); err != nil {
		t.Fatalf("dismissing: %v", err)
	}
	queue, _ := mod.Queue(owner())
	if flagged(queue, listing) != nil {
		t.Fatal("a dismissed report is still queued")
	}
	// The listing never left the market.
	if _, err := public.Detail(context.Background(), listing); err != nil {
		t.Fatalf("a dismissed listing is not serving: %v", err)
	}
	if err := mod.DismissReports(owner(), listing); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("double dismiss = %v, want ErrNoListing", err)
	}
}

func flagged(list []store.FlaggedListing, id string) *store.FlaggedListing {
	for i := range list {
		if list[i].ID == id {
			return &list[i]
		}
	}
	return nil
}
