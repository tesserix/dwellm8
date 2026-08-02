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

// The alert fan-out (#126 delivering #144): a publication reaches exactly the
// opted-in searches it satisfies, once — the redelivered event finds the
// watermark already advanced — and never a disabled token.
func TestAlertsForListing(t *testing.T) {
	pool, plat := pools(t)
	searches := store.NewSearches(plat)
	tokens := store.NewPushTokens(plat)
	listings := store.NewListings(pool)
	prospects := store.NewProspects(plat)
	ctx := context.Background()

	city := fmt.Sprintf("Alertabad-%d", time.Now().UnixNano())
	watcher := verifiedGuest(t, prospects)
	muted := verifiedGuest(t, prospects)

	wid, err := searches.Save(ctx, watcher.ID, city, "", 0, 0)
	if err != nil {
		t.Fatalf("saving: %v", err)
	}
	if _, err := searches.Save(ctx, muted.ID, city, "", 0, 0); err != nil {
		t.Fatalf("saving muted: %v", err)
	}
	mutedID, _ := searches.Mine(ctx, muted.ID)
	if err := searches.SetAlerts(ctx, muted.ID, mutedID[0].ID, false); err != nil {
		t.Fatalf("muting: %v", err)
	}
	tok := fmt.Sprintf("ExponentPushToken[t-%d]", time.Now().UnixNano())
	if err := tokens.Register(ctx, watcher.ID, tok, "ios"); err != nil {
		t.Fatalf("registering: %v", err)
	}
	if err := tokens.Register(ctx, muted.ID, tok+"-m", "android"); err != nil {
		t.Fatalf("registering muted: %v", err)
	}

	u := unit(t, plat, "AL")
	lid, err := listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}
	if err := listings.Move(owner(), lid, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}

	l, targets, err := searches.AlertsForListing(ctx, lid)
	if err != nil {
		t.Fatalf("matching: %v", err)
	}
	if l.City != city || len(targets) != 1 || targets[0].Token != tok || targets[0].SearchID != wid {
		t.Fatalf("targets = %+v (listing %+v); want exactly the opted-in watcher", targets, l)
	}

	// Redelivery: the watermark has moved, nobody hears it twice.
	if _, again, _ := searches.AlertsForListing(ctx, lid); len(again) != 0 {
		t.Fatalf("a redelivered event alerted again: %+v", again)
	}

	// A dead token is disabled and stops being a target for the next listing.
	if err := tokens.Disable(ctx, []string{tok}); err != nil {
		t.Fatalf("disabling: %v", err)
	}
	lid2, err := listings.Create(owner(), draft(unit(t, plat, "AM"), city))
	if err != nil {
		t.Fatalf("creating second: %v", err)
	}
	if err := listings.Move(owner(), lid2, domain.StateLive, events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing second: %v", err)
	}
	if _, dead, _ := searches.AlertsForListing(ctx, lid2); len(dead) != 0 {
		t.Fatalf("a disabled token was targeted: %+v", dead)
	}
}
