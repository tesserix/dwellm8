// Package service is the discovery module's public Go interface — the seam
// every other module and every handler reaches it through. ADR-0001 §3.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

// ErrOccupied is a listing publication refused because somebody lives there:
// the advert would describe a flat that is not available (#135). The message
// names the tenancy.
var ErrOccupied = errors.New("listing: the unit is occupied")

// Occupancy answers whether a unit is let on a date. The lease module
// satisfies it; declared here so the dependency points at an interface in this
// module's own terms.
type Occupancy interface {
	ActiveOnUnit(ctx context.Context, unitID string, on effective.Date) (leaseID string, err error)
}

// Listings publishes adverts generated from live inventory.
type Listings struct {
	store     *store.Listings
	public    *store.Public
	occupancy Occupancy
	now       func() effective.Date
	log       *slog.Logger
}

// NewListings wires the service to its stores. now is the product clock — IST,
// the zone the availability dates are in (routine.Today at wiring).
func NewListings(s *store.Listings, p *store.Public, now func() effective.Date, log *slog.Logger) *Listings {
	return &Listings{store: s, public: p, now: now, log: log}
}

// WithOccupancy teaches the service to refuse advertising an occupied unit.
// Optional the way every cross-module edge is: nil means the guard is the
// activation-time no-double-let alone.
func (s *Listings) WithOccupancy(o Occupancy) *Listings {
	s.occupancy = o
	return s
}

// Create writes a draft from a unit already in the system — pre-filled by the
// caller from inventory, never re-keyed into a second system.
func (s *Listings) Create(ctx context.Context, d domain.Draft) (string, error) {
	id, err := s.store.Create(ctx, d)
	if err != nil {
		return "", err
	}
	s.log.Info("listing drafted", "listing", id, "unit", d.UnitID)
	return id, nil
}

// Publish takes a draft live. Two gates before the database's own: the
// disclosure rule (#134) and the occupancy rule (#135) — a listing for a unit
// with an active tenancy is blocked naming the tenancy, unless the
// availability date is after that tenancy ends.
func (s *Listings) Publish(ctx context.Context, id string, actor events.Actor) error {
	l, err := s.store.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := draftOf(l).PublishableNow(); err != nil {
		return err
	}

	if s.occupancy != nil {
		on := l.AvailableFrom
		if on.Zero() {
			on = s.now()
		}
		leaseID, err := s.occupancy.ActiveOnUnit(ctx, l.UnitID, on)
		if err != nil {
			return fmt.Errorf("checking occupancy: %w", err)
		}
		if leaseID != "" {
			return fmt.Errorf("%w: tenancy %s occupies it on %s — set available_from after it ends",
				ErrOccupied, leaseID, on)
		}
	}

	if err := s.store.Move(ctx, id, domain.StateLive, actor); err != nil {
		return err
	}
	s.log.Info("listing published", "listing", id, "unit", l.UnitID)
	return nil
}

// Pause takes a live listing off the market, keeping its publication history.
func (s *Listings) Pause(ctx context.Context, id string, actor events.Actor) error {
	return s.store.Move(ctx, id, domain.StatePaused, actor)
}

// Resume puts a paused listing back.
func (s *Listings) Resume(ctx context.Context, id string, actor events.Actor) error {
	return s.store.Move(ctx, id, domain.StateLive, actor)
}

// Withdraw ends the advertisement. The row stays: what was advertised at what
// rent is the record a misrepresentation complaint turns on.
func (s *Listings) Withdraw(ctx context.Context, id string, actor events.Actor) error {
	return s.store.Move(ctx, id, domain.StateWithdrawn, actor)
}

// Get is the owner's view of one listing.
func (s *Listings) Get(ctx context.Context, id string) (store.Listing, error) {
	return s.store.Get(ctx, id)
}

// List is the owner's portfolio of adverts.
func (s *Listings) List(ctx context.Context, state string) ([]store.Listing, error) {
	return s.store.List(ctx, state)
}

// Search is the anonymous front door (#133).
func (s *Listings) Search(ctx context.Context, q store.Query) ([]store.Card, error) {
	return s.public.Search(ctx, q)
}

// Detail is the anonymous listing page with the true cost stated (#134).
func (s *Listings) Detail(ctx context.Context, id string) (store.Card, error) {
	return s.public.Detail(ctx, id)
}

// draftOf rebuilds the domain draft from the stored row, so publication runs
// the same rules a creation did — one definition, not two.
func draftOf(l store.Listing) domain.Draft {
	return domain.Draft{
		PropertyID: l.PropertyID, UnitID: l.UnitID, UnitParentID: l.UnitParentID,
		Headline: l.Headline, Locality: l.Locality, City: l.City, StateCode: l.StateCode,
		Costs: l.Costs, Bedrooms: l.Bedrooms, CarpetAreaSqft: l.CarpetAreaSqft,
		AvailableFrom: l.AvailableFrom,
		ListerKind:    l.ListerKind, AgentRegistration: l.AgentRegistration,
	}
}
