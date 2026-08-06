// Package service is the lease module's public Go interface — the seam every
// other module reaches it through, and the only thing a handler is allowed to
// call. ADR-0001 §3.
package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Leases creates and starts tenancies.
type Leases struct {
	store *store.Leases
	log   *slog.Logger

	// residents resolves a mobile number to the party it identifies. Optional:
	// nil means every party must arrive with an id, which is how this module
	// worked before the tenant surface existed. See WithResidents.
	residents Residents

	// checklists says what blocking work stands in front of closing a tenancy, and
	// billing says how far it has been invoiced. Both optional, and both refused
	// again by the schema when they are absent — see terminate.go.
	checklists Checklists
	billing    Billing
}

// NewLeases wires the service to its store.
func NewLeases(s *store.Leases, log *slog.Logger) *Leases {
	return &Leases{store: s, log: log}
}

// Created is a lease and the event its creation published.
//
// The event is returned rather than published: ADR-0002's outbox does not exist
// yet, and a service that logged an event and called it published would be worse
// than one that admits it hands the event back. When the outbox lands, this is
// where it is written — inside the same transaction as the lease.
type Created struct {
	ID    string
	Lease domain.Lease
	Terms domain.Terms
	Event domain.Event
}

// Create writes a lease in draft.
//
// Draft, not active, however complete the request is. Two competing offers on one
// flat are legitimate and a tenancy is not, so becoming one is a second, checked
// call.
func (s *Leases) Create(ctx context.Context, d domain.Draft) (Created, error) {
	d, err := s.resolveParties(ctx, d)
	if err != nil {
		return Created{}, err
	}
	out, err := s.store.Create(ctx, d)
	if err != nil {
		return Created{}, err
	}
	s.log.Info("lease created",
		"lease", out.ID, "unit", d.Unit, "from", d.Term.From(), "event", out.Event)
	return Created{ID: out.ID, Lease: out.Lease, Terms: out.Terms, Event: out.Event}, nil
}

// Activate starts the tenancy.
//
// Where the two guards the database owns actually fire — the no-double-let
// exclusion and ADR-0024's deferred tax-path trigger. The store maps both into
// errors a caller can tell apart, and this passes them through unchanged rather
// than flattening them into "could not activate".
// Offer sends a draft out for signature — the step between writing a lease and
// starting the tenancy it describes.
func (s *Leases) Offer(ctx context.Context, id string, by domain.Actor) error {
	return s.store.Offer(ctx, id, by)
}

func (s *Leases) Activate(ctx context.Context, id string, by domain.Actor) error {
	if err := s.store.Activate(ctx, id, by); err != nil {
		return err
	}
	s.log.Info("tenancy started", "lease", id, "by", by, "event", domain.EventStarted)
	return nil
}

// Schedule returns the charges a tenancy owes to a horizon.
//
// The rent is read as of the tenancy's start rather than taken from the caller:
// a schedule computed from an amount the caller supplied is a schedule that
// agrees with whatever the caller believed.
func (s *Leases) Schedule(ctx context.Context, l domain.Lease, through effective.Date) ([]domain.Period, error) {
	terms, err := s.store.Terms(ctx, l.ID, l.Term.From())
	if err != nil {
		return nil, fmt.Errorf("lease %s: %w", l.ID, err)
	}
	return l.Schedule(terms, through)
}
