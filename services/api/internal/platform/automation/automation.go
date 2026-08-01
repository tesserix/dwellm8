// Package automation is the engine behind ADR-0033's prebuilt automations.
//
// It knows nothing about property. What it owns is the three things every
// automation needs and none of them should implement twice: how a setting is
// resolved, what happens when a proposal exceeds its ceiling, and how a run that
// is interrupted does not repeat itself.
//
// # A setting is an override, never a prerequisite
//
// The catalogue lives in Go. An organisation with no rows runs every automation on
// its defaults, which is the story's "no configuration" obtained by resolving
// absence rather than by a seeding step somebody will one day miss — and the
// failure mode of a missed seeding step is silence, because an organisation with
// nothing chasing its arrears looks exactly like one whose tenants all pay.
//
// # An automation proposes; it does not act
//
// Act() never calls a module directly. It calls Propose, which decides between
// acting, asking and skipping. That is what makes "stops and requests approval"
// a property of the engine rather than a discipline each automation has to
// remember, and it is why the ceiling can be raised without touching any of them.
//
// # Durability is the idempotency key
//
// ADR-0028's argument, unchanged: a CronJob that overruns is a CronJob that runs
// twice, and what makes the second run harmless is the key rather than a lock. The
// effect is performed before the run is recorded, so a crash between the two
// repeats the proposal on the next pass — at-least-once, the same guarantee
// ADR-0002 gives for events, and stated here rather than hoped for.
package automation

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Key names an automation in the catalogue and in a settings row.
type Key string

func (k Key) String() string { return string(k) }

// keyPattern is the schema's CHECK, restated so a bad key fails at the catalogue
// rather than at the first write.
func (k Key) valid() bool {
	if len(k) < 3 || len(k) > 64 {
		return false
	}
	for i, r := range k {
		switch {
		case r >= 'a' && r <= 'z':
		case r == '_' || (r >= '0' && r <= '9'):
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// TriggerKind is what makes an automation run.
type TriggerKind string

const (
	// TriggerSchedule runs on the periodic job, once per organisation. ADR-0028.
	TriggerSchedule TriggerKind = "schedule"
	// TriggerEvent runs when a fact arrives on the stream it names. The event is
	// the trigger and never the action: an automation that publishes what it
	// consumes is a loop nobody sees until production.
	TriggerEvent TriggerKind = "event"
)

// Param is a setting an organisation may change without a release.
//
// Every parameter is an integer, and that is a deliberate restriction rather than
// a simplification: a parameter that can be an arbitrary shape is a parameter the
// settings screen cannot render and the engine cannot bound. Days, counts and
// money all fit, and anything that does not is a new automation.
type Param struct {
	Name    string
	Purpose string
	// Unit is what the number means, for a screen that has to label it.
	Unit string
	// Default applies when the organisation has not said otherwise.
	Default int64
	Min     int64
	Max     int64
}

// Definition is one prebuilt automation.
type Definition struct {
	Key     Key
	Name    string
	Purpose string

	Trigger TriggerKind
	// On is the event type a TriggerEvent automation reacts to.
	On string

	// EnabledByDefault is true for every prebuilt automation, and the field exists
	// so that one shipped dark is a visible decision rather than an absent row.
	EnabledByDefault bool

	// CeilingMinor is the default limit above which this automation asks instead
	// of acting. Zero means every proposal carrying money must be approved, which
	// is the right default for anything that moves money at all.
	CeilingMinor int64

	Params []Param

	// Act proposes whatever this automation would do. It is given a Run and must
	// reach the world only through it.
	Act func(ctx context.Context, r *Run) error
}

// Catalogue is the set of automations this build ships.
type Catalogue []Definition

// Validate asserts the catalogue is usable before anything runs on it.
func (c Catalogue) Validate() error {
	seen := map[Key]bool{}
	for _, d := range c {
		switch {
		case !d.Key.valid():
			return fmt.Errorf("automation: %q is not a usable key", d.Key)
		case seen[d.Key]:
			return fmt.Errorf("automation: two automations are called %s", d.Key)
		case d.Name == "" || d.Purpose == "":
			return fmt.Errorf("automation: %s has no name or no purpose — a switch nobody can "+
				"explain is a switch nobody dares turn off", d.Key)
		case d.Act == nil:
			return fmt.Errorf("automation: %s does nothing", d.Key)
		case d.Trigger == TriggerEvent && d.On == "":
			return fmt.Errorf("automation: %s is event-triggered and names no event", d.Key)
		case d.Trigger == TriggerSchedule && d.On != "":
			return fmt.Errorf("automation: %s is scheduled and also names an event", d.Key)
		case d.CeilingMinor < 0:
			return fmt.Errorf("automation: %s has a negative ceiling", d.Key)
		}
		seen[d.Key] = true

		names := map[string]bool{}
		for _, p := range d.Params {
			switch {
			case p.Name == "":
				return fmt.Errorf("automation: %s has an unnamed parameter", d.Key)
			case names[p.Name]:
				return fmt.Errorf("automation: %s has two parameters called %s", d.Key, p.Name)
			case p.Min > p.Max:
				return fmt.Errorf("automation: %s.%s permits nothing (min %d, max %d)",
					d.Key, p.Name, p.Min, p.Max)
			case p.Default < p.Min || p.Default > p.Max:
				return fmt.Errorf("automation: %s.%s defaults to %d, outside %d..%d — a default "+
					"the engine would refuse to save", d.Key, p.Name, p.Default, p.Min, p.Max)
			}
			names[p.Name] = true
		}
	}
	return nil
}

// Lookup finds one automation.
func (c Catalogue) Lookup(k Key) (Definition, bool) {
	for _, d := range c {
		if d.Key == k {
			return d, true
		}
	}
	return Definition{}, false
}

// Scheduled returns the automations the periodic job runs, in catalogue order so
// a run covers them the same way every time.
func (c Catalogue) Scheduled() Catalogue {
	var out Catalogue
	for _, d := range c {
		if d.Trigger == TriggerSchedule {
			out = append(out, d)
		}
	}
	return out
}

// For returns the automations that react to an event type.
func (c Catalogue) For(eventType string) Catalogue {
	var out Catalogue
	for _, d := range c {
		if d.Trigger == TriggerEvent && d.On == eventType {
			out = append(out, d)
		}
	}
	return out
}

// Override is what an organisation changed. Every field is optional, because a
// row exists to record a difference and an absent field is not one.
type Override struct {
	// Enabled is nil when the organisation has not switched this automation.
	Enabled *bool
	// Params holds only the parameters that were changed.
	Params map[string]int64
	// CeilingMinor is nil when the catalogue's ceiling stands.
	CeilingMinor *int64
}

// Settings is an automation as it will actually run: the catalogue resolved
// through whatever the organisation changed.
type Settings struct {
	Key          Key
	Enabled      bool
	Params       map[string]int64
	CeilingMinor int64
	// Overridden names the fields the organisation changed, so a settings screen
	// can show what is customised and what is still the default.
	Overridden []string
}

// Param returns a resolved parameter. A name the catalogue does not declare
// returns zero: the engine refuses to save one, so reaching for one is a bug in
// an automation rather than data.
func (s Settings) Param(name string) int64 { return s.Params[name] }

// Resolve applies an override to a definition. ADR-0033 §1.
func Resolve(d Definition, o Override) Settings {
	s := Settings{
		Key:          d.Key,
		Enabled:      d.EnabledByDefault,
		CeilingMinor: d.CeilingMinor,
		Params:       make(map[string]int64, len(d.Params)),
	}
	for _, p := range d.Params {
		s.Params[p.Name] = p.Default
	}

	if o.Enabled != nil && *o.Enabled != d.EnabledByDefault {
		s.Enabled = *o.Enabled
		s.Overridden = append(s.Overridden, "enabled")
	} else if o.Enabled != nil {
		s.Enabled = *o.Enabled
	}
	if o.CeilingMinor != nil {
		s.CeilingMinor = *o.CeilingMinor
		if *o.CeilingMinor != d.CeilingMinor {
			s.Overridden = append(s.Overridden, "approval_ceiling_minor")
		}
	}
	for _, p := range d.Params {
		v, ok := o.Params[p.Name]
		if !ok {
			continue
		}
		// Clamped rather than refused: a stored value outside the range is a
		// parameter whose bounds were tightened by a release, and refusing to run
		// would switch the automation off without anybody deciding to.
		if v < p.Min {
			v = p.Min
		}
		if v > p.Max {
			v = p.Max
		}
		s.Params[p.Name] = v
		if v != p.Default {
			s.Overridden = append(s.Overridden, p.Name)
		}
	}
	sort.Strings(s.Overridden)
	return s
}

// Validate checks an override against what the catalogue declares, so a bad
// setting is refused at the API rather than clamped silently at the next run.
func Validate(d Definition, o Override) error {
	if o.CeilingMinor != nil && *o.CeilingMinor < 0 {
		return fmt.Errorf("automation: a ceiling of %d", *o.CeilingMinor)
	}
	declared := map[string]Param{}
	for _, p := range d.Params {
		declared[p.Name] = p
	}
	var unknown []string
	for name, v := range o.Params {
		p, ok := declared[name]
		if !ok {
			unknown = append(unknown, name)
			continue
		}
		if v < p.Min || v > p.Max {
			return fmt.Errorf("automation: %s.%s must be between %d and %d, and %d is not",
				d.Key, name, p.Min, p.Max, v)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return fmt.Errorf("automation: %s has no parameter called %s",
			d.Key, strings.Join(unknown, " or "))
	}
	return nil
}

// SubjectKind is what an automation acted on. Closed, and the same list the
// schema's CHECK holds.
type SubjectKind string

const (
	SubjectLease         SubjectKind = "lease"
	SubjectUnit          SubjectKind = "unit"
	SubjectProperty      SubjectKind = "property"
	SubjectOrganisation  SubjectKind = "organisation"
	SubjectChecklist     SubjectKind = "checklist"
	SubjectStatutoryRule SubjectKind = "statutory_rule"
)

// Subject is the record an automation acted on, and the one whose screen will
// later ask what was automated.
type Subject struct {
	Kind SubjectKind
	ID   string
}

// Outcome is what a proposal came to.
type Outcome string

const (
	// OutcomeActed is the effect performed and recorded.
	OutcomeActed Outcome = "acted"
	// OutcomeSkipped is a proposal this organisation has already had, or one the
	// automation itself decided against. Recorded, because "it considered this
	// tenancy and did nothing" is a fact somebody will ask about.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeAwaitingApproval is the story's failure scenario: over the ceiling,
	// nothing performed, an approval requested naming what it wanted.
	OutcomeAwaitingApproval Outcome = "awaiting_approval"
	// OutcomeFailed is the effect erroring. The run continues to the next subject.
	OutcomeFailed Outcome = "failed"
)

// ErrApprovalRequired is returned by Propose when the ceiling stopped it, so an
// automation that wants to stop the whole subject can, and one that wants to try
// the next thing can too.
var ErrApprovalRequired = errors.New("automation: over the approval ceiling")

// Proposal is what an automation would like to do.
type Proposal struct {
	Subject Subject
	// Action is a short stable verb, e.g. "arrears.reminder" or
	// "checklist.move_out". It is what a record's history shows.
	Action string
	Detail string

	// AmountMinor is the money this would move, or zero. Only a non-zero amount
	// can exceed a ceiling, which is why an automation that moves no money never
	// needs an approval and an automation that does can never avoid one.
	AmountMinor int64

	// Key makes this proposal distinct within (organisation, automation). Usually
	// the subject and the step or period — "lease-7:step-2", not "lease-7" — so a
	// ladder can climb without repeating its first rung.
	Key string

	// Do performs the effect. Called only when the proposal is inside the ceiling
	// and has not been performed before.
	Do func(ctx context.Context) error
}

func (p Proposal) validate() error {
	switch {
	case p.Subject.ID == "" || p.Subject.Kind == "":
		return errors.New("automation: a proposal about nothing")
	case p.Action == "":
		return errors.New("automation: a proposal with no action — a record's history would " +
			"show that something happened and not what")
	case p.Key == "":
		return errors.New("automation: a proposal with no idempotency key would repeat every run")
	case p.AmountMinor < 0:
		return fmt.Errorf("automation: a proposal moving %d", p.AmountMinor)
	}
	return nil
}

// Record is one row of the run log.
type Record struct {
	Automation Key
	Subject    Subject
	Outcome    Outcome
	Action     string
	Detail     string
	Params     map[string]int64
	Amount     int64
	Key        string
}

// ApprovalRequest is what a refused proposal asks for.
type ApprovalRequest struct {
	Automation Key
	Subject    Subject
	Action     string
	Amount     int64
	Ceiling    int64
	Key        string
}

// Store is the engine's persistence. An interface so the engine's decisions are
// testable without a database — the decisions are the interesting part, and they
// are all in this file.
type Store interface {
	// Overrides reads what the organisation in the context changed.
	Overrides(ctx context.Context) (map[Key]Override, error)
	// Save writes one override.
	Save(ctx context.Context, key Key, o Override, by string) error
	// Recorded reports whether this proposal has already been run, whatever it
	// came to. It is the pre-check that makes a second pass a no-op.
	Recorded(ctx context.Context, key Key, idempotencyKey string) (bool, error)
	// Record writes a run, reporting false when the key was already used.
	Record(ctx context.Context, r Record) (bool, error)
	// Requested reports whether this proposal already has a live approval, so a
	// ladder does not ask twice while somebody is deciding.
	Requested(ctx context.Context, key Key, idempotencyKey string) (bool, error)
	// RequestApproval writes the request, reporting false when it already exists.
	RequestApproval(ctx context.Context, a ApprovalRequest) (bool, error)
}
