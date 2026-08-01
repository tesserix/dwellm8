// Package domain holds the discovery module's aggregates and rules.
//
// A listing is an advertisement generated from a real unit (ADR-0019). It can
// say nothing the operating system does not know: the unit exists, the rent is
// the asked rent, and the moment the unit is let the advertisement dies. The
// rules here are the ones a database constraint cannot carry — what a listing
// must disclose before it may be published, and which state moves are moves.
package domain

import (
	"errors"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// State is where a listing is in its life.
type State string

const (
	// StateDraft is written but invisible to strangers.
	StateDraft State = "draft"
	// StateLive is published and searchable.
	StateLive State = "live"
	// StatePaused is temporarily off the market, keeping its publication
	// history — which is what makes it different from withdrawn.
	StatePaused State = "paused"
	// StateLet is the terminal success: the unit found its tenant. Advertising
	// it again is a new listing, so the record of what was advertised at what
	// rent survives.
	StateLet State = "let"
	// StateWithdrawn is the terminal abandonment.
	StateWithdrawn State = "withdrawn"
)

// Event names the facts this module publishes. ADR-0002 §2.
type Event string

const (
	EventPublished Event = "discovery.listing.published"
	EventPaused    Event = "discovery.listing.paused"
	EventResumed   Event = "discovery.listing.resumed"
	EventWithdrawn Event = "discovery.listing.withdrawn"
	EventLet       Event = "discovery.listing.let"

	EventEnquiryReceived  Event = "discovery.enquiry.received"
	EventEnquiryResponded Event = "discovery.enquiry.responded"
	EventConverted        Event = "discovery.prospect.converted"
)

var (
	// ErrDraft reports a draft that cannot be a listing at all.
	ErrDraft = errors.New("listing: invalid draft")
	// ErrDisclosure reports a listing that hides a cost, which is the one thing
	// this surface exists not to do (#134).
	ErrDisclosure = errors.New("listing: incomplete cost disclosure")
	// ErrTransition reports a state move the lifecycle does not have.
	ErrTransition = errors.New("listing: invalid transition")
)

// Costs is the true monthly and one-time outlay, itemised. Every amount is
// minor units (ADR-0007); zero is a real answer meaning "nothing", and the
// Confirmed flag is what separates "zero" from "nobody filled this in".
type Costs struct {
	RentMinor         int64
	MaintenanceMinor  int64
	ParkingMinor      int64
	OtherMonthlyMinor int64
	DepositMinor      int64
	OneTimeMinor      int64
	// Confirmed is the publisher's statement that the components above are
	// complete — supplied, or genuinely not applicable. Publication is blocked
	// without it rather than defaulting the gaps to zero, because a zero that
	// means "unknown" is exactly the hidden charge the page promises not to have.
	Confirmed bool
}

// TotalMonthlyMinor is the number the prospect actually pays each month.
func (c Costs) TotalMonthlyMinor() int64 {
	return c.RentMinor + c.MaintenanceMinor + c.ParkingMinor + c.OtherMonthlyMinor
}

// TotalOneTimeMinor is what moving in costs beyond the months.
func (c Costs) TotalOneTimeMinor() int64 {
	return c.DepositMinor + c.OneTimeMinor
}

// Draft is what an owner supplies to advertise a unit. Property and unit are
// the live inventory the listing is generated from — never re-keyed data.
type Draft struct {
	PropertyID   string
	UnitID       string
	UnitParentID string

	Headline  string
	Locality  string
	City      string
	StateCode string

	Costs          Costs
	Bedrooms       int
	CarpetAreaSqft float64
	AvailableFrom  effective.Date
}

// Validate refuses what cannot be a listing, naming the field rather than the
// form: "invalid request" is not a thing anybody can fix.
func (d Draft) Validate() error {
	switch {
	case d.PropertyID == "" || d.UnitID == "":
		return fmt.Errorf("%w: a listing is generated from a unit, so property_id and unit_id are required", ErrDraft)
	case d.Headline == "":
		return fmt.Errorf("%w: headline is required", ErrDraft)
	case d.Locality == "" || d.City == "":
		return fmt.Errorf("%w: locality and city are required — a listing shows a locality, not an address", ErrDraft)
	case len(d.StateCode) != 2:
		return fmt.Errorf("%w: state_code is the two-letter state, e.g. KA", ErrDraft)
	case d.Costs.RentMinor <= 0:
		return fmt.Errorf("%w: rent_minor must be positive", ErrDraft)
	case d.Costs.MaintenanceMinor < 0 || d.Costs.ParkingMinor < 0 ||
		d.Costs.OtherMonthlyMinor < 0 || d.Costs.DepositMinor < 0 || d.Costs.OneTimeMinor < 0:
		return fmt.Errorf("%w: no cost component may be negative", ErrDraft)
	case d.Bedrooms < 0 || d.Bedrooms > 20:
		return fmt.Errorf("%w: bedrooms must be between 0 and 20", ErrDraft)
	case d.CarpetAreaSqft < 0:
		return fmt.Errorf("%w: carpet_area_sqft may not be negative", ErrDraft)
	}
	return nil
}

// PublishableNow reports whether the listing may go live. Validation says it
// is a listing; this says it is an honest one (#134's gate).
func (d Draft) PublishableNow() error {
	if err := d.Validate(); err != nil {
		return err
	}
	if !d.Costs.Confirmed {
		return fmt.Errorf("%w: every cost component must be supplied or explicitly marked "+
			"not applicable before publication — set costs.confirmed", ErrDisclosure)
	}
	return nil
}

// transitions is the lifecycle. A move not listed here is not a move.
var transitions = map[State]map[State]Event{
	StateDraft: {
		StateLive:      EventPublished,
		StateWithdrawn: EventWithdrawn,
	},
	StateLive: {
		StatePaused:    EventPaused,
		StateLet:       EventLet,
		StateWithdrawn: EventWithdrawn,
	},
	StatePaused: {
		StateLive:      EventResumed,
		StateLet:       EventLet,
		StateWithdrawn: EventWithdrawn,
	},
}

// Transition returns the event a move publishes, or refuses the move. Both
// terminal states refuse everything: a let listing is history, not inventory.
func Transition(from, to State) (Event, error) {
	ev, ok := transitions[from][to]
	if !ok {
		return "", fmt.Errorf("%w: a %s listing cannot become %s", ErrTransition, from, to)
	}
	return ev, nil
}
