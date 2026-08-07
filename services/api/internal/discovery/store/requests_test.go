package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

// Viewing requests (#331): the two asks that the published times do not cover.
// A private viewing at a time the prospect proposes, and a video walkthrough
// with a link instead of a place. Neither needs a slot to exist first — the
// "by appointment only" listing must not be a dead end.

func systemActor() events.Actor { return events.Actor{Kind: events.ActorSystem} }

func TestAPrivateRequestNeedsNoPublishedSlot(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	at := time.Now().Add(48 * time.Hour).Truncate(time.Second)

	r, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "Weekends only, I work nights", []time.Time{at})
	if err != nil {
		t.Fatalf("requesting a private viewing on a listing with no slots: %v", err)
	}
	if r.Enquiry.State != "new" || len(r.Proposals) != 1 {
		t.Fatalf("request = %+v; want one open proposed time on a new enquiry", r)
	}
	if r.Proposals[0].ProposedBy != "prospect" || !r.Proposals[0].StartsAt.Equal(at) {
		t.Fatalf("proposal = %+v; want the prospect's own time", r.Proposals[0])
	}
}

func TestAcceptingARequestMakesTheSlotAndTheBookingAtOnce(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	first := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	second := first.Add(2 * time.Hour)

	r, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", []time.Time{first, second})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}

	booked, err := f.inspections.AcceptRequest(owner(), r.Enquiry.ID, r.Proposals[1].ID,
		store.Confirmation{MeetingPoint: "Tower B gate", AssignedTo: "Meera"}, systemActor())
	if err != nil {
		t.Fatalf("accepting: %v", err)
	}
	if !booked.StartsAt.Equal(second) || booked.MeetingPoint == "" {
		t.Fatalf("confirmation = %+v; want the accepted time and the meeting point", booked)
	}
	if booked.Enquiry.State != "scheduled" {
		t.Fatalf("enquiry state = %q; want scheduled", booked.Enquiry.State)
	}

	// The slot exists with the prospect in it — never a place with nobody in it.
	slots, err := f.inspections.OwnerSlots(owner(), f.listing)
	if err != nil || len(slots) != 1 {
		t.Fatalf("slots = %d, %v; want the one the acceptance made", len(slots), err)
	}
	if slots[0].Capacity != 1 || slots[0].Booked != 1 {
		t.Fatalf("slot = %+v; want capacity 1, booked 1", slots[0])
	}

	// The time that was not taken is not still on offer, and the answered
	// request cannot be answered twice.
	after, err := f.inspections.ProspectRequests(context.Background(), p.ID)
	if err != nil || len(after) != 1 {
		t.Fatalf("prospect requests = %d, %v; want one", len(after), err)
	}
	for _, pr := range after[0].Proposals {
		if pr.State == "open" {
			t.Fatalf("proposal %s is still open after the request was answered", pr.ID)
		}
	}
	if _, err := f.inspections.AcceptRequest(owner(), r.Enquiry.ID, r.Proposals[0].ID,
		store.Confirmation{MeetingPoint: "Tower B gate"}, systemActor()); !errors.Is(err, store.ErrNotAnswerable) {
		t.Fatalf("second acceptance = %v; want ErrNotAnswerable", err)
	}
}

func TestAProposedTimeInThePastIsRefusedBySentence(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)

	_, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", []time.Time{time.Now().Add(-time.Hour)})
	if !errors.Is(err, store.ErrPastTime) {
		t.Fatalf("requesting a time in the past = %v; want ErrPastTime", err)
	}
	if _, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", nil); !errors.Is(err, store.ErrPastTime) {
		t.Fatalf("requesting with no time at all = %v; want a refusal", err)
	}
}

func TestAnOnlineRequestCarriesALinkAndNoCapacity(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	at := time.Now().Add(72 * time.Hour).Truncate(time.Second)

	r, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"online_inspection", "I am in Dubai until March", []time.Time{at})
	if err != nil {
		t.Fatalf("requesting an online viewing: %v", err)
	}

	// Before the answer the prospect holds no link: a room anybody could read
	// is a room anybody could join.
	pending, err := f.inspections.ProspectRequests(context.Background(), p.ID)
	if err != nil || len(pending) != 1 {
		t.Fatalf("prospect requests = %d, %v; want one", len(pending), err)
	}
	if pending[0].MeetingLink != "" {
		t.Fatalf("an unanswered online request already carries a link")
	}

	booked, err := f.inspections.AcceptRequest(owner(), r.Enquiry.ID, r.Proposals[0].ID,
		store.Confirmation{MeetingLink: "https://meet.example.com/abcd"}, systemActor())
	if err != nil {
		t.Fatalf("accepting the online request: %v", err)
	}
	if booked.MeetingLink != "https://meet.example.com/abcd" || booked.MeetingPoint != "" {
		t.Fatalf("confirmation = %+v; want a link and no place", booked)
	}
	// It occupies nothing at the property.
	slots, err := f.inspections.OwnerSlots(owner(), f.listing)
	if err != nil || len(slots) != 0 {
		t.Fatalf("slots = %d, %v; an online viewing took capacity at the property", len(slots), err)
	}

	confirmed, err := f.inspections.ProspectRequests(context.Background(), p.ID)
	if err != nil || confirmed[0].MeetingLink == "" {
		t.Fatalf("the confirmed request carries no link: %+v, %v", confirmed, err)
	}
}

func TestAProspectHoldsTwoOpenRequestsOnOneListing(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	at := time.Now().Add(96 * time.Hour).Truncate(time.Second)

	for _, kind := range []string{"inspection", "online_inspection"} {
		if _, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
			kind, "", []time.Time{at}); err != nil {
			t.Fatalf("requesting a %s: %v", kind, err)
		}
	}
	_, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", []time.Time{at.Add(time.Hour)})
	if !errors.Is(err, store.ErrTooManyRequests) {
		t.Fatalf("third open request = %v; want ErrTooManyRequests", err)
	}
}

func TestACounterIsOneRoundTripNotAThread(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	asked := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	offered := asked.Add(24 * time.Hour)

	r, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", []time.Time{asked})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}

	counter, err := f.inspections.CounterRequest(owner(), r.Enquiry.ID, offered,
		store.Confirmation{MeetingPoint: "Tower B gate"}, systemActor())
	if err != nil {
		t.Fatalf("countering: %v", err)
	}
	if counter.ProposedBy != "owner" || !counter.StartsAt.Equal(offered) {
		t.Fatalf("counter = %+v; want the owner's time", counter)
	}
	// The prospect weighing the counter sees the time, not the exact place.
	pending, err := f.inspections.ProspectRequests(context.Background(), p.ID)
	if err != nil || len(pending) != 1 || pending[0].MeetingPoint != "" {
		t.Fatalf("an open counter already discloses the meeting point: %+v, %v", pending, err)
	}

	booked, err := f.inspections.AcceptCounter(context.Background(), p.ID, r.Enquiry.ID, counter.ID)
	if err != nil {
		t.Fatalf("prospect accepting the counter: %v", err)
	}
	if !booked.StartsAt.Equal(offered) || booked.Enquiry.State != "scheduled" {
		t.Fatalf("booked = %+v; want the countered time, scheduled", booked)
	}
	if booked.MeetingPoint != "Tower B gate" {
		t.Fatalf("meeting point = %q; want the one the counter carried", booked.MeetingPoint)
	}
	// The round trip is over: no second counter on a settled request.
	if _, err := f.inspections.CounterRequest(owner(), r.Enquiry.ID, offered.Add(time.Hour),
		store.Confirmation{}, systemActor()); !errors.Is(err, store.ErrNotAnswerable) {
		t.Fatalf("second counter = %v; want ErrNotAnswerable", err)
	}
}

func TestADeclinedRequestSaysWhoDeclinedIt(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	at := time.Now().Add(48 * time.Hour).Truncate(time.Second)

	r, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", []time.Time{at})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}
	if err := f.inspections.DeclineRequest(owner(), r.Enquiry.ID, systemActor()); err != nil {
		t.Fatalf("declining: %v", err)
	}

	got, err := f.inspections.ProspectRequests(context.Background(), p.ID)
	if err != nil || len(got) != 1 {
		t.Fatalf("requests = %d, %v", len(got), err)
	}
	if got[0].Enquiry.State != "closed" || got[0].Outcome != "declined_by_owner" {
		t.Fatalf("declined request = %+v; want closed, declined_by_owner", got[0])
	}
	// A declined request no longer counts against the ceiling.
	if _, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", []time.Time{at.Add(time.Hour)}); err != nil {
		t.Fatalf("requesting again after a decline: %v", err)
	}
}

func TestRequestsStandInTheManagersQueue(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	at := time.Now().Add(48 * time.Hour).Truncate(time.Second)

	for _, kind := range []string{"inspection", "online_inspection"} {
		if _, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
			kind, "", []time.Time{at}); err != nil {
			t.Fatalf("requesting a %s: %v", kind, err)
		}
	}

	queue, err := f.inspections.OwnerRequests(owner())
	if err != nil {
		t.Fatalf("manager queue: %v", err)
	}
	kinds := map[string]bool{}
	for _, r := range queue {
		if r.Enquiry.ListingID == f.listing {
			kinds[r.Enquiry.Kind] = true
			if len(r.Proposals) == 0 {
				t.Fatalf("a request in the queue shows no proposed time: %+v", r)
			}
		}
	}
	if !kinds["inspection"] || !kinds["online_inspection"] {
		t.Fatalf("queue kinds = %v; want both requests", kinds)
	}
}

// A confirmed online viewing is an appointment somebody has to keep, so it
// stands in the day's list beside the ones at the property (#331).
func TestAConfirmedOnlineViewingIsInTheDay(t *testing.T) {
	f := newInspectionFixture(t)
	p := f.verifiedProspect(t)
	at := time.Now().Add(26 * time.Hour).Truncate(time.Second)

	r, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"online_inspection", "", []time.Time{at})
	if err != nil {
		t.Fatalf("requesting: %v", err)
	}
	if _, err := f.inspections.AcceptRequest(owner(), r.Enquiry.ID, r.Proposals[0].ID,
		store.Confirmation{MeetingLink: "https://meet.example.com/xyz"}, systemActor()); err != nil {
		t.Fatalf("accepting: %v", err)
	}

	day, err := f.inspections.DayView(owner(), at.Truncate(24*time.Hour))
	if err != nil {
		t.Fatalf("day view: %v", err)
	}
	found := false
	for _, b := range day {
		found = found || b.Enquiry.ID == r.Enquiry.ID
	}
	if !found {
		t.Fatalf("the online viewing is missing from the day's list of %d", len(day))
	}

	// And it concludes like any other viewing — otherwise it stays scheduled
	// for ever.
	if err := f.inspections.RecordOutcome(owner(), r.Enquiry.ID,
		store.Outcome{Outcome: "interested"}, systemActor()); err != nil {
		t.Fatalf("recording the outcome of an online viewing: %v", err)
	}
}

func TestAnUnverifiedProspectCannotRequestAViewing(t *testing.T) {
	f := newInspectionFixture(t)
	_, hash := token(t)
	p, err := f.prospects.Ensure(context.Background(), hash)
	if err != nil {
		t.Fatalf("prospect: %v", err)
	}
	if _, err := f.inspections.RequestViewing(context.Background(), f.listing, p.ID,
		"inspection", "", []time.Time{time.Now().Add(48 * time.Hour)}); !errors.Is(err, store.ErrNotVerified) {
		t.Fatalf("unverified request = %v; want ErrNotVerified", err)
	}
}
