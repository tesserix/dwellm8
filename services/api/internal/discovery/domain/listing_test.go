package domain

import (
	"errors"
	"testing"
)

func validDraft() Draft {
	return Draft{
		PropertyID: "p-1", UnitID: "u-1",
		Headline: "2BHK near the lake", Locality: "HSR Layout", City: "Bengaluru", StateCode: "KA",
		Bedrooms: 2,
		Costs: Costs{
			RentMinor: 30_000_00, MaintenanceMinor: 3_000_00, DepositMinor: 60_000_00,
			Confirmed: true,
		},
	}
}

func TestDraftValidation(t *testing.T) {
	if err := validDraft().Validate(); err != nil {
		t.Fatalf("a valid draft refused: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*Draft)
	}{
		{"no unit", func(d *Draft) { d.UnitID = "" }},
		{"no headline", func(d *Draft) { d.Headline = "" }},
		{"no locality", func(d *Draft) { d.Locality = "" }},
		{"bad state code", func(d *Draft) { d.StateCode = "KAR" }},
		{"zero rent", func(d *Draft) { d.Costs.RentMinor = 0 }},
		{"negative maintenance", func(d *Draft) { d.Costs.MaintenanceMinor = -1 }},
		{"absurd bedrooms", func(d *Draft) { d.Bedrooms = 21 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := validDraft()
			tc.mutate(&d)
			if err := d.Validate(); !errors.Is(err, ErrDraft) {
				t.Fatalf("want ErrDraft, got %v", err)
			}
		})
	}
}

// #134: publication is blocked until every cost component is supplied or
// explicitly marked not applicable — a zero meaning "unknown" is the hidden
// charge the page promises not to have.
func TestPublicationRequiresConfirmedCosts(t *testing.T) {
	d := validDraft()
	d.Costs.Confirmed = false
	if err := d.PublishableNow(); !errors.Is(err, ErrDisclosure) {
		t.Fatalf("want ErrDisclosure, got %v", err)
	}
	d.Costs.Confirmed = true
	if err := d.PublishableNow(); err != nil {
		t.Fatalf("a confirmed draft refused publication: %v", err)
	}
}

func TestTotals(t *testing.T) {
	c := Costs{RentMinor: 100, MaintenanceMinor: 20, ParkingMinor: 5, OtherMonthlyMinor: 3,
		DepositMinor: 500, OneTimeMinor: 50}
	if got := c.TotalMonthlyMinor(); got != 128 {
		t.Fatalf("monthly total = %d, want 128", got)
	}
	if got := c.TotalOneTimeMinor(); got != 550 {
		t.Fatalf("one-time total = %d, want 550", got)
	}
}

func TestLifecycle(t *testing.T) {
	allowed := []struct {
		from, to State
		ev       Event
	}{
		{StateDraft, StateLive, EventPublished},
		{StateDraft, StateWithdrawn, EventWithdrawn},
		{StateLive, StatePaused, EventPaused},
		{StateLive, StateLet, EventLet},
		{StateLive, StateWithdrawn, EventWithdrawn},
		{StatePaused, StateLive, EventResumed},
		{StatePaused, StateLet, EventLet},
		{StatePaused, StateWithdrawn, EventWithdrawn},
		// #332: a tenancy that falls over before it starts puts the advert back
		// as itself. Republishing from scratch would lose the viewings and
		// outcomes the next appraisal is read from.
		{StateLet, StateLive, EventResumed},
	}
	for _, tc := range allowed {
		ev, err := Transition(tc.from, tc.to)
		if err != nil || ev != tc.ev {
			t.Fatalf("%s→%s = (%v, %v), want %s", tc.from, tc.to, ev, err, tc.ev)
		}
	}

	refused := []struct{ from, to State }{
		{StateDraft, StatePaused}, // pausing needs a publication to keep
		{StateDraft, StateLet},    // a draft was never advertised
		{StateLet, StatePaused},   // nothing to pause: it is off the market already
		{StateWithdrawn, StateLive},
		{StateLive, StateLive},
	}
	for _, tc := range refused {
		if _, err := Transition(tc.from, tc.to); !errors.Is(err, ErrTransition) {
			t.Fatalf("%s→%s allowed, want ErrTransition", tc.from, tc.to)
		}
	}
}
