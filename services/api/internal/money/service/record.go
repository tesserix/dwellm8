package service

import (
	"context"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// Record is rent that arrived without a provider in the path: cash to a
// caretaker, a cheque, a transfer the manager watched land.
//
// It exists because Collect alone cannot finish the job. An online collection
// is created here and carried to captured by a webhook days later; an offline
// one has already happened by the time anybody types it in, and there is no
// second event coming. So this walks the whole state machine in one act —
// created, attempted, captured — with the person recording it as the evidence
// (ADR-0011 §6).
//
// Idempotent on the same terms as Collect: the same key returns the same
// payment and posts one receipt.
func (s *Payments) Record(ctx context.Context, req CollectRequest) (collect.Payment, error) {
	if !req.Method.IsOffline() {
		return collect.Payment{}, fmt.Errorf(
			"money: %s goes through a provider and cannot be asserted by a person", req.Method)
	}

	p, _, err := s.Collect(ctx, req)
	if err != nil {
		return collect.Payment{}, err
	}
	// An earlier recording of the same key is already wherever it got to.
	if p.Status != collect.StatusCreated {
		return p, nil
	}

	attempted, err := s.store.ApplyConfirmed(ctx, p.ID, collect.StatusAttempted, s.now())
	if err != nil {
		return collect.Payment{}, fmt.Errorf("recording %s as attempted: %w", p.ID, err)
	}
	return s.Confirm(ctx, attempted)
}
