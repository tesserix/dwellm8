// Package domain holds the checklist rules. ADR-0032.
//
// A move-in is fifteen steps and a move-out is twenty, and firing them as one graph
// is what stops a step being skipped by the person on their fourth property of the
// day. Three things here are more than bookkeeping.
//
// # A template is resolved rather than chosen
//
// Nobody picks a template. A manager says "start the move-out", and which list that
// is depends on the organisation and on the kind of property — a hostel move-out is
// not a commercial one, and a firm that manages a tower and a co-living block needs
// both while having one organisation. Resolve() is that decision, and it is here
// rather than in SQL so the order is readable and testable without a database.
//
// # Due dates come from an anchor, once
//
// Every step carries a signed offset in days from the checklist's anchor — the
// move-out date, the handover date, the tenancy start. The dates are computed when
// the checklist is fired and stored, never recomputed on read: an instance is a
// record of what was asked for, and a template edited next month must not change
// what somebody was told to do last week.
//
// # Blocking is a precondition, not a state
//
// A step marked blocking prevents the checklist completing and prevents the lease
// transition its process gates. This package produces the refusal a person reads,
// naming the outstanding steps; the schema refuses the same thing again from a
// trigger, because leases.state is written by paths that skip this code (ADR-0032
// §4). A blocking step cannot be skipped at all — if it were skippable it was not
// blocking, and one skipped blocking step makes every gate advisory.
package domain

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Process is a multi-step operation a checklist automates.
type Process string

const (
	ProcessMoveIn          Process = "move_in"
	ProcessMoveOut         Process = "move_out"
	ProcessOwnerOnboarding Process = "owner_onboarding"
	ProcessManagerHandover Process = "manager_handover"
	ProcessTenancyRenewal  Process = "tenancy_renewal"
)

// processes is the closed set, and whether the process is about a tenancy.
//
// A tenancy process names a lease, and the schema refuses one that does not: without
// it the move-out gate has nothing to attach to and would silently never fire.
var processes = map[Process]bool{
	ProcessMoveIn:          true,
	ProcessMoveOut:         true,
	ProcessTenancyRenewal:  true,
	ProcessOwnerOnboarding: false,
	ProcessManagerHandover: false,
}

// Processes returns every process, ordered.
func Processes() []Process {
	out := make([]Process, 0, len(processes))
	for p := range processes {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Known reports whether this is a process at all.
func (p Process) Known() bool { _, ok := processes[p]; return ok }

// AboutATenancy reports whether this process must name a lease.
func (p Process) AboutATenancy() bool { return processes[p] }

// Gates reports whether an unfinished checklist of this process blocks a lease
// transition to `to`.
//
// Move-out only, and only into the two states that end a tenancy. A move-in that is
// half done is a problem for somebody else; a move-out that is half done means the
// deposit is being settled against an inspection nobody did.
func (p Process) Gates(to string) bool {
	return p == ProcessMoveOut && (to == "terminated" || to == "settled")
}

func (p Process) String() string { return string(p) }

// PropertyKind is `properties.kind` — the vocabulary that already exists, rather
// than a second one for the same idea. Empty means the template serves any kind.
type PropertyKind string

var propertyKinds = map[PropertyKind]bool{
	"standalone": true, "building": true, "society": true,
	"commercial": true, "coliving": true, "plot": true,
}

// KnownPropertyKind reports whether k is a property kind, treating empty as "any".
func KnownPropertyKind(k PropertyKind) bool { return k == "" || propertyKinds[k] }

// OwnerRole is who a step lands on by default. A role rather than a person: a
// template outlives the manager who wrote it.
type OwnerRole string

var ownerRoles = map[OwnerRole]bool{
	"manager": true, "staff": true, "accountant": true, "owner": true,
	"field_agent": true, "warden": true, "guard": true, "tenant": true, "vendor": true,
}

// Step is one step of a template.
type Step struct {
	Code        string
	Title       string
	Description string
	Position    int
	Blocking    bool
	Owner       OwnerRole
	// DueOffsetDays is signed and relative to the checklist's anchor: "three days
	// before the tenant leaves" is -3 and "seven days after handover" is 7.
	DueOffsetDays int
	DependsOn     []string
}

// Template is a process for an organisation and a property kind.
type Template struct {
	ID       string
	TenantID string
	// Default marks the platform library — the bottom two rungs of the resolution
	// order below. An organisation may read one and may not write one.
	Default bool

	Process Process
	// Kind is empty for a template that serves any property kind.
	Kind    PropertyKind
	Name    string
	Version int
	Steps   []Step
}

// ErrNoTemplate is a process with nothing to fire. Distinguishable because the
// caller's answer is to configure one, not to retry.
var ErrNoTemplate = errors.New("checklist: no template for this process")

// ErrTemplate is a template that would not work: a dangling dependency, a cycle, a
// duplicate code.
var ErrTemplate = errors.New("checklist: the template is not usable")

// Resolve picks the template to fire, most specific first. ADR-0032 §2.
//
//  1. this organisation's, for this process and this property kind
//  2. this organisation's, for this process, any kind
//  3. the platform default, for this process and this kind
//  4. the platform default, for this process, any kind
//
// The candidates are whatever the store found for the process; this decides between
// them. A firm that has configured a co-living move-out gets theirs for the
// co-living block and the default for the tower, which is the case that makes
// "configurable per organisation" insufficient on its own.
func Resolve(candidates []Template, tenant string, process Process, kind PropertyKind) (Template, error) {
	rank := func(t Template) int {
		switch {
		case !t.Default && t.Kind == kind && kind != "":
			return 0
		case !t.Default && t.Kind == "":
			return 1
		case t.Default && t.Kind == kind && kind != "":
			return 2
		case t.Default && t.Kind == "":
			return 3
		}
		return -1 // a kind-specific template for some other kind
	}

	best, bestRank := Template{}, 4
	for _, t := range candidates {
		if t.Process != process {
			continue
		}
		if !t.Default && t.TenantID != tenant {
			continue // not ours, and not the library — the policy should have hidden it
		}
		r := rank(t)
		if r < 0 || r > bestRank {
			continue
		}
		// A later version of the same rung wins, which is what makes publishing a
		// new version take effect without retiring the old one first.
		if r == bestRank && t.Version <= best.Version {
			continue
		}
		best, bestRank = t, r
	}
	if bestRank == 4 {
		return Template{}, fmt.Errorf("%w: %s%s", ErrNoTemplate, process, forKind(kind))
	}
	return best, nil
}

func forKind(k PropertyKind) string {
	if k == "" {
		return ""
	}
	return " on a " + string(k) + " property"
}

// Validate asserts what the schema will assert, naming the step rather than the
// constraint.
//
// The cycle check is the one that earns its place. A dangling dependency fails
// loudly at the next write; a cycle produces its damage weeks later, at the move-out
// that will not close, on a task nobody can release.
func (t Template) Validate() error {
	if t.TenantID == "" {
		return fmt.Errorf("%w: a template belonging to no organisation", ErrTemplate)
	}
	if !t.Process.Known() {
		return fmt.Errorf("%w: %q is not a process", ErrTemplate, t.Process)
	}
	if !KnownPropertyKind(t.Kind) {
		return fmt.Errorf("%w: %q is not a property kind", ErrTemplate, t.Kind)
	}
	if t.Name == "" {
		return fmt.Errorf("%w: a template with no name is a template nobody can pick", ErrTemplate)
	}
	if t.Version < 1 {
		return fmt.Errorf("%w: version %d", ErrTemplate, t.Version)
	}
	if len(t.Steps) == 0 {
		return fmt.Errorf("%w: %s has no steps, so firing it would do nothing", ErrTemplate, t.Process)
	}

	byCode := make(map[string]Step, len(t.Steps))
	positions := map[int]string{}
	for _, s := range t.Steps {
		switch {
		case s.Code == "":
			return fmt.Errorf("%w: a step with no code", ErrTemplate)
		case s.Title == "":
			return fmt.Errorf("%w: step %s has no title", ErrTemplate, s.Code)
		case s.Position < 1:
			return fmt.Errorf("%w: step %s is at position %d", ErrTemplate, s.Code, s.Position)
		case !ownerRoles[s.Owner]:
			return fmt.Errorf("%w: step %s is owned by %q, which is not a role", ErrTemplate, s.Code, s.Owner)
		}
		if _, dup := byCode[s.Code]; dup {
			return fmt.Errorf("%w: two steps are called %s", ErrTemplate, s.Code)
		}
		if other, dup := positions[s.Position]; dup {
			return fmt.Errorf("%w: %s and %s are both at position %d",
				ErrTemplate, other, s.Code, s.Position)
		}
		byCode[s.Code] = s
		positions[s.Position] = s.Code
	}

	for _, s := range t.Steps {
		for _, d := range s.DependsOn {
			if d == s.Code {
				return fmt.Errorf("%w: step %s waits for itself", ErrTemplate, s.Code)
			}
			if _, ok := byCode[d]; !ok {
				return fmt.Errorf("%w: step %s depends on %s, which is not a step of this template",
					ErrTemplate, s.Code, d)
			}
		}
	}
	if cycle := findCycle(byCode); cycle != "" {
		return fmt.Errorf("%w: the steps depend on each other in a circle: %s — every task in it "+
			"would wait for a task waiting for it", ErrTemplate, cycle)
	}
	return nil
}

// findCycle returns the first cycle it finds as a printable path, or "".
//
// A depth-first walk with three colours rather than a visited set, because the
// useful output is the path: "deposit -> meter -> keys -> deposit" tells an author
// which edge to remove, and "there is a cycle" does not.
func findCycle(steps map[string]Step) string {
	const (
		white = 0
		grey  = 1
		black = 2
	)
	colour := make(map[string]int, len(steps))
	var path []string

	codes := make([]string, 0, len(steps))
	for c := range steps {
		codes = append(codes, c)
	}
	sort.Strings(codes) // deterministic, so the same template always names the same edge

	var walk func(string) string
	walk = func(code string) string {
		colour[code] = grey
		path = append(path, code)
		deps := append([]string(nil), steps[code].DependsOn...)
		sort.Strings(deps)
		for _, d := range deps {
			switch colour[d] {
			case grey:
				at := 0
				for i, c := range path {
					if c == d {
						at = i
						break
					}
				}
				return strings.Join(append(append([]string{}, path[at:]...), d), " -> ")
			case white:
				if found := walk(d); found != "" {
					return found
				}
			}
		}
		path = path[:len(path)-1]
		colour[code] = black
		return ""
	}

	for _, c := range codes {
		if colour[c] == white {
			if found := walk(c); found != "" {
				return found
			}
		}
	}
	return ""
}

// TaskState is where one task has got to.
type TaskState string

const (
	// TaskBlocked is waiting on another task. Not a synonym for pending: it says the
	// reason this cannot be done yet is another task, which is a sentence a screen
	// can show before somebody drives to the property.
	TaskBlocked TaskState = "blocked"
	// TaskPending is ready to be done.
	TaskPending TaskState = "pending"
	// TaskDone is done.
	TaskDone TaskState = "done"
	// TaskSkipped is a non-blocking step that did not apply, with a reason.
	TaskSkipped TaskState = "skipped"
)

// Settled reports whether the task no longer holds anything up.
func (s TaskState) Settled() bool { return s == TaskDone || s == TaskSkipped }

// Task is one materialised step of a fired checklist.
type Task struct {
	ID       string
	StepCode string
	Title    string
	Position int
	Blocking bool
	Owner    OwnerRole
	Assignee string
	DueOn    effective.Date
	State    TaskState
	// DependsOn holds step codes, matching the template. Ids would be more direct
	// and would mean the tasks could not be built in one pass.
	DependsOn []string
}

// State is where a checklist has got to.
type State string

const (
	StateOpen      State = "open"
	StateCompleted State = "completed"
	// StateAbandoned is a checklist somebody stopped, with a reason. The other kind
	// of abandonment — the one nobody declared — is derived, never stored: see
	// ADR-0032 §5 and the checklist_stalled view.
	StateAbandoned State = "abandoned"
)

// Checklist is one firing of a template.
type Checklist struct {
	ID       string
	TenantID string

	Process         Process
	TemplateID      string
	TemplateVersion int

	PropertyID string
	UnitID     string
	LeaseID    string

	// AnchorOn is the date every task's due date was computed from. Stored, because
	// a reader who cannot see it cannot say why a task is due on Tuesday.
	AnchorOn effective.Date
	State    State
	Tasks    []Task
}

// Event is what a checklist publishes. ADR-0001's naming —
// <module>.<aggregate>.<past-tense-verb> — and the seam anything that later needs a
// durable workflow consumes, rather than re-deriving the process.
type Event string

const (
	EventStarted       Event = "maintenance.checklist.started"
	EventCompleted     Event = "maintenance.checklist.completed"
	EventAbandoned     Event = "maintenance.checklist.abandoned"
	EventTaskCompleted Event = "maintenance.checklist_task.completed"
	// A skipped step publishes its own event rather than sharing the completed one:
	// "the inspection was done" and "the inspection did not apply" are different
	// facts, and a consumer that treats them alike is a consumer that reports a
	// process as performed when it was waived.
	EventTaskSkipped Event = "maintenance.checklist_task.skipped"
)

// Events returns every event this module publishes, ordered.
func Events() []Event {
	out := []Event{EventStarted, EventCompleted, EventAbandoned, EventTaskCompleted, EventTaskSkipped}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// ErrAnchorRequired is a checklist fired with no date to measure from.
var ErrAnchorRequired = errors.New("checklist: a checklist needs the date its steps are measured from")

// ErrBlocked is the story's failure scenario: something was attempted that a
// blocking step stands in front of. Distinguishable, because the caller's response
// is to show the outstanding steps rather than to retry.
var ErrBlocked = errors.New("checklist: a blocking step is outstanding")

// ErrDependency is a task finished out of order.
var ErrDependency = errors.New("checklist: a step this one waits on is not settled")

// Trigger materialises a template into a checklist. ADR-0032 §3.
//
// Every task is built here, in one pass, with its due date already computed: nothing
// is derived on read. A step with dependencies starts blocked rather than pending,
// so a screen can say why before somebody sets off.
func (t Template) Trigger(tenant string, anchor effective.Date, about Subject) (Checklist, error) {
	if err := t.Validate(); err != nil {
		return Checklist{}, err
	}
	if tenant == "" {
		return Checklist{}, errors.New("checklist: a checklist belonging to no organisation")
	}
	if anchor.Zero() {
		return Checklist{}, fmt.Errorf("%w: %s", ErrAnchorRequired, t.Process)
	}
	if about.PropertyID == "" {
		return Checklist{}, errors.New("checklist: a checklist must name the property it is about — " +
			"one that does not cannot be judged at unit granularity")
	}
	if t.Process.AboutATenancy() && about.LeaseID == "" {
		return Checklist{}, fmt.Errorf("checklist: a %s is about a tenancy and must name one", t.Process)
	}

	steps := append([]Step(nil), t.Steps...)
	sort.Slice(steps, func(i, j int) bool { return steps[i].Position < steps[j].Position })

	tasks := make([]Task, 0, len(steps))
	for _, s := range steps {
		state := TaskPending
		if len(s.DependsOn) > 0 {
			state = TaskBlocked
		}
		tasks = append(tasks, Task{
			StepCode:  s.Code,
			Title:     s.Title,
			Position:  s.Position,
			Blocking:  s.Blocking,
			Owner:     s.Owner,
			DueOn:     anchor.AddDays(s.DueOffsetDays),
			State:     state,
			DependsOn: append([]string(nil), s.DependsOn...),
		})
	}

	return Checklist{
		TenantID: tenant, Process: t.Process,
		TemplateID: t.ID, TemplateVersion: t.Version,
		PropertyID: about.PropertyID, UnitID: about.UnitID, LeaseID: about.LeaseID,
		AnchorOn: anchor, State: StateOpen, Tasks: tasks,
	}, nil
}

// Subject is what a checklist is about.
type Subject struct {
	PropertyID string
	UnitID     string
	LeaseID    string
}

// Outstanding returns the blocking tasks that are not settled, in order.
//
// This is what the lease module asks before closing a tenancy and what the refusal
// is built from. A completed or abandoned checklist holds nothing up.
func (c Checklist) Outstanding() []Task {
	if c.State != StateOpen {
		return nil
	}
	var out []Task
	for _, t := range c.Tasks {
		if t.Blocking && !t.State.Settled() {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out
}

// Titles renders tasks as a sentence fragment, for a refusal that names the steps
// rather than describing them.
func Titles(tasks []Task) string {
	names := make([]string, 0, len(tasks))
	for _, t := range tasks {
		names = append(names, t.Title)
	}
	return strings.Join(names, ", ")
}

// Complete marks one task done and releases whatever was waiting on it.
//
// Releasing here as well as in the schema's trigger is not duplication for its own
// sake: this returns the whole updated checklist, so a caller can render the graph's
// new shape from one response instead of reading it back.
func (c Checklist) Complete(stepCode string) (Checklist, error) {
	return c.settle(stepCode, TaskDone)
}

// Skip marks a non-blocking task skipped, with a reason.
func (c Checklist) Skip(stepCode, reason string) (Checklist, error) {
	if reason == "" {
		return c, errors.New("checklist: a skipped step must say why — it is the first question " +
			"in any dispute about the process")
	}
	return c.settle(stepCode, TaskSkipped)
}

func (c Checklist) settle(stepCode string, to TaskState) (Checklist, error) {
	if c.State != StateOpen {
		return c, fmt.Errorf("checklist: this checklist is %s, so its tasks are history", c.State)
	}

	at := -1
	for i, t := range c.Tasks {
		if strings.EqualFold(t.StepCode, stepCode) {
			at = i
			break
		}
	}
	if at < 0 {
		return c, fmt.Errorf("checklist: no step %q in this checklist", stepCode)
	}
	task := c.Tasks[at]
	if task.State.Settled() {
		return c, nil // a retried request asks for the state the task is already in
	}
	// A blocking step may not be skipped at all. ADR-0032 §4.
	if to == TaskSkipped && task.Blocking {
		return c, fmt.Errorf("%w: %s is a blocking step, so it cannot be skipped — if it does not "+
			"apply it should not be blocking", ErrBlocked, task.Title)
	}

	settled := map[string]bool{}
	for _, t := range c.Tasks {
		settled[strings.ToLower(t.StepCode)] = t.State.Settled()
	}
	var waiting []Task
	for _, d := range task.DependsOn {
		if !settled[strings.ToLower(d)] {
			for _, t := range c.Tasks {
				if strings.EqualFold(t.StepCode, d) {
					waiting = append(waiting, t)
				}
			}
		}
	}
	if len(waiting) > 0 {
		sort.Slice(waiting, func(i, j int) bool { return waiting[i].Position < waiting[j].Position })
		return c, fmt.Errorf("%w: %s cannot be done yet — it waits on %s",
			ErrDependency, task.Title, Titles(waiting))
	}

	out := c
	out.Tasks = append([]Task(nil), c.Tasks...)
	out.Tasks[at].State = to
	settled[strings.ToLower(task.StepCode)] = true

	// Release whatever was waiting only on this. Recomputed from the graph rather
	// than tracked, because a count maintained by one caller is a count the next
	// caller forgets to decrement.
	for i, t := range out.Tasks {
		if t.State != TaskBlocked {
			continue
		}
		ready := true
		for _, d := range t.DependsOn {
			if !settled[strings.ToLower(d)] {
				ready = false
				break
			}
		}
		if ready {
			out.Tasks[i].State = TaskPending
		}
	}
	return out, nil
}

// Finish closes the checklist, refusing while blocking steps are outstanding and
// naming them. The story's failure scenario.
func (c Checklist) Finish() (Checklist, error) {
	if c.State != StateOpen {
		return c, fmt.Errorf("checklist: this checklist is already %s", c.State)
	}
	if outstanding := c.Outstanding(); len(outstanding) > 0 {
		return c, fmt.Errorf("%w: this process is not finished — %s",
			ErrBlocked, Titles(outstanding))
	}
	out := c
	out.State = StateCompleted
	return out, nil
}

// Abandon stops a checklist, with a reason.
//
// Abandoning is a decision and it is recorded as one. The other kind — the checklist
// nobody decided about — is the derived one, and it is deliberately not reachable
// from here: a flag somebody has to set is a flag somebody forgets.
func (c Checklist) Abandon(reason string) (Checklist, error) {
	if c.State != StateOpen {
		return c, fmt.Errorf("checklist: this checklist is already %s", c.State)
	}
	if reason == "" {
		return c, errors.New("checklist: an abandoned checklist must say why, or it is " +
			"indistinguishable from one nobody looked at")
	}
	out := c
	out.State = StateAbandoned
	return out, nil
}
