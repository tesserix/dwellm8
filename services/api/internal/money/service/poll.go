package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// Polling: asking the provider what happened, rather than waiting to be told.
//
// ADR-0011 built the delivery path and it is the right one in production — a
// webhook arrives in seconds where a poll takes as long as its interval. But a
// webhook cannot reach a laptop, and it cannot reach a cluster whose ingress the
// provider has not been told about, so a collection built only around deliveries
// cannot be exercised until the last piece of infrastructure is in place.
//
// This is the other half, and it was nearly free: Confirm already asks Cashfree
// `GET /orders/{id}/payments` rather than trusting anything the delivery said.
// The webhook was never the source of truth; it was the *trigger*. So polling
// swaps the trigger and reuses every line that follows it.
//
// # It cannot double-collect
//
// Both paths end in the same Confirm, and Confirm ends in ApplyConfirmedAndPost,
// which is one transaction with an idempotency key on the entry. A webhook and a
// poll racing on the same payment produce one posting and one ErrStaleTransition
// — which is logged and is not a fault.

// Pollable is the slice of the store a sweep needs. An interface so the sweep
// can be tested without a database, and so it cannot reach anything else.
type Pollable interface {
	Pending(ctx context.Context, olderThan time.Time, limit int) ([]collect.Payment, error)
}

// PollResult is what one sweep did.
type PollResult struct {
	Asked     int
	Moved     int
	Unchanged int
	Failed    int
}

// Poll asks the provider about one payment and applies whatever it says.
//
// Named for what it does rather than for the payment's outcome: it may confirm,
// fail, expire or change nothing, and a caller that assumed "poll" meant
// "confirm" would write a happy path with no other branches.
func (s *Payments) Poll(ctx context.Context, p collect.Payment) (collect.Payment, error) {
	if !p.Status.AwaitingPayer() {
		// Nothing to ask about. Note this is not IsTerminal: a captured payment is
		// non-terminal — it can still settle or be refunded — and asking the
		// provider about it changes nothing, because settlement is a
		// reconciliation question rather than a payment-status one.
		return p, nil
	}
	return s.Confirm(ctx, p)
}

// Sweep polls every payment that has been waiting longer than `settleWithin`.
//
// The age filter is the whole design. A payment created two seconds ago is a
// tenant still looking at their UPI app, and asking about it produces a request
// per second per collection for an answer that has not changed. Waiting means
// the sweep asks about payments that have plausibly finished.
//
// It returns what it did rather than logging and swallowing, because "the sweep
// ran" and "the sweep moved eleven payments" are different facts and only the
// second one is worth alerting on.
func (s *Payments) Sweep(ctx context.Context, store Pollable, settleWithin time.Duration, limit int) (PollResult, error) {
	if limit <= 0 {
		limit = 100
	}
	older := s.now().Add(-settleWithin)

	pending, err := store.Pending(ctx, older, limit)
	if err != nil {
		return PollResult{}, fmt.Errorf("money: reading pending payments: %w", err)
	}

	var out PollResult
	for _, p := range pending {
		out.Asked++
		updated, err := s.Poll(ctx, p)
		switch {
		case err != nil:
			// One provider error does not stop the sweep. A single unreachable
			// order would otherwise leave every later payment unasked, which turns
			// one stuck collection into all of them.
			out.Failed++
			s.log.Error("polling a payment", "payment", p.ID, "provider", p.Provider, "error", err)
		case updated.Status != p.Status:
			out.Moved++
			s.log.Info("a poll moved a payment",
				"payment", p.ID, "from", p.Status, "to", updated.Status)
		default:
			out.Unchanged++
		}
	}
	return out, nil
}

// ErrNotPollable is a payment whose provider cannot be asked.
var ErrNotPollable = errors.New("money: that provider cannot be polled")
