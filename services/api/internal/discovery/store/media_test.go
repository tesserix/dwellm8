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
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Listing media against PostgreSQL (#136): membership is idempotent per
// object, position zero is the cover and reordering is one deliberate act,
// moderation removes from public delivery immediately, and an unpublished
// listing's photographs are as private as the flat.

func mediaFixture(t *testing.T) (*store.MediaStore, *store.Listings, string) {
	t.Helper()
	pool, plat := pools(t)
	listings := store.NewListings(pool)
	media := store.NewMedia(pool, plat)
	u := unit(t, plat, "MD")
	id, err := listings.Create(owner(), draft(u, fmt.Sprintf("Mediaville-%d", time.Now().UnixNano())))
	if err != nil {
		t.Fatalf("creating the listing: %v", err)
	}
	return media, listings, id
}

func TestMediaOrderAndModeration(t *testing.T) {
	media, listings, listing := mediaFixture(t)
	ctx := owner()

	// Three photos; re-recording the first is the same photo, not a fourth.
	var ids []string
	for i, path := range []string{"org/x/a.jpg", "org/x/b.jpg", "org/x/c.jpg"} {
		id, err := media.Record(ctx, listing, path, "image/jpeg", i)
		if err != nil {
			t.Fatalf("recording %s: %v", path, err)
		}
		ids = append(ids, id)
	}
	again, err := media.Record(ctx, listing, "org/x/a.jpg", "image/jpeg", 0)
	if err != nil || again != ids[0] {
		t.Fatalf("re-record = %q, %v; want the same row %s", again, err, ids[0])
	}

	// The cover is chosen deliberately: c to the front.
	if err := media.Reorder(ctx, listing, []string{ids[2], ids[0], ids[1]}); err != nil {
		t.Fatalf("reordering: %v", err)
	}
	got, err := media.ForListing(ctx, listing)
	if err != nil || len(got) != 3 || got[0].ID != ids[2] {
		t.Fatalf("order after reorder = %+v, %v; want c first", got, err)
	}

	// Unpublished: a stranger gets nothing, whatever the media rows say.
	pub, err := media.PublicForListing(context.Background(), listing)
	if err != nil || len(pub) != 0 {
		t.Fatalf("draft listing served %d photos publicly", len(pub))
	}

	// Published: all three serve; a report queues; a takedown removes at once.
	if err := listings.Move(ctx, listing, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	pub, _ = media.PublicForListing(context.Background(), listing)
	if len(pub) != 3 {
		t.Fatalf("live listing served %d photos, want 3", len(pub))
	}

	if err := media.Report(context.Background(), listing, ids[1]); err != nil {
		t.Fatalf("reporting: %v", err)
	}
	pub, _ = media.PublicForListing(context.Background(), listing)
	if len(pub) != 2 {
		t.Fatalf("a reported photo kept serving publicly: %d", len(pub))
	}

	if err := media.Takedown(ctx, listing, ids[1], "personal information visible"); err != nil {
		t.Fatalf("takedown: %v", err)
	}
	// Removed is gone from the owner's working set too, and a second takedown
	// has nothing to act on.
	mine, _ := media.ForListing(ctx, listing)
	if len(mine) != 2 {
		t.Fatalf("owner still sees the removed photo: %d", len(mine))
	}
	if err := media.Takedown(ctx, listing, ids[1], "again"); !errors.Is(err, store.ErrNoMedia) {
		t.Fatalf("double takedown = %v, want ErrNoMedia", err)
	}
}

// Another organisation can neither attach media to my listing nor see mine.
func TestMediaIsolation(t *testing.T) {
	media, _, listing := mediaFixture(t)
	other := otherOrg()
	if _, err := media.Record(other, listing, "org/y/evil.jpg", "image/jpeg", 0); !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("another organisation attached media: %v", err)
	}
	if _, err := media.Record(owner(), listing, "org/x/mine.jpg", "image/jpeg", 0); err != nil {
		t.Fatalf("recording: %v", err)
	}
	got, err := media.ForListing(other, listing)
	if err != nil || len(got) != 0 {
		t.Fatalf("another organisation read %d media rows", len(got))
	}
}

func otherOrg() context.Context {
	return tenancy.With(context.Background(), isolationtest.OrgFirm)
}
