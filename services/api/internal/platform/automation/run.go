package automation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Run is what an automation is handed. It carries the settings it resolved to and
// is the only way it may reach the world.
//
// The restriction is the design: an automation that could call a module service
// directly could act beyond its ceiling, and the ceiling would become a rule each
// author has to remember rather than a property of the engine.
type Run struct {
	Definition Definition
	Settings   Settings

	// Event is the fact an event-triggered automation is reacting to, and empty
	// for a scheduled one.
	Event Event

	store Store
	log   *slog.Logger

	// tallies, so a run can report what it did without the caller re-reading the
	// log it just wrote.
	acted, skipped, awaiting, failed int
}

// Param is the resolved value of a declared parameter.
func (r *Run) Param(name string) int64 { return r.Settings.Param(name) }

// Days is Param with the unit spelled, because every scheduled automation reads
// at least one and `r.Days("first_reminder_after")` says what it means.
func (r *Run) Days(name string) int { return int(r.Settings.Param(name)) }

// Propose decides between acting, asking and skipping. ADR-0033 §3.
//
// The order matters and is the whole of the method:
//
//  1. a proposal already recorded is skipped, whatever it would have done — the
//     idempotency key, which is what makes an overrunning CronJob harmless;
//  2. a proposal carrying more money than the ceiling asks and performs nothing;
//  3. anything else acts, and is recorded afterwards.
//
// Recorded afterwards, and that is a decision with a window in it: a crash between
// the effect and the row repeats the proposal on the next pass. At-least-once, the
// same guarantee ADR-0002 gives events, and the alternative is worse — a row
// written first claims an effect that may then fail, and the run log is the thing
// somebody reads to find out what actually happened.
//
// A failure is recorded too, so the next pass does not retry it. Retrying a failing
// action every hour is how one broken automation becomes an outage, and the row
// carries the key so it can be requeued deliberately.
func (r *Run) Propose(ctx context.Context, p Proposal) (Outcome, error) {
	if err := p.validate(); err != nil {
		return "", err
	}

	done, err := r.store.Recorded(ctx, r.Definition.Key, p.Key)
	if err != nil {
		return "", fmt.Errorf("automation %s: reading the run log: %w", r.Definition.Key, err)
	}
	if done {
		r.skipped++
		return OutcomeSkipped, nil
	}

	// The ceiling before the effect, and the approval carries the same key the
	// action would have used, so granting it does not produce a second request.
	if p.AmountMinor > r.Settings.CeilingMinor {
		asked, err := r.store.Requested(ctx, r.Definition.Key, p.Key)
		if err != nil {
			return "", fmt.Errorf("automation %s: checking for an approval: %w", r.Definition.Key, err)
		}
		if !asked {
			if _, err := r.store.RequestApproval(ctx, ApprovalRequest{
				Automation: r.Definition.Key, Subject: p.Subject, Action: p.Action,
				Amount: p.AmountMinor, Ceiling: r.Settings.CeilingMinor, Key: p.Key,
			}); err != nil {
				return "", fmt.Errorf("automation %s: requesting approval: %w", r.Definition.Key, err)
			}
			r.log.Info("automation asked instead of acting",
				"automation", r.Definition.Key, "subject", p.Subject.ID,
				"amount_minor", p.AmountMinor, "ceiling_minor", r.Settings.CeilingMinor)
		}
		// Not recorded as a run, deliberately: nothing was done, the approval row
		// is the record, and writing a run here would consume the key that the
		// action needs when the approval is granted.
		r.awaiting++
		return OutcomeAwaitingApproval, ErrApprovalRequired
	}

	// A proposal with nothing to do is still recorded. "It looked at this tenancy
	// and decided against it" is the answer to a question somebody will ask, and a
	// log that holds only successes cannot give it.
	if p.Do == nil {
		if _, err := r.record(ctx, p, OutcomeSkipped, p.Detail); err != nil {
			return "", err
		}
		r.skipped++
		return OutcomeSkipped, nil
	}

	if err := p.Do(ctx); err != nil {
		if _, rerr := r.record(ctx, p, OutcomeFailed, err.Error()); rerr != nil {
			return "", rerr
		}
		r.failed++
		r.log.Error("automation failed",
			"automation", r.Definition.Key, "subject", p.Subject.ID,
			"action", p.Action, "key", p.Key, "error", err)
		return OutcomeFailed, err
	}

	if _, err := r.record(ctx, p, OutcomeActed, p.Detail); err != nil {
		return "", err
	}
	r.acted++
	r.log.Info("automation acted",
		"automation", r.Definition.Key, "subject", p.Subject.ID, "action", p.Action)
	return OutcomeActed, nil
}

func (r *Run) record(ctx context.Context, p Proposal, o Outcome, detail string) (bool, error) {
	written, err := r.store.Record(ctx, Record{
		Automation: r.Definition.Key,
		Subject:    p.Subject,
		Outcome:    o,
		Action:     p.Action,
		Detail:     detail,
		Params:     r.Settings.Params,
		Amount:     p.AmountMinor,
		Key:        p.Key,
	})
	if err != nil {
		return false, fmt.Errorf("automation %s: recording the run: %w", r.Definition.Key, err)
	}
	return written, nil
}

// Result is what one pass did.
type Result struct {
	Organisations int
	Automations   int
	Acted         int
	Skipped       int
	Awaiting      int
	Failed        int
}

// Organisations lists who to run for.
//
// The one privileged query in a run, and everything after it happens inside one
// organisation's session. ADR-0028 §3: running the whole pass as the platform role
// would put every organisation's tenancies in one result, where a single wrong
// join is a reminder sent to somebody else's tenant.
type Organisations interface {
	Active(ctx context.Context) ([]tenancy.ID, error)
}

// Runner executes a catalogue.
type Runner struct {
	catalogue Catalogue
	orgs      Organisations
	store     Store
	log       *slog.Logger
}

// NewRunner wires the engine.
func NewRunner(c Catalogue, o Organisations, s Store, log *slog.Logger) (*Runner, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &Runner{catalogue: c, orgs: o, store: s, log: log}, nil
}

// RunAll runs every scheduled automation for every active organisation.
//
// One organisation's failure does not stop the pass: an automation erroring for
// one landlord must not stop every other landlord's arrears being chased, and the
// alternative — abort on first error — makes the whole run only as reliable as its
// worst tenant's data.
func (r *Runner) RunAll(ctx context.Context) (Result, error) {
	orgs, err := r.orgs.Active(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("listing organisations: %w", err)
	}

	var out Result
	for _, org := range orgs {
		res, err := r.RunFor(tenancy.With(ctx, org))
		if err != nil {
			r.log.Error("automations failed for an organisation",
				"organisation", org, "error", err)
			out.Failed++
			continue
		}
		out.Organisations++
		out.Automations += res.Automations
		out.Acted += res.Acted
		out.Skipped += res.Skipped
		out.Awaiting += res.Awaiting
		out.Failed += res.Failed
	}
	return out, nil
}

// RunFor runs the scheduled automations for the organisation in the context.
func (r *Runner) RunFor(ctx context.Context) (Result, error) {
	if _, ok := tenancy.From(ctx); !ok {
		return Result{}, tenancy.ErrNoTenant
	}
	overrides, err := r.store.Overrides(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("reading the settings: %w", err)
	}

	var out Result
	for _, d := range r.catalogue.Scheduled() {
		settings := Resolve(d, overrides[d.Key])
		if !settings.Enabled {
			continue
		}
		out.Automations++
		run := &Run{Definition: d, Settings: settings, store: r.store, log: r.log}
		err := d.Act(ctx, run)
		out.Acted += run.acted
		out.Skipped += run.skipped
		out.Awaiting += run.awaiting
		out.Failed += run.failed
		if err != nil && !errors.Is(err, ErrApprovalRequired) {
			// Logged and carried, for the reason RunAll carries an organisation's
			// failure: one broken automation must not stop the other five.
			r.log.Error("automation stopped early", "automation", d.Key, "error", err)
			// Counted only when the proposal itself did not already count it: an
			// automation that returns the error Propose gave it has one failure,
			// not two, and a tally that double-counts is a tally nobody trusts.
			if run.failed == 0 {
				out.Failed++
			}
		}
	}
	return out, nil
}

// Handle runs the automations that react to one event, for the organisation the
// event belongs to. ADR-0033 §5's three checklist automations arrive this way.
func (r *Runner) Handle(ctx context.Context, eventType string, subject Subject, data map[string]string) error {
	matching := r.catalogue.For(eventType)
	if len(matching) == 0 {
		return nil
	}
	overrides, err := r.store.Overrides(ctx)
	if err != nil {
		return fmt.Errorf("reading the settings: %w", err)
	}

	for _, d := range matching {
		settings := Resolve(d, overrides[d.Key])
		if !settings.Enabled {
			continue
		}
		run := &Run{
			Definition: d, Settings: settings, store: r.store, log: r.log,
			Event: Event{Type: eventType, Subject: subject, Data: data},
		}
		if err := d.Act(ctx, run); err != nil && !errors.Is(err, ErrApprovalRequired) {
			return fmt.Errorf("automation %s on %s: %w", d.Key, eventType, err)
		}
	}
	return nil
}

// Event is the fact an event-triggered automation is reacting to. Empty for a
// scheduled one, and an automation that reads it must be TriggerEvent — the
// catalogue's Validate is what keeps those two facts together.
type Event struct {
	Type    string
	Subject Subject
	Data    map[string]string
}

// Settings resolves the whole catalogue for the organisation in the context,
// which is what the settings screen reads.
func (r *Runner) SettingsFor(ctx context.Context) ([]Settings, Catalogue, error) {
	overrides, err := r.store.Overrides(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Settings, 0, len(r.catalogue))
	for _, d := range r.catalogue {
		out = append(out, Resolve(d, overrides[d.Key]))
	}
	return out, r.catalogue, nil
}

// Set writes one override, after checking it against what the catalogue declares.
func (r *Runner) Set(ctx context.Context, key Key, o Override, by string) error {
	d, ok := r.catalogue.Lookup(key)
	if !ok {
		return fmt.Errorf("automation: there is no automation called %s", key)
	}
	if err := Validate(d, o); err != nil {
		return err
	}
	return r.store.Save(ctx, key, o, by)
}

// Catalogue returns what this build ships, for a screen that has to explain each
// switch before somebody turns it off.
func (r *Runner) Catalogue() Catalogue { return r.catalogue }
