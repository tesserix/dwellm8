package service_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// ADR-0032 §4, from the lease module's side. The gate is asked before anything is
// written, so these run without a database: what is under test is that the refusal
// happens first and says what is outstanding.

type gate struct {
	steps []service.ChecklistStep
	err   error
	askedFor,
	askedTo string
}

func (g *gate) Outstanding(_ context.Context, leaseID, to string) ([]service.ChecklistStep, error) {
	g.askedFor, g.askedTo = leaseID, to
	return g.steps, g.err
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func termination() domain.Termination {
	return domain.Termination{
		EffectiveOn: effective.Day(2026, 7, 1),
		By:          domain.ActorOwner,
		Reason:      "the tenant is leaving",
		Decision:    domain.DecisionNone,
	}
}

// The story's failure scenario, at the seam: refused, naming the outstanding step.
func TestATenancyWillNotCloseOverAnUnfinishedMoveOut(t *testing.T) {
	g := &gate{steps: []service.ChecklistStep{
		{Title: "Exit inspection", Owner: "field_agent"},
		{Title: "Final meter reading", Owner: "field_agent"},
	}}
	// A nil store is safe and is the point: the refusal must come before anything is
	// written, so nothing here should ever reach one.
	leases := service.NewLeases(nil, quiet()).WithChecklists(g)

	_, err := leases.Terminate(context.Background(), "lease-1", termination())
	if !errors.Is(err, service.ErrChecklistOutstanding) {
		t.Fatalf("terminating over an unfinished move-out gave %v, want ErrChecklistOutstanding", err)
	}
	for _, want := range []string{"Exit inspection", "Final meter reading"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is %q and does not name %q — a manager told the tenancy "+
				"cannot be closed learns nothing", err, want)
		}
	}
	if !strings.Contains(err.Error(), "field_agent") {
		t.Errorf("the refusal is %q and does not say whose the steps are", err)
	}
	if g.askedFor != "lease-1" || g.askedTo != string(domain.StateTerminated) {
		t.Errorf("the gate was asked about (%q, %q), want (lease-1, terminated)", g.askedFor, g.askedTo)
	}
}

// A module that cannot answer must not be read as "nothing outstanding": that would
// turn an outage in the maintenance module into a tenancy closing over a move-out
// nobody checked.
func TestAGateThatCannotAnswerRefuses(t *testing.T) {
	g := &gate{err: errors.New("the checklist store is down")}
	leases := service.NewLeases(nil, quiet()).WithChecklists(g)

	_, err := leases.Terminate(context.Background(), "lease-1", termination())
	if err == nil {
		t.Fatal("a tenancy closed while the checklist gate was unavailable")
	}
	if errors.Is(err, service.ErrChecklistOutstanding) {
		t.Error("an unavailable gate was reported as outstanding work, which sends somebody " +
			"looking for a step that does not exist")
	}
}

// A lease service built without the gate still works — that is how this module ran
// before ADR-0032 — and the schema's trigger is what covers it. Asserted so the
// nil case is a decision rather than an accident.
func TestWithoutAGateTheSchemaIsTheOnlyGuard(t *testing.T) {
	leases := service.NewLeases(nil, quiet())
	if _, err := leases.Terminate(context.Background(), "", termination()); err == nil {
		t.Fatal("terminating with no lease named was accepted")
	}
}
