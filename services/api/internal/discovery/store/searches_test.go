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

// Saved searches against PostgreSQL (#144): dedupe on criteria, a fresh match
// counted exactly once until seen, the opt-out that retains the search, and a
// stranger's searches invisible to another prospect.

func TestSavedSearchStory(t *testing.T) {
	pool, plat := pools(t)
	searches := store.NewSearches(plat)
	listings := store.NewListings(pool)
	prospects := store.NewProspects(plat)
	ctx := context.Background()

	city := fmt.Sprintf("Searchpur-%d", time.Now().UnixNano())
	p := verifiedGuest(t, prospects)

	// Saving twice is one search.
	id, err := searches.Save(ctx, p.ID, city, "", 40_000_00, 2)
	if err != nil {
		t.Fatalf("saving: %v", err)
	}
	again, err := searches.Save(ctx, p.ID, city, "", 40_000_00, 2)
	if err != nil || again != id {
		t.Fatalf("re-save = %q, %v; want the same search %s", again, err, id)
	}

	// Nothing published yet: zero news.
	mine, err := searches.Mine(ctx, p.ID)
	if err != nil || len(mine) != 1 || mine[0].NewMatches != 0 {
		t.Fatalf("mine = %+v, %v; want one search with no news", mine, err)
	}

	// A matching listing goes live: one fresh match, counted once.
	u := unit(t, plat, "SS")
	lid, err := listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("creating the listing: %v", err)
	}
	if err := listings.Move(owner(), lid, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	mine, _ = searches.Mine(ctx, p.ID)
	if len(mine) != 1 || mine[0].NewMatches != 1 {
		t.Fatalf("after publication mine = %+v; want exactly one fresh match", mine)
	}

	// Seen: the same listing is never news again — the no-resend rule.
	if err := searches.Seen(ctx, p.ID, id); err != nil {
		t.Fatalf("marking seen: %v", err)
	}
	mine, _ = searches.Mine(ctx, p.ID)
	if mine[0].NewMatches != 0 {
		t.Fatalf("a seen listing counted again: %+v", mine[0])
	}

	// Opt-out retains the search.
	if err := searches.SetAlerts(ctx, p.ID, id, false); err != nil {
		t.Fatalf("opting out: %v", err)
	}
	mine, _ = searches.Mine(ctx, p.ID)
	if len(mine) != 1 || mine[0].AlertsEnabled {
		t.Fatalf("opt-out lost the search or kept alerts: %+v", mine)
	}

	// Another prospect sees nothing of it, and cannot delete it.
	rival := verifiedGuest(t, prospects)
	if theirs, _ := searches.Mine(ctx, rival.ID); len(theirs) != 0 {
		t.Fatalf("a stranger reads %d of my searches", len(theirs))
	}
	if err := searches.Delete(ctx, rival.ID, id); !errors.Is(err, store.ErrNoSearch) {
		t.Fatalf("a stranger deleted my search: %v", err)
	}

	// I can.
	if err := searches.Delete(ctx, p.ID, id); err != nil {
		t.Fatalf("deleting: %v", err)
	}
	if mine, _ = searches.Mine(ctx, p.ID); len(mine) != 0 {
		t.Fatalf("a deleted search survives: %+v", mine)
	}
}
