package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

const org = "11111111-1111-1111-1111-111111111111"

func date(t *testing.T, s string) effective.Date {
	t.Helper()
	d, err := effective.ParseDate(s)
	if err != nil {
		t.Fatalf("parsing %s: %v", s, err)
	}
	return d
}

// moveOut is the shape the story describes: keys, then a reading, then the money,
// with an optional photograph step that does not hold anything up.
func moveOut() domain.Template {
	return domain.Template{
		ID: "t-1", TenantID: org, Process: domain.ProcessMoveOut, Name: "Move-out", Version: 1,
		Steps: []domain.Step{
			{Code: "keys", Title: "Collect the keys", Position: 1, Blocking: true, Owner: "field_agent"},
			{Code: "meter", Title: "Final meter reading", Position: 2, Blocking: true,
				Owner: "field_agent", DueOffsetDays: 1, DependsOn: []string{"keys"}},
			{Code: "deposit", Title: "Settle the deposit", Position: 3, Blocking: true,
				Owner: "accountant", DueOffsetDays: 7, DependsOn: []string{"meter"}},
			{Code: "photos", Title: "Exit photographs", Position: 4,
				Owner: "field_agent", DependsOn: []string{"keys"}},
		},
	}
}

func fired(t *testing.T) domain.Checklist {
	t.Helper()
	c, err := moveOut().Trigger(org, date(t, "2026-06-30"),
		domain.Subject{PropertyID: "p-1", UnitID: "u-1", LeaseID: "l-1"})
	if err != nil {
		t.Fatalf("firing the move-out: %v", err)
	}
	return c
}

// The story's primary scenario: one action, every task created with an owner and a
// due date, and the ones that wait on something saying so.
func TestFiringAChecklistDatesEveryStepFromTheAnchor(t *testing.T) {
	c := fired(t)

	if len(c.Tasks) != 4 {
		t.Fatalf("fired %d tasks, want 4 — one action must create the whole process", len(c.Tasks))
	}
	want := map[string]struct {
		due   string
		state domain.TaskState
		owner domain.OwnerRole
	}{
		"keys":    {"2026-06-30", domain.TaskPending, "field_agent"},
		"meter":   {"2026-07-01", domain.TaskBlocked, "field_agent"},
		"deposit": {"2026-07-07", domain.TaskBlocked, "accountant"},
		"photos":  {"2026-06-30", domain.TaskBlocked, "field_agent"},
	}
	for _, task := range c.Tasks {
		w, ok := want[task.StepCode]
		if !ok {
			t.Fatalf("unexpected step %s", task.StepCode)
		}
		if got := task.DueOn.String(); got != w.due {
			t.Errorf("%s is due %s, want %s — the offset is measured from the anchor", task.StepCode, got, w.due)
		}
		if task.State != w.state {
			t.Errorf("%s starts %s, want %s — a task waiting on another must say so before "+
				"somebody drives to the property", task.StepCode, task.State, w.state)
		}
		if task.Owner != w.owner {
			t.Errorf("%s is owned by %s, want %s", task.StepCode, task.Owner, w.owner)
		}
	}
}

// A negative offset is "three days before the tenant leaves", which is most of a
// move-in and half of a renewal.
func TestAnOffsetMayBeNegative(t *testing.T) {
	tpl := moveOut()
	tpl.Steps[0].DueOffsetDays = -3
	c, err := tpl.Trigger(org, date(t, "2026-06-30"), domain.Subject{PropertyID: "p-1", LeaseID: "l-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := c.Tasks[0].DueOn.String(); got != "2026-06-27" {
		t.Fatalf("a -3 offset produced %s, want 2026-06-27", got)
	}
}

// Dating from today would be right on the day it was fired and wrong every day
// after, which is the kind of bug nobody reports because every screen agrees.
func TestAChecklistWithNoAnchorIsRefused(t *testing.T) {
	_, err := moveOut().Trigger(org, effective.Date{}, domain.Subject{PropertyID: "p-1", LeaseID: "l-1"})
	if !errors.Is(err, domain.ErrAnchorRequired) {
		t.Fatalf("firing with no anchor gave %v, want ErrAnchorRequired", err)
	}
}

func TestATenancyProcessMustNameATenancy(t *testing.T) {
	_, err := moveOut().Trigger(org, date(t, "2026-06-30"), domain.Subject{PropertyID: "p-1"})
	if err == nil {
		t.Fatal("a move-out with no lease was accepted — the gate would then have nothing to attach to")
	}
	if !strings.Contains(err.Error(), "tenancy") {
		t.Errorf("the refusal is %q and does not say what is missing", err)
	}
}

// Ticking the last box first records a process that did not happen.
func TestATaskCannotBeDoneBeforeWhatItWaitsOn(t *testing.T) {
	c := fired(t)

	_, err := c.Complete("deposit")
	if !errors.Is(err, domain.ErrDependency) {
		t.Fatalf("settling the deposit first gave %v, want ErrDependency", err)
	}
	if !strings.Contains(err.Error(), "Final meter reading") {
		t.Errorf("the refusal is %q — it must name the step being waited on, or nobody knows "+
			"what to do next", err)
	}
}

func TestSettlingAStepReleasesWhatWasWaitingOnlyOnIt(t *testing.T) {
	c := fired(t)

	after, err := c.Complete("keys")
	if err != nil {
		t.Fatalf("collecting the keys: %v", err)
	}
	for _, task := range after.Tasks {
		want := domain.TaskBlocked
		switch task.StepCode {
		case "keys":
			want = domain.TaskDone
		case "meter", "photos":
			want = domain.TaskPending
		}
		if task.State != want {
			t.Errorf("after the keys, %s is %s, want %s", task.StepCode, task.State, want)
		}
	}
	// The original is untouched: a caller holding the previous value must not see
	// it change under them.
	if c.Tasks[0].State != domain.TaskPending {
		t.Error("Complete mutated the checklist it was called on")
	}
}

// If a step is skippable it was not blocking, and one skipped blocking step makes
// every gate advisory. ADR-0032 §4.
func TestABlockingStepCannotBeSkipped(t *testing.T) {
	c := fired(t)

	_, err := c.Skip("keys", "no time")
	if !errors.Is(err, domain.ErrBlocked) {
		t.Fatalf("skipping a blocking step gave %v, want ErrBlocked", err)
	}

	after, err := c.Complete("keys")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := after.Skip("photos", ""); err == nil {
		t.Error("a step was skipped with no reason — a skip nobody explained is a step nobody did")
	}
	if _, err := after.Skip("photos", "the camera failed"); err != nil {
		t.Errorf("skipping a non-blocking step with a reason was refused: %v", err)
	}
}

// The story's failure scenario: refused, naming the outstanding step.
func TestAChecklistWillNotFinishWhileBlockingStepsAreOutstanding(t *testing.T) {
	c := fired(t)

	_, err := c.Finish()
	if !errors.Is(err, domain.ErrBlocked) {
		t.Fatalf("finishing an unfinished process gave %v, want ErrBlocked", err)
	}
	for _, want := range []string{"Collect the keys", "Final meter reading", "Settle the deposit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is %q and does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Exit photographs") {
		t.Error("the refusal names a step that is not blocking, so it reads as more work than there is")
	}

	for _, step := range []string{"keys", "meter", "deposit"} {
		if c, err = c.Complete(step); err != nil {
			t.Fatalf("settling %s: %v", step, err)
		}
	}
	if len(c.Outstanding()) != 0 {
		t.Fatalf("%d blocking steps outstanding after all three were done", len(c.Outstanding()))
	}
	// The optional photograph is still pending, and it does not hold the process.
	done, err := c.Finish()
	if err != nil {
		t.Fatalf("finishing with only a non-blocking step outstanding was refused: %v", err)
	}
	if done.State != domain.StateCompleted {
		t.Fatalf("the checklist is %s after finishing", done.State)
	}
}

// A finished process holds nothing up, and neither does an abandoned one — but the
// abandonment says why, or it is indistinguishable from one nobody looked at.
func TestAbandoningSaysWhy(t *testing.T) {
	c := fired(t)

	if _, err := c.Abandon(""); err == nil {
		t.Error("a checklist was abandoned with no reason")
	}
	gone, err := c.Abandon("the tenant withdrew their notice")
	if err != nil {
		t.Fatal(err)
	}
	if gone.State != domain.StateAbandoned {
		t.Fatalf("the checklist is %s", gone.State)
	}
	if len(gone.Outstanding()) != 0 {
		t.Error("an abandoned checklist still reports outstanding work, so it would keep a " +
			"tenancy from closing forever")
	}
}

// A retried request asks for the state the task is already in. ADR-0011 §3's rule,
// applied here for the same reason: at-least-once delivery and impatient people.
func TestSettlingASettledTaskIsANoOp(t *testing.T) {
	c := fired(t)
	after, err := c.Complete("keys")
	if err != nil {
		t.Fatal(err)
	}
	again, err := after.Complete("keys")
	if err != nil {
		t.Fatalf("settling the same step twice gave %v, want no error", err)
	}
	if again.Tasks[0].State != domain.TaskDone {
		t.Error("the second settlement changed the state")
	}
}

// ADR-0032 §2. The order is the whole feature: a firm managing a tower and a
// co-living block has one organisation and needs two lists.
func TestResolutionPrefersTheMostSpecificTemplate(t *testing.T) {
	lib := func(id string, kind domain.PropertyKind, dflt bool, version int) domain.Template {
		owner := org
		if dflt {
			owner = "00000000-0000-0000-0000-0000000000d8"
		}
		return domain.Template{ID: id, TenantID: owner, Default: dflt,
			Process: domain.ProcessMoveOut, Kind: kind, Name: id, Version: version}
	}
	all := []domain.Template{
		lib("default-any", "", true, 1),
		lib("default-coliving", "coliving", true, 1),
		lib("ours-any", "", false, 1),
		lib("ours-coliving", "coliving", false, 1),
	}

	cases := []struct {
		name       string
		candidates []domain.Template
		kind       domain.PropertyKind
		want       string
	}{
		{"ours for this kind beats everything", all, "coliving", "ours-coliving"},
		{"ours for any kind beats the library", all, "building", "ours-any"},
		{"the library's kind beats its any", all[:2], "coliving", "default-coliving"},
		{"the library's any is the floor", all[:2], "building", "default-any"},
		{"a firm with nothing configured still fires something",
			[]domain.Template{lib("default-any", "", true, 1)}, "commercial", "default-any"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := domain.Resolve(c.candidates, org, domain.ProcessMoveOut, c.kind)
			if err != nil {
				t.Fatalf("resolving: %v", err)
			}
			if got.ID != c.want {
				t.Errorf("resolved %s, want %s", got.ID, c.want)
			}
		})
	}

	if _, err := domain.Resolve(nil, org, domain.ProcessMoveOut, "building"); !errors.Is(err, domain.ErrNoTemplate) {
		t.Error("resolving with no candidates did not report that there is nothing to fire")
	}
}

// A newer published version of the same rung wins without the old one being retired
// first, which is what makes publishing a change a single write.
func TestALaterVersionOfTheSameRungWins(t *testing.T) {
	candidates := []domain.Template{
		{ID: "v1", TenantID: org, Process: domain.ProcessMoveOut, Version: 1},
		{ID: "v2", TenantID: org, Process: domain.ProcessMoveOut, Version: 2},
	}
	got, err := domain.Resolve(candidates, org, domain.ProcessMoveOut, "building")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "v2" {
		t.Fatalf("resolved %s, want v2", got.ID)
	}
}

// A cycle is a checklist that can never complete and therefore a tenancy that can
// never close. It has to fail when the template is written, not weeks later.
func TestATemplateThatCyclesIsRefused(t *testing.T) {
	tpl := moveOut()
	tpl.Steps[0].DependsOn = []string{"deposit"} // keys <- deposit <- meter <- keys

	err := tpl.Validate()
	if !errors.Is(err, domain.ErrTemplate) {
		t.Fatalf("a cyclic template validated: %v", err)
	}
	if !strings.Contains(err.Error(), "circle") {
		t.Errorf("the refusal is %q and does not say what is wrong", err)
	}
	// The path is printed so an author knows which edge to remove.
	for _, want := range []string{"keys", "meter", "deposit"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is %q and does not name %s", err, want)
		}
	}
}

func TestATemplateIsRefusedForTheOrdinaryMistakes(t *testing.T) {
	cases := map[string]func(*domain.Template){
		"a dependency on a step that does not exist": func(tpl *domain.Template) {
			tpl.Steps[0].DependsOn = []string{"nowhere"}
		},
		"a step that waits for itself": func(tpl *domain.Template) {
			tpl.Steps[0].DependsOn = []string{"keys"}
		},
		"two steps with one code": func(tpl *domain.Template) {
			tpl.Steps[1].Code = "keys"
		},
		"two steps at one position": func(tpl *domain.Template) {
			tpl.Steps[1].Position = 1
		},
		"an owner that is not a role": func(tpl *domain.Template) {
			tpl.Steps[0].Owner = "the caretaker's cousin"
		},
		"no steps at all": func(tpl *domain.Template) {
			tpl.Steps = nil
		},
		"a process nobody has heard of": func(tpl *domain.Template) {
			tpl.Process = "vibes"
		},
	}
	for name, break_ := range cases {
		t.Run(name, func(t *testing.T) {
			tpl := moveOut()
			break_(&tpl)
			if err := tpl.Validate(); !errors.Is(err, domain.ErrTemplate) {
				t.Fatalf("validated %s: %v", name, err)
			}
		})
	}
}

// Which processes gate which transitions. A move-in that is half done is somebody
// else's problem; a move-out that is half done means the deposit is being settled
// against an inspection nobody did.
func TestOnlyTheMoveOutGatesTheClose(t *testing.T) {
	for _, to := range []string{"terminated", "settled"} {
		if !domain.ProcessMoveOut.Gates(to) {
			t.Errorf("the move-out does not gate %s", to)
		}
	}
	for _, p := range domain.Processes() {
		if p == domain.ProcessMoveOut {
			continue
		}
		if p.Gates("terminated") {
			t.Errorf("%s gates a termination, which would stop a tenancy closing for an "+
				"unrelated process", p)
		}
	}
	if domain.ProcessMoveOut.Gates("in_notice") {
		t.Error("the move-out gates in_notice — serving notice is not closing a tenancy, and " +
			"blocking it would mean the checklist has to be done before it can be started")
	}
}
