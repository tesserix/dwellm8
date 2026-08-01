package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Checklists is the maintenance module's seam, as this module needs it: what
// blocking work stands in front of a lease transition. ADR-0032 §4, ADR-0001 §3 — a
// service interface, never maintenance's store.
//
// It answers with the steps rather than a boolean because the refusal has to name
// them: a manager told "the tenancy cannot be closed" learns nothing, and one told
// "the exit inspection and the final meter reading are outstanding" knows what to do.
type Checklists interface {
	Outstanding(ctx context.Context, leaseID, to string) ([]ChecklistStep, error)
}

// ChecklistStep is one outstanding blocking step, as this module sees it.
type ChecklistStep struct {
	Title string
	Owner string
	DueOn effective.Date
}

// Billing is the money module's seam: how far a tenancy has been invoiced.
//
// It decides whether a termination is retrospective, which is the difference between
// a close that needs no decision and one that must adjust, refund or forfeit.
type Billing interface {
	BilledThrough(ctx context.Context, leaseID string) (time.Time, error)
}

// WithChecklists returns a copy that consults the maintenance module before closing
// a tenancy.
//
// A copy rather than a setter, for the reason WithResidents is one: the wiring in
// main() reads as one expression, and a service built without the gate cannot
// acquire it later from somewhere less visible.
func (s *Leases) WithChecklists(c Checklists) *Leases {
	out := *s
	out.checklists = c
	return &out
}

// WithBilling returns a copy that knows how far a tenancy has been invoiced.
func (s *Leases) WithBilling(b Billing) *Leases {
	out := *s
	out.billing = b
	return &out
}

// ErrChecklistOutstanding is the story's failure scenario: a tenancy that cannot be
// closed because a blocking step is not done. It names them.
var ErrChecklistOutstanding = errors.New("lease: the move-out is not finished")

// Terminate ends a tenancy.
//
// Three refusals, in the order a person would meet them. The checklist gate comes
// first because it is the one they can act on without a decision — finish the step —
// and putting it after the settlement decision would make somebody choose between
// adjusting and refunding for a termination that was never going to be permitted.
//
// The schema refuses all three again. That is not belt and braces: this path
// produces the sentence, and the triggers cover the paths that never come through
// here (ADR-0032 §4).
func (s *Leases) Terminate(ctx context.Context, id string, t domain.Termination) (domain.Lease, error) {
	if id == "" {
		return domain.Lease{}, store.ErrNoLease
	}

	if s.checklists != nil {
		outstanding, err := s.checklists.Outstanding(ctx, id, string(domain.StateTerminated))
		if err != nil {
			return domain.Lease{}, fmt.Errorf("lease %s: checking the move-out: %w", id, err)
		}
		if len(outstanding) > 0 {
			return domain.Lease{}, fmt.Errorf("%w: %s", ErrChecklistOutstanding, stepTitles(outstanding))
		}
	}

	var billed effective.Date
	if s.billing != nil {
		through, err := s.billing.BilledThrough(ctx, id)
		if err != nil {
			return domain.Lease{}, fmt.Errorf("lease %s: reading how far it has been billed: %w", id, err)
		}
		if !through.IsZero() {
			billed = effective.DateOf(through, through.Location())
		}
	}

	out, err := s.store.Terminate(ctx, id, t, billed)
	if err != nil {
		return domain.Lease{}, err
	}
	s.log.Info("tenancy ended",
		"lease", id, "on", t.EffectiveOn, "by", t.By, "decision", t.Decision,
		"event", domain.EventEnded)
	return out, nil
}

// stepTitles renders the outstanding steps into the refusal, each with who owes it:
// a list of tasks is only actionable if it says whose they are.
func stepTitles(steps []ChecklistStep) string {
	out := ""
	for i, s := range steps {
		if i > 0 {
			out += ", "
		}
		out += s.Title
		if s.Owner != "" {
			out += " (" + s.Owner + ")"
		}
	}
	return out
}
