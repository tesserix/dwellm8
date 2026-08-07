package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// A signed tenancy takes the listing off the market (#332). The state change is
// bookkeeping; the part that matters is that the people holding a viewing are
// told rather than finding out at the door.

func TestALetUnitTakesItsViewingsWithIt(t *testing.T) {
	_, plat := pools(t)
	f := newInspectionFixture(t)

	booked := f.slot(t, 24*time.Hour, 2)
	spare := f.slot(t, 48*time.Hour, 2)
	p := f.verifiedProspect(t)
	if _, _, err := f.inspections.Book(context.Background(), f.listing, p.ID, booked); err != nil {
		t.Fatalf("booking: %v", err)
	}
	asker := f.verifiedProspect(t)
	enq, err := store.NewEnquiries(nil, plat).Create(context.Background(),
		f.listing, asker.ID, "enquiry", "Is parking included?", nil)
	if err != nil {
		t.Fatalf("enquiring: %v", err)
	}

	closed, err := store.NewLetMarker(plat).MarkLetByUnit(context.Background(),
		isolationtest.OrgOwner.String(), f.unit)
	if err != nil || closed != 1 {
		t.Fatalf("MarkLetByUnit = %d, %v; want 1", closed, err)
	}

	for _, id := range []string{booked, spare} {
		if got := slotState(t, plat, id); got != "closed" {
			t.Errorf("slot %s is %q after the unit was let; want closed", id, got)
		}
	}

	// The viewing is called off with the reason on it, and the enquiry that
	// never got as far as a viewing is closed too — the pipeline must not keep
	// showing work that no longer exists.
	state, outcome := enquiryOutcome(t, plat, bookedEnquiry(t, plat, booked))
	if state != "closed" || outcome != "listing_let" {
		t.Errorf("the booked viewing is %s/%s; want closed/listing_let", state, outcome)
	}
	if state, _ := enquiryOutcome(t, plat, enq.ID); state != "closed" {
		t.Errorf("the open enquiry is %s; want closed", state)
	}

	// One cancellation fact per prospect who was holding a viewing: that is what
	// tells them, and it goes through the outbox, never an inline send.
	if n := cancellations(t, plat, f.listing); n != 1 {
		t.Errorf("cancellation events = %d; want 1", n)
	}

	// Idempotent: the same tenancy delivered twice must not tell anyone twice.
	again, err := store.NewLetMarker(plat).MarkLetByUnit(context.Background(),
		isolationtest.OrgOwner.String(), f.unit)
	if err != nil || again != 0 {
		t.Fatalf("second MarkLetByUnit = %d, %v; want 0", again, err)
	}
	if n := cancellations(t, plat, f.listing); n != 1 {
		t.Errorf("cancellation events after a redelivery = %d; want 1", n)
	}
}

// A let listing goes back on the market as itself: the viewings, enquiries and
// outcomes on it are the letting history, so republishing from scratch loses
// exactly what the next appraisal is read from.
func TestALetListingCanGoBackOnTheMarket(t *testing.T) {
	_, plat := pools(t)
	f := newInspectionFixture(t)

	if _, err := store.NewLetMarker(plat).MarkLetByUnit(context.Background(),
		isolationtest.OrgOwner.String(), f.unit); err != nil {
		t.Fatalf("letting: %v", err)
	}
	if err := f.listings.Move(owner(), f.listing, domain.StateLive,
		events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("relisting: %v", err)
	}
	got, err := f.listings.Get(owner(), f.listing)
	if err != nil || got.State != domain.StateLive {
		t.Fatalf("state after relisting = %s, %v; want live", got.State, err)
	}
}

func TestNobodyBooksAViewingOnAListingThatIsNotLive(t *testing.T) {
	_, plat := pools(t)
	f := newInspectionFixture(t)
	slot := f.slot(t, 24*time.Hour, 2)
	p := f.verifiedProspect(t)

	if _, err := store.NewLetMarker(plat).MarkLetByUnit(context.Background(),
		isolationtest.OrgOwner.String(), f.unit); err != nil {
		t.Fatalf("letting: %v", err)
	}
	if _, _, err := f.inspections.Book(context.Background(), f.listing, p.ID, slot); !errors.Is(err, store.ErrListingNotLive) {
		t.Fatalf("booking on a let listing = %v; want ErrListingNotLive", err)
	}
}

func slotState(t *testing.T, plat tenancy.PlatformPool, id string) string {
	t.Helper()
	var state string
	query(t, plat, &state, `SELECT state FROM inspection_slots WHERE id = $1`, id)
	return state
}

func bookedEnquiry(t *testing.T, plat tenancy.PlatformPool, slotID string) string {
	t.Helper()
	var id string
	query(t, plat, &id, `SELECT id::text FROM enquiries WHERE slot_id = $1`, slotID)
	return id
}

func enquiryOutcome(t *testing.T, plat tenancy.PlatformPool, id string) (string, string) {
	t.Helper()
	var state, outcome string
	err := tenancy.Platform(context.Background(), plat, "reading an enquiry in a test",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT state, coalesce(outcome, '') FROM enquiries WHERE id = $1`, id).
				Scan(&state, &outcome)
		})
	if err != nil {
		t.Fatalf("reading enquiry %s: %v", id, err)
	}
	return state, outcome
}

func cancellations(t *testing.T, plat tenancy.PlatformPool, listingID string) int {
	t.Helper()
	var n int
	query(t, plat, &n, `
		SELECT count(*) FROM outbox
		 WHERE type = 'discovery.inspection.cancelled'
		   AND payload->>'listing_id' = $1`, listingID)
	return n
}

func query(t *testing.T, plat tenancy.PlatformPool, into any, sql string, args ...any) {
	t.Helper()
	err := tenancy.Platform(context.Background(), plat, "reading a row in a test",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, sql, args...).Scan(into)
		})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
}
