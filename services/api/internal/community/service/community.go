// Package service is the community module's public interface — the seam
// ADR-0001 §3 requires, and the only way another module or surface reaches
// the tenancy conversation or the gate.
package service

import (
	"context"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/community/domain"
	"github.com/tesserix/dwellm8/services/api/internal/community/store"
)

// Community is the module's service.
type Community struct {
	store *store.Community
	log   *slog.Logger
}

// New builds the service.
func New(s *store.Community, log *slog.Logger) *Community {
	return &Community{store: s, log: log}
}

// Re-exported so a caller does not import the domain to read a response.
type (
	Message  = domain.Message
	Pass     = domain.Pass
	PassKind = domain.PassKind
)

// Errors a caller distinguishes.
var (
	ErrMessage = domain.ErrMessage
	ErrPass    = domain.ErrPass
	ErrNoPass  = store.ErrNoPass
)

// Send validates and appends one message to the lease's thread.
func (s *Community) Send(ctx context.Context, m Message) (Message, error) {
	if err := m.ValidateSend(); err != nil {
		return Message{}, err
	}
	return s.store.Send(ctx, m)
}

// Thread reads a lease's conversation, oldest first.
func (s *Community) Thread(ctx context.Context, leaseID string, limit int) ([]Message, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	return s.store.Thread(ctx, leaseID, limit)
}

// CreatePass validates and records one expected visitor.
func (s *Community) CreatePass(ctx context.Context, p Pass) (Pass, error) {
	if err := p.ValidateCreate(); err != nil {
		return Pass{}, err
	}
	out, err := s.store.CreatePass(ctx, p)
	if err != nil {
		return Pass{}, err
	}
	s.log.Info("gate pass created", "pass", out.ID, "lease", out.LeaseID, "kind", out.Kind)
	return out, nil
}

// Passes lists one tenancy's passes, newest first.
func (s *Community) Passes(ctx context.Context, leaseID string, limit int) ([]Pass, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	return s.store.PassesForLease(ctx, leaseID, limit)
}

// CancelPass withdraws an expected visitor on one tenancy.
func (s *Community) CancelPass(ctx context.Context, leaseID, id string) (Pass, error) {
	if id == "" || leaseID == "" {
		return Pass{}, ErrNoPass
	}
	return s.store.CancelPass(ctx, leaseID, id)
}
