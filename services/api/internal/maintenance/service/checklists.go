// Package service is the maintenance module's public interface — the seam ADR-0001
// §3 requires, and the only way another module may reach a checklist.
//
// The lease module is the caller that matters: it asks Outstanding() before closing
// a tenancy and refuses with what comes back. That is one method rather than a
// shared table, and the boundary test is what keeps it that way.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Checklists is the module's service.
type Checklists struct {
	store *store.Checklists
	log   *slog.Logger
}

// NewChecklists builds the service.
func NewChecklists(s *store.Checklists, log *slog.Logger) *Checklists {
	return &Checklists{store: s, log: log}
}

// Checklist and Task are re-exported so a caller does not import the domain to
// read a response. The lease module needs Task, and only Task.
type (
	Checklist = domain.Checklist
	Task      = domain.Task
	Process   = domain.Process
	Progress  = store.Progress
)

// Errors a caller distinguishes. ErrBlocked is the story's failure scenario and the
// one the lease module maps to a refusal naming the steps.
var (
	ErrBlocked     = domain.ErrBlocked
	ErrNoTemplate  = domain.ErrNoTemplate
	ErrNoChecklist = store.ErrNoChecklist
)

// Start resolves the template for this process and property kind, materialises it
// and writes it. ADR-0032 §2 and §3.
//
// The property kind is passed in rather than looked up here: the caller already knows
// which property it is acting on, and a second read of the same row to answer a
// question the request already contained is a round trip for nothing.
func (s *Checklists) Start(ctx context.Context, process Process, kind domain.PropertyKind,
	about domain.Subject, anchor effective.Date, by string) (Checklist, error) {

	tenant, ok := tenancy.From(ctx)
	if !ok {
		return Checklist{}, tenancy.ErrNoTenant
	}
	if !process.Known() {
		return Checklist{}, fmt.Errorf("%w: %q is not a process", domain.ErrTemplate, process)
	}

	candidates, err := s.store.TemplatesFor(ctx, process)
	if err != nil {
		return Checklist{}, err
	}
	template, err := domain.Resolve(candidates, tenant.String(), process, kind)
	if err != nil {
		return Checklist{}, err
	}
	c, err := template.Trigger(tenant.String(), anchor, about)
	if err != nil {
		return Checklist{}, err
	}

	out, err := s.store.Trigger(ctx, c, by)
	var already store.ErrAlreadyOpen
	if errors.As(err, &already) {
		// Firing twice is a manager pressing the button twice, so the existing
		// process comes back rather than an error a client has to interpret.
		s.log.Info("checklist already open", "process", process, "checklist", already.ExistingID)
		return s.store.Read(ctx, already.ExistingID)
	}
	return out, err
}

// Read returns one checklist with its tasks.
func (s *Checklists) Read(ctx context.Context, id string) (Checklist, error) {
	if id == "" {
		return Checklist{}, ErrNoChecklist
	}
	return s.store.Read(ctx, id)
}

// Complete settles one task and releases whatever waited on it.
func (s *Checklists) Complete(ctx context.Context, id, stepCode, by string) (Checklist, error) {
	return s.store.Settle(ctx, id, stepCode, domain.TaskDone, by, "")
}

// Skip settles a non-blocking task with a reason. A blocking one is refused.
func (s *Checklists) Skip(ctx context.Context, id, stepCode, by, reason string) (Checklist, error) {
	return s.store.Settle(ctx, id, stepCode, domain.TaskSkipped, by, reason)
}

// Finish closes a checklist, refusing while blocking steps are outstanding.
func (s *Checklists) Finish(ctx context.Context, id string) (Checklist, error) {
	return s.store.Close(ctx, id, domain.StateCompleted, "")
}

// Abandon stops a checklist, with a reason.
func (s *Checklists) Abandon(ctx context.Context, id, reason string) (Checklist, error) {
	return s.store.Close(ctx, id, domain.StateAbandoned, reason)
}

// Portfolio reads progress across the organisation, latest first, stalled first.
func (s *Checklists) Portfolio(ctx context.Context, state domain.State, limit int) ([]Progress, error) {
	return s.store.Portfolio(ctx, state, limit)
}

// Outstanding returns the blocking steps that stand in front of a lease transition,
// or nothing when none do. ADR-0032 §4.
//
// This is the whole of the lease module's dependency on this one. It answers with
// tasks rather than a boolean because the refusal has to name them: a manager told
// "the tenancy cannot be closed" learns nothing, and one told "the exit inspection
// and the final meter reading are outstanding" knows what to do next.
func (s *Checklists) Outstanding(ctx context.Context, leaseID string, to string) ([]Task, error) {
	if leaseID == "" {
		return nil, nil
	}
	var gating []domain.Process
	for _, p := range domain.Processes() {
		if p.Gates(to) {
			gating = append(gating, p)
		}
	}
	var out []Task
	for _, p := range gating {
		tasks, err := s.store.Outstanding(ctx, leaseID, p)
		if err != nil {
			return nil, err
		}
		out = append(out, tasks...)
	}
	return out, nil
}

// Titles renders tasks as a sentence fragment, so a caller building a refusal does
// not reimplement it.
func Titles(tasks []Task) string { return domain.Titles(tasks) }

// LeaseGate adapts this service to the lease module's Checklists seam.
//
// It exists because the two modules must not share a type: the lease module declares
// the shape it needs, this one satisfies it, and neither imports the other's domain.
// The adapter is the whole of the coupling, and it is eleven lines.
type LeaseGate struct{ Checklists *Checklists }

// Outstanding returns the blocking steps standing in front of a lease transition, in
// the lease module's shape.
func (g LeaseGate) Outstanding(ctx context.Context, leaseID, to string) ([]leaseservice.ChecklistStep, error) {
	tasks, err := g.Checklists.Outstanding(ctx, leaseID, to)
	if err != nil {
		return nil, err
	}
	out := make([]leaseservice.ChecklistStep, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, leaseservice.ChecklistStep{
			Title: t.Title, Owner: string(t.Owner), DueOn: t.DueOn})
	}
	return out, nil
}
