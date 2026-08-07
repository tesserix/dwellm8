package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

// A recurring viewing time against PostgreSQL (#330): the pattern materialises
// bookable slots, materialising twice changes nothing, and an occurrence the
// manager moved or cancelled is never put back by the next run.

func nextSaturday() time.Time {
	d := time.Now().AddDate(0, 0, 1)
	for d.Weekday() != time.Saturday {
		d = d.AddDate(0, 0, 1)
	}
	return time.Date(d.Year(), d.Month(), d.Day(), 0, 0, 0, 0, time.UTC)
}

func saturdays(from time.Time) store.ScheduleDraft {
	return store.ScheduleDraft{
		Weekdays: []int{int(time.Saturday)}, StartTime: "10:00", Zone: "Asia/Kolkata",
		DurationMins: 30, Capacity: 4, MeetingPoint: "Under the clock", StartsOn: from,
	}
}

func materialised(t *testing.T, f inspectionFixture, weeks int) (string, []store.Slot) {
	t.Helper()
	from := nextSaturday()
	id, err := f.inspections.CreateSchedule(owner(), f.listing, saturdays(from))
	if err != nil {
		t.Fatalf("creating the schedule: %v", err)
	}
	if _, err := f.inspections.Materialise(owner(), id, from.AddDate(0, 0, 7*weeks-1)); err != nil {
		t.Fatalf("materialising: %v", err)
	}
	slots, err := f.inspections.OwnerSlots(owner(), f.listing)
	if err != nil {
		t.Fatalf("reading slots: %v", err)
	}
	return id, slots
}

func TestASeriesBecomesBookableSlots(t *testing.T) {
	f := newInspectionFixture(t)
	_, slots := materialised(t, f, 4)

	if len(slots) != 4 {
		t.Fatalf("got %d slots, want 4", len(slots))
	}
	for _, s := range slots {
		if s.Capacity != 4 || s.MeetingPoint != "Under the clock" {
			t.Errorf("slot %s did not take the series' shape: %+v", s.ID, s)
		}
		if s.StartsAt.In(time.UTC).Format("15:04") != "04:30" { // 10:00 IST
			t.Errorf("slot at %s is not the series' local ten o'clock", s.StartsAt)
		}
		// The clock the viewing happens on. Without it the manager's phone
		// renders the instant in whatever zone the manager is standing in (#334).
		if s.Zone != "Asia/Kolkata" {
			t.Errorf("slot %s came back in zone %q, want the series'", s.ID, s.Zone)
		}
	}
}

// The horizon job re-runs; a second pass must not raise, duplicate, or double
// the manager's calendar.
func TestMaterialisingTwiceChangesNothing(t *testing.T) {
	f := newInspectionFixture(t)
	id, first := materialised(t, f, 4)

	added, err := f.inspections.Materialise(owner(), id, nextSaturday().AddDate(0, 0, 27))
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if added != 0 {
		t.Errorf("the second pass added %d slots", added)
	}
	after, _ := f.inspections.OwnerSlots(owner(), f.listing)
	if len(after) != len(first) {
		t.Fatalf("slots went from %d to %d on a repeat run", len(first), len(after))
	}
}

func TestACancelledOccurrenceStaysCancelled(t *testing.T) {
	f := newInspectionFixture(t)
	id, slots := materialised(t, f, 4)

	if err := f.inspections.CancelOccurrence(owner(), slots[1].ID); err != nil {
		t.Fatalf("cancelling one: %v", err)
	}
	if _, err := f.inspections.Materialise(owner(), id, nextSaturday().AddDate(0, 0, 27)); err != nil {
		t.Fatalf("re-materialising: %v", err)
	}

	after, _ := f.inspections.OwnerSlots(owner(), f.listing)
	if len(after) != 4 {
		t.Fatalf("the cancelled occurrence was replaced: %d slots", len(after))
	}
	if after[1].State != "cancelled" {
		t.Errorf("occurrence 2 is %s, want cancelled", after[1].State)
	}
}

func TestAMovedOccurrenceKeepsItsNewTime(t *testing.T) {
	f := newInspectionFixture(t)
	id, slots := materialised(t, f, 4)
	moved := slots[2].StartsAt.Add(2 * time.Hour)

	if err := f.inspections.MoveOccurrence(owner(), slots[2].ID, moved); err != nil {
		t.Fatalf("moving one: %v", err)
	}
	if _, err := f.inspections.Materialise(owner(), id, nextSaturday().AddDate(0, 0, 27)); err != nil {
		t.Fatalf("re-materialising: %v", err)
	}

	after, _ := f.inspections.OwnerSlots(owner(), f.listing)
	if len(after) != 4 {
		t.Fatalf("moving an occurrence left %d slots, want 4", len(after))
	}
	if !after[2].StartsAt.Equal(moved) {
		t.Errorf("occurrence 3 is at %s, want %s", after[2].StartsAt, moved)
	}
}

// Ending a series is administration. A prospect holding a confirmed time is
// not thrown out by it — that takes a cancellation, by name, with notice.
func TestEndingASeriesLeavesBookedTimesStanding(t *testing.T) {
	f := newInspectionFixture(t)
	id, slots := materialised(t, f, 4)
	p := f.verifiedProspect(t)
	if _, _, err := f.inspections.Book(owner(), f.listing, p.ID, slots[2].ID); err != nil {
		t.Fatalf("booking: %v", err)
	}

	if err := f.inspections.EndSeries(owner(), id, time.Now()); err != nil {
		t.Fatalf("ending the series: %v", err)
	}

	after, _ := f.inspections.OwnerSlots(owner(), f.listing)
	for i, s := range after {
		want := "closed"
		if i == 2 {
			want = "open"
		}
		if s.State != want {
			t.Errorf("slot %d is %s, want %s", i, s.State, want)
		}
	}
	if _, err := f.inspections.Materialise(owner(), id, nextSaturday().AddDate(0, 0, 27)); err != nil {
		t.Fatalf("re-materialising an ended series: %v", err)
	}
	if again, _ := f.inspections.OwnerSlots(owner(), f.listing); len(again) != 4 {
		t.Fatalf("an ended series produced %d slots", len(again))
	}
}

// Changing the series from a date forward is the calendar's "this and all
// after it": everything before that date is exactly as it was.
func TestAmendingFromADateLeavesTheEarlierTimesAlone(t *testing.T) {
	f := newInspectionFixture(t)
	id, slots := materialised(t, f, 4)
	from := slots[2].StartsAt

	next := saturdays(from)
	next.StartTime = "14:00"
	newID, err := f.inspections.AmendSchedule(owner(), id, next, from)
	if err != nil {
		t.Fatalf("amending: %v", err)
	}
	if newID == id {
		t.Fatal("amending must leave the old series as the record of what was advertised")
	}
	if _, err := f.inspections.Materialise(owner(), newID, from.AddDate(0, 0, 7)); err != nil {
		t.Fatalf("materialising the amendment: %v", err)
	}

	all, _ := f.inspections.OwnerSlots(owner(), f.listing)
	var open []store.Slot
	for _, s := range all {
		if s.State == "open" {
			open = append(open, s)
		}
	}
	if len(open) != 4 {
		t.Fatalf("got %d bookable slots, want the two before plus the two after: 4", len(open))
	}
	for i, want := range []string{"04:30", "04:30", "08:30", "08:30"} { // 10:00 then 14:00 IST
		if got := open[i].StartsAt.In(time.UTC).Format("15:04"); got != want {
			t.Errorf("bookable slot %d is at %s UTC, want %s", i, got, want)
		}
	}
}

func TestAScheduleIsRefusedOnAListingThatIsNotOurs(t *testing.T) {
	f := newInspectionFixture(t)
	_, err := f.inspections.CreateSchedule(owner(), "00000000-0000-0000-0000-000000000000", saturdays(nextSaturday()))
	if !errors.Is(err, store.ErrNoListing) {
		t.Fatalf("got %v, want ErrNoListing", err)
	}
}

func TestTheSeriesReadsBackAsTheManagerStatedIt(t *testing.T) {
	f := newInspectionFixture(t)
	id, _ := materialised(t, f, 2)

	got, err := f.inspections.Schedules(owner(), f.listing)
	if err != nil {
		t.Fatalf("reading the schedules: %v", err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("got %d schedules, want the one just created", len(got))
	}
	if got[0].StartTime != "10:00" || got[0].Zone != "Asia/Kolkata" ||
		len(got[0].Weekdays) != 1 || got[0].Weekdays[0] != int(time.Saturday) {
		t.Errorf("the series came back as %+v", got[0])
	}
}

// A let or withdrawn listing is finished. Viewing times on it would be a
// phantom: a prospect can never book them, and a manager would still see them.
func TestAFinishedListingTakesNoMoreViewingTimes(t *testing.T) {
	f := newInspectionFixture(t)
	if err := f.listings.Move(owner(), f.listing, domain.StateWithdrawn,
		events.Actor{Kind: events.ActorSystem}); err != nil {
		t.Fatalf("withdrawing: %v", err)
	}

	_, err := f.inspections.CreateSchedule(owner(), f.listing, saturdays(nextSaturday()))
	if !errors.Is(err, store.ErrListingNotLive) {
		t.Fatalf("got %v, want ErrListingNotLive", err)
	}
}
