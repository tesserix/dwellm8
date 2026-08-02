package service

import (
	"context"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/store"
)

// Tickets is the maintenance module's ticket service — the seam the resident
// surface (and later the ops surface) reaches tickets through.
type Tickets struct {
	store *store.Tickets
	log   *slog.Logger
}

// NewTickets builds the service.
func NewTickets(s *store.Tickets, log *slog.Logger) *Tickets {
	return &Tickets{store: s, log: log}
}

// Re-exported so a caller does not import the domain to read a response.
type (
	Ticket         = domain.Ticket
	TicketEvent    = domain.TicketEvent
	TicketCategory = domain.TicketCategory
	TicketAction   = domain.TicketAction
	TicketPatch    = domain.TicketPatch
)

// Errors a caller distinguishes.
var (
	ErrTicket   = domain.ErrTicket
	ErrNoTicket = store.ErrNoTicket
)

// Raise validates and writes a new ticket with its opening timeline line.
func (s *Tickets) Raise(ctx context.Context, t Ticket) (Ticket, error) {
	if err := t.ValidateRaise(); err != nil {
		return Ticket{}, err
	}
	opening := "Reported: " + t.Title
	out, err := s.store.Raise(ctx, t, opening)
	if err != nil {
		return Ticket{}, err
	}
	s.log.Info("ticket raised", "ticket", out.ID, "lease", out.LeaseID, "category", out.Category)
	return out, nil
}

// ForLease lists one tenancy's tickets, newest first.
func (s *Tickets) ForLease(ctx context.Context, leaseID string, limit int) ([]Ticket, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.store.ForLease(ctx, leaseID, limit)
}

// Read returns one ticket with its timeline.
func (s *Tickets) Read(ctx context.Context, id string) (Ticket, error) {
	if id == "" {
		return Ticket{}, ErrNoTicket
	}
	return s.store.Read(ctx, id)
}

// ForOrg lists the organisation's tickets — the manager's worklist (#237).
func (s *Tickets) ForOrg(ctx context.Context, settled bool, limit int) ([]Ticket, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	return s.store.ForOrg(ctx, settled, limit)
}

// Advance applies one manager action: the domain judges the move, the store
// writes it with its timeline line, and the answer carries the timeline.
func (s *Tickets) Advance(ctx context.Context, id string, a TicketAction, p TicketPatch) (Ticket, error) {
	t, err := s.store.Read(ctx, id)
	if err != nil {
		return Ticket{}, err
	}
	next, line, err := t.Advance(a, p)
	if err != nil {
		return Ticket{}, err
	}
	if _, err := s.store.Apply(ctx, next, line); err != nil {
		return Ticket{}, err
	}
	s.log.Info("ticket advanced", "ticket", id, "action", a, "status", next.Status)
	return s.store.Read(ctx, id)
}
