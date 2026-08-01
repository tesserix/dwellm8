package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

// ErrBadOutcome names the closed vocabulary, so the refusal teaches the API.
var ErrBadOutcome = errors.New(
	"inspection: outcome must be interested, not_interested, applied, prospect_no_show or staff_no_show")

// ErrBadObjection is an objection outside the structured set (#141) — free
// text goes in the note, where it cannot pollute the aggregate.
var ErrBadObjection = errors.New(
	"inspection: objections must be among price, condition, locality, size, terms")

var outcomes = map[string]bool{
	"interested": true, "not_interested": true, "applied": true,
	"prospect_no_show": true, "staff_no_show": true,
}

var objections = map[string]bool{
	"price": true, "condition": true, "locality": true, "size": true, "terms": true,
}

// Inspections runs the viewing loop: slots, bookings, changes and outcomes.
type Inspections struct {
	store     *store.Inspections
	prospects *store.Prospects
	log       *slog.Logger
}

// NewInspections wires the service.
func NewInspections(s *store.Inspections, p *store.Prospects, log *slog.Logger) *Inspections {
	return &Inspections{store: s, prospects: p, log: log}
}

// PublishSlot offers a viewing time (owner side).
func (s *Inspections) PublishSlot(ctx context.Context, listingID string, d store.SlotDraft) (string, error) {
	if d.StartsAt.IsZero() || d.StartsAt.Before(time.Now()) {
		return "", fmt.Errorf("%w: starts_at must be in the future", store.ErrNoSlot)
	}
	id, err := s.store.PublishSlot(ctx, listingID, d)
	if err != nil {
		return "", err
	}
	s.log.Info("viewing slot published", "slot", id, "listing", listingID, "at", d.StartsAt)
	return id, nil
}

// CloseSlot stops new bookings on a slot.
func (s *Inspections) CloseSlot(ctx context.Context, slotID string) error {
	return s.store.CloseSlot(ctx, slotID)
}

// OwnerSlots is the owner's view of a listing's calendar.
func (s *Inspections) OwnerSlots(ctx context.Context, listingID string) ([]store.Slot, error) {
	return s.store.OwnerSlots(ctx, listingID)
}

// OpenSlots is what a prospect chooses from. No token needed: the times a flat
// can be seen are as public as the flat's advert.
func (s *Inspections) OpenSlots(ctx context.Context, listingID string) ([]store.Slot, error) {
	return s.store.OpenSlots(ctx, listingID)
}

// Book holds a place for the token's verified prospect. On a full slot the
// alternatives come back with the refusal.
func (s *Inspections) Book(ctx context.Context, token, listingID, slotID string) (store.Booked, []store.Slot, error) {
	p, err := s.resolve(ctx, token)
	if err != nil {
		return store.Booked{}, nil, err
	}
	if !p.Verified {
		return store.Booked{}, nil, store.ErrNotVerified
	}
	return s.store.Book(ctx, listingID, p.ID, slotID)
}

// Reschedule moves the token's booking to another slot.
func (s *Inspections) Reschedule(ctx context.Context, token, enquiryID, slotID string) (store.Booked, []store.Slot, error) {
	p, err := s.resolve(ctx, token)
	if err != nil {
		return store.Booked{}, nil, err
	}
	return s.store.Reschedule(ctx, p.ID, enquiryID, slotID)
}

// CancelByProspect releases the token's booking.
func (s *Inspections) CancelByProspect(ctx context.Context, token, enquiryID string) error {
	p, err := s.resolve(ctx, token)
	if err != nil {
		return err
	}
	return s.store.CancelByProspect(ctx, p.ID, enquiryID)
}

// CancelByOwner releases a booking from the owner's side.
func (s *Inspections) CancelByOwner(ctx context.Context, enquiryID string) error {
	return s.store.CancelByOwner(ctx, enquiryID)
}

// RecordOutcome concludes a viewing (#141): under a minute on a phone, so the
// vocabulary is validated here and the message names what is allowed.
func (s *Inspections) RecordOutcome(ctx context.Context, enquiryID string, o store.Outcome,
	actor events.Actor) error {
	if !outcomes[o.Outcome] {
		return ErrBadOutcome
	}
	for _, obj := range o.Objections {
		if !objections[obj] {
			return ErrBadObjection
		}
	}
	if err := s.store.RecordOutcome(ctx, enquiryID, o, actor); err != nil {
		return err
	}
	s.log.Info("viewing concluded", "enquiry", enquiryID, "outcome", o.Outcome)
	return nil
}

// ListingFeedback is the owner's aggregate: the pattern, never the prospect.
func (s *Inspections) ListingFeedback(ctx context.Context, listingID string) (store.Feedback, error) {
	return s.store.ListingFeedback(ctx, listingID)
}

// DayView is the schedule for one day, ordered.
func (s *Inspections) DayView(ctx context.Context, on time.Time) ([]store.Booked, error) {
	return s.store.DayView(ctx, on)
}

func (s *Inspections) resolve(ctx context.Context, token string) (store.Prospect, error) {
	p, err := s.prospects.ByToken(ctx, hash(token))
	if errors.Is(err, store.ErrNoProspect) {
		return store.Prospect{}, ErrBadToken
	}
	return p, err
}
