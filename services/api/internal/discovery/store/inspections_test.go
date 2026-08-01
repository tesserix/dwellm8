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

// The viewing loop against PostgreSQL: slots with capacity, the booking race,
// reschedules that keep their context, cancellations that release the place,
// and outcomes that aggregate for the owner without naming a prospect.

type inspectionFixture struct {
	listings    *store.Listings
	inspections *store.Inspections
	prospects   *store.Prospects
	listing     string
}

func newInspectionFixture(t *testing.T) inspectionFixture {
	t.Helper()
	pool, plat := pools(t)
	f := inspectionFixture{
		listings:    store.NewListings(pool),
		inspections: store.NewInspections(pool, plat),
		prospects:   store.NewProspects(plat),
	}
	city := fmt.Sprintf("Slotford-%d", time.Now().UnixNano())
	u := unit(t, plat, fmt.Sprintf("S-%d", time.Now().UnixNano()%1_000_000))
	id, err := f.listings.Create(owner(), draft(u, city))
	if err != nil {
		t.Fatalf("creating the listing: %v", err)
	}
	if err := f.listings.Move(owner(), id, domain.StateLive,
		events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	f.listing = id
	return f
}

func (f inspectionFixture) verifiedProspect(t *testing.T) store.Prospect {
	t.Helper()
	_, hash := token(t)
	p, err := f.prospects.Ensure(context.Background(), hash)
	if err != nil {
		t.Fatalf("prospect: %v", err)
	}
	if err := f.prospects.Verify(context.Background(), hash, "exo-"+p.ID[:8], "XXXXXX7777"); err != nil {
		t.Fatalf("verifying: %v", err)
	}
	p.Verified = true
	return p
}

func (f inspectionFixture) slot(t *testing.T, in time.Duration, capacity int) string {
	t.Helper()
	id, err := f.inspections.PublishSlot(owner(), f.listing, store.SlotDraft{
		StartsAt: time.Now().Add(in).Truncate(time.Second), Capacity: capacity,
		AssignedTo: "Meera", MeetingPoint: "Tower A lobby, ask for Meera",
	})
	if err != nil {
		t.Fatalf("publishing a slot: %v", err)
	}
	return id
}

func TestBookingRaceAndMeetingPoint(t *testing.T) {
	f := newInspectionFixture(t)
	slotA := f.slot(t, 24*time.Hour, 1)
	f.slot(t, 26*time.Hour, 1) // the alternative the loser is offered

	// A prospect browsing slots sees times and remaining places, never the
	// exact meeting point — that arrives with a confirmed booking.
	open, err := f.inspections.OpenSlots(context.Background(), f.listing)
	if err != nil || len(open) != 2 {
		t.Fatalf("open slots = %d, %v; want 2", len(open), err)
	}
	for _, s := range open {
		if s.MeetingPoint != "" {
			t.Fatalf("a browsing prospect sees the meeting point")
		}
	}

	p1, p2 := f.verifiedProspect(t), f.verifiedProspect(t)
	booked, _, err := f.inspections.Book(context.Background(), f.listing, p1.ID, slotA)
	if err != nil {
		t.Fatalf("booking: %v", err)
	}
	if booked.MeetingPoint == "" || booked.Enquiry.State != "scheduled" {
		t.Fatalf("confirmation = %+v; want the meeting point and a scheduled enquiry", booked)
	}

	// The last place is gone: the second prospect is refused by name and
	// offered the adjacent slot, not silence (#139).
	_, alts, err := f.inspections.Book(context.Background(), f.listing, p2.ID, slotA)
	if !errors.Is(err, store.ErrSlotFull) {
		t.Fatalf("second booking = %v, want ErrSlotFull", err)
	}
	if len(alts) != 1 {
		t.Fatalf("alternatives = %d, want the one open slot", len(alts))
	}
}

func TestUnverifiedProspectCannotBook(t *testing.T) {
	f := newInspectionFixture(t)
	slot := f.slot(t, 24*time.Hour, 2)

	_, hash := token(t)
	p, err := f.prospects.Ensure(context.Background(), hash)
	if err != nil {
		t.Fatalf("prospect: %v", err)
	}
	if _, _, err := f.inspections.Book(context.Background(), f.listing, p.ID, slot); !errors.Is(err, store.ErrNotVerified) {
		t.Fatalf("unverified booking = %v, want ErrNotVerified", err)
	}
}

func TestRescheduleKeepsContextAndReleasesThePlace(t *testing.T) {
	f := newInspectionFixture(t)
	slotA, slotB := f.slot(t, 24*time.Hour, 1), f.slot(t, 48*time.Hour, 1)
	p := f.verifiedProspect(t)

	booked, _, err := f.inspections.Book(context.Background(), f.listing, p.ID, slotA)
	if err != nil {
		t.Fatalf("booking: %v", err)
	}

	moved, _, err := f.inspections.Reschedule(context.Background(), p.ID, booked.Enquiry.ID, slotB)
	if err != nil {
		t.Fatalf("rescheduling: %v", err)
	}
	if moved.Enquiry.ID != booked.Enquiry.ID {
		t.Fatalf("reschedule minted a new enquiry — the original context is lost (#140)")
	}

	// The old place is free again: another prospect books it.
	p2 := f.verifiedProspect(t)
	if _, _, err := f.inspections.Book(context.Background(), f.listing, p2.ID, slotA); err != nil {
		t.Fatalf("rebooking the released slot: %v", err)
	}
}

func TestCancelReleasesAndConcludesOnce(t *testing.T) {
	f := newInspectionFixture(t)
	slot := f.slot(t, 24*time.Hour, 1)
	p := f.verifiedProspect(t)

	booked, _, err := f.inspections.Book(context.Background(), f.listing, p.ID, slot)
	if err != nil {
		t.Fatalf("booking: %v", err)
	}
	if err := f.inspections.CancelByProspect(context.Background(), p.ID, booked.Enquiry.ID); err != nil {
		t.Fatalf("cancelling: %v", err)
	}
	// Once: a second cancellation has nothing to act on.
	if err := f.inspections.CancelByProspect(context.Background(), p.ID, booked.Enquiry.ID); !errors.Is(err, store.ErrNotReschedulable) {
		t.Fatalf("double cancel = %v, want ErrNotReschedulable", err)
	}
	// The place is free again.
	p2 := f.verifiedProspect(t)
	if _, _, err := f.inspections.Book(context.Background(), f.listing, p2.ID, slot); err != nil {
		t.Fatalf("rebooking after cancellation: %v", err)
	}
	// And another prospect cannot cancel somebody else's booking.
	b2, _, _ := f.inspections.Book(context.Background(), f.listing, p.ID, f.slot(t, 72*time.Hour, 1))
	if err := f.inspections.CancelByProspect(context.Background(), p2.ID, b2.Enquiry.ID); !errors.Is(err, store.ErrNotReschedulable) {
		t.Fatalf("cancelling another prospect's viewing = %v, want refusal", err)
	}
}

func TestOutcomesAggregateWithoutNamingAnybody(t *testing.T) {
	f := newInspectionFixture(t)
	actor := events.Actor{Kind: events.ActorSystem}

	book := func(i int) store.Booked {
		p := f.verifiedProspect(t)
		s := f.slot(t, 24*time.Hour+time.Duration(i)*time.Hour, 1)
		b, _, err := f.inspections.Book(context.Background(), f.listing, p.ID, s)
		if err != nil {
			t.Fatalf("booking: %v", err)
		}
		return b
	}

	// Two viewings cite price; one is a no-show.
	for i, o := range []store.Outcome{
		{Outcome: "not_interested", Objections: []string{"price", "size"}},
		{Outcome: "not_interested", Objections: []string{"price"}},
		{Outcome: "prospect_no_show"},
	} {
		b := book(i)
		if err := f.inspections.RecordOutcome(owner(), b.Enquiry.ID, o, actor); err != nil {
			t.Fatalf("recording %s: %v", o.Outcome, err)
		}
	}

	fb, err := f.inspections.ListingFeedback(owner(), f.listing)
	if err != nil {
		t.Fatalf("feedback: %v", err)
	}
	if fb.Completed != 3 || fb.NoShows != 1 {
		t.Fatalf("feedback = %+v; want 3 concluded, 1 no-show", fb)
	}
	// The pattern, plainly: price twice, size once (#141).
	if fb.Objections["price"] != 2 || fb.Objections["size"] != 1 {
		t.Fatalf("objections = %v; want price:2 size:1", fb.Objections)
	}
}

func TestSlotClashIsRefusedByName(t *testing.T) {
	f := newInspectionFixture(t)
	at := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if _, err := f.inspections.PublishSlot(owner(), f.listing, store.SlotDraft{StartsAt: at}); err != nil {
		t.Fatalf("first slot: %v", err)
	}
	if _, err := f.inspections.PublishSlot(owner(), f.listing, store.SlotDraft{StartsAt: at}); !errors.Is(err, store.ErrSlotClash) {
		t.Fatalf("clashing slot = %v, want ErrSlotClash", err)
	}
}
