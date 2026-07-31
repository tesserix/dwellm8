package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

func interval(t *testing.T, fy int, fm time.Month, fd int, to ...int) effective.Interval {
	t.Helper()
	var (
		iv  effective.Interval
		err error
	)
	if len(to) == 0 {
		iv, err = effective.Since(effective.Day(fy, fm, fd))
	} else {
		iv, err = effective.Between(effective.Day(fy, fm, fd), effective.Day(to[0], time.Month(to[1]), to[2]))
	}
	if err != nil {
		t.Fatalf("interval: %v", err)
	}
	return iv
}

func history(t *testing.T, records ...effective.Record[tds.Facts]) tds.History {
	t.Helper()
	h, err := tds.NewHistory(records)
	if err != nil {
		t.Fatalf("building the tax history: %v", err)
	}
	return h
}

func pending(t *testing.T, tax tds.History) domain.Lease {
	t.Helper()
	return domain.Lease{
		ID: "lease-1", TenantID: "org-1", Property: "prop-1", Unit: "unit-1",
		State: domain.StatePendingSignature,
		Term:  interval(t, 2026, 4, 1, 2027, 4, 1),
		Tax:   tax,
	}
}

// The story's primary path, from the lease's side: a company tenant and a resident
// landlord, and the tenancy starts.
func TestATenancyWithARecordedTaxPathActivates(t *testing.T) {
	l := pending(t, history(t, effective.Record[tds.Facts]{
		ID: "1", Range: interval(t, 2026, 4, 1), Kind: effective.KindChange,
		Value: tds.Facts{Deductor: tds.Business, Residency: tds.Resident,
			From: effective.Day(2026, 4, 1), Source: "tenant declaration"},
	}))

	active, ev, err := l.Activate(domain.ActorOwner)
	if err != nil {
		t.Fatalf("activating: %v", err)
	}
	if active.State != domain.StateActive || ev != domain.EventStarted {
		t.Errorf("activation produced %s/%s", active.State, ev)
	}

	path, _, err := active.TaxPathOn(effective.Day(2026, 5, 7))
	if err != nil {
		t.Fatalf("resolving the path: %v", err)
	}
	if path.Section != tds.Section194I {
		t.Errorf("the tenancy is on section %s, want 194-I", path.Section)
	}
}

// A lease that cannot say which section governs it does not become a tenancy. The
// alternative is a payout run nine months later discovering that every payment
// under it should have been deducted from.
func TestALeaseWithNoTaxFactsDoesNotBecomeATenancy(t *testing.T) {
	l := pending(t, tds.History{})

	if _, _, err := l.Activate(domain.ActorOwner); !errors.Is(err, domain.ErrTaxFacts) {
		t.Fatalf("activated a lease with no recorded tax path: %v", err)
	}

	// Facts that start after the tenancy does are the same failure: the first month
	// has no section.
	late := pending(t, history(t, effective.Record[tds.Facts]{
		ID: "1", Range: interval(t, 2026, 6, 1), Kind: effective.KindChange,
		Value: tds.Facts{Deductor: tds.Business, Residency: tds.Resident,
			From: effective.Day(2026, 6, 1), Source: "tenant declaration"},
	}))
	if _, _, err := late.Activate(domain.ActorOwner); !errors.Is(err, domain.ErrTaxFacts) {
		t.Errorf("activated a tenancy whose first two months have no section: %v", err)
	}
}

// The story's failure scenario, at the point it is enforced: a non-resident
// landlord means section 195, and the lease cannot be completed until the deductor
// has accepted an obligation that is theirs and starts at the first rupee.
func TestASection195TenancyCannotStartUnacknowledged(t *testing.T) {
	facts := tds.Facts{Deductor: tds.IndividualNoAudit, Residency: tds.NonResident,
		From: effective.Day(2026, 4, 1), Source: "landlord declaration"}
	l := pending(t, history(t, effective.Record[tds.Facts]{
		ID: "1", Range: interval(t, 2026, 4, 1), Kind: effective.KindChange, Value: facts,
	}))

	_, _, err := l.Activate(domain.ActorOwner)
	if !errors.Is(err, tds.ErrNotAcknowledged) {
		t.Fatalf("a section 195 tenancy started without an acknowledgement: %v", err)
	}

	facts.AcknowledgedOn, facts.AcknowledgedBy = effective.Day(2026, 3, 28), "tenant:priya"
	acknowledged := pending(t, history(t, effective.Record[tds.Facts]{
		ID: "1", Range: interval(t, 2026, 4, 1), Kind: effective.KindChange, Value: facts,
	}))
	if _, _, err := acknowledged.Activate(domain.ActorOwner); err != nil {
		t.Errorf("an acknowledged section 195 tenancy was refused: %v", err)
	}
}

// A section change inside the tenancy is reported before the payout run meets it,
// and one outside the occupancy is not the tenancy's problem.
func TestASectionChangeInsideTheTenancyIsReported(t *testing.T) {
	resident := tds.Facts{Deductor: tds.Business, Residency: tds.Resident,
		From: effective.Day(2026, 4, 1), Source: "tenant declaration"}
	left := tds.Facts{Deductor: tds.Business, Residency: tds.NonResident,
		From: effective.Day(2026, 10, 1), Source: "landlord declaration",
		AcknowledgedOn: effective.Day(2026, 10, 1), AcknowledgedBy: "tenant:acme"}

	l := pending(t, history(t,
		effective.Record[tds.Facts]{ID: "1", Range: interval(t, 2026, 4, 1, 2026, 10, 1),
			Kind: effective.KindChange, Value: resident},
		effective.Record[tds.Facts]{ID: "2", Range: interval(t, 2026, 10, 1),
			Kind: effective.KindChange, Value: left},
	))

	changes, err := l.TaxSectionChanges()
	if err != nil {
		t.Fatalf("listing section changes: %v", err)
	}
	if len(changes) != 1 || !changes[0].Equal(effective.Day(2026, 10, 1)) {
		t.Fatalf("section changes %v, want one on 2026-10-01", changes)
	}

	// The same history on a tenancy that ended in August: the landlord's move is
	// after occupancy ceased, so nothing under this lease changes section.
	ended := l
	ended.EndedOn = effective.Day(2026, 8, 1)
	changes, err = ended.TaxSectionChanges()
	if err != nil {
		t.Fatalf("listing section changes: %v", err)
	}
	if len(changes) != 0 {
		t.Errorf("a tenancy that ceased in August reports a section change at %v", changes)
	}
}

// Activation is a transition like any other, and the tax gate does not replace the
// state machine's own rules.
func TestActivationStillObeysTheStateMachine(t *testing.T) {
	l := pending(t, history(t, effective.Record[tds.Facts]{
		ID: "1", Range: interval(t, 2026, 4, 1), Kind: effective.KindChange,
		Value: tds.Facts{Deductor: tds.Business, Residency: tds.Resident,
			From: effective.Day(2026, 4, 1), Source: "tenant declaration"},
	}))

	l.State = domain.StateTerminated
	if _, _, err := l.Activate(domain.ActorOwner); !errors.Is(err, domain.ErrTransition) {
		t.Errorf("a terminated tenancy was activated: %v", err)
	}

	l.State = domain.StatePendingSignature
	if _, _, err := l.Activate(domain.ActorSystem); err != nil {
		t.Errorf("the clock could not activate a countersigned lease: %v", err)
	}
}
