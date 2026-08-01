package automation_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

const org = tenancy.ID("11111111-1111-1111-1111-111111111111")

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// memory is the store, in memory. The decisions worth testing are all in the
// engine — which rung, which ceiling, whether it has run before — and a database
// would only slow down asserting them.
type memory struct {
	overrides map[automation.Key]automation.Override
	runs      map[string]automation.Record
	approvals map[string]automation.ApprovalRequest
	saved     []automation.Override
}

func newMemory() *memory {
	return &memory{
		overrides: map[automation.Key]automation.Override{},
		runs:      map[string]automation.Record{},
		approvals: map[string]automation.ApprovalRequest{},
	}
}

func key(k automation.Key, idem string) string { return k.String() + "/" + idem }

func (m *memory) Overrides(context.Context) (map[automation.Key]automation.Override, error) {
	return m.overrides, nil
}

func (m *memory) Save(_ context.Context, k automation.Key, o automation.Override, _ string) error {
	m.overrides[k] = o
	m.saved = append(m.saved, o)
	return nil
}

func (m *memory) Recorded(_ context.Context, k automation.Key, idem string) (bool, error) {
	_, ok := m.runs[key(k, idem)]
	return ok, nil
}

func (m *memory) Record(_ context.Context, r automation.Record) (bool, error) {
	id := key(r.Automation, r.Key)
	if _, ok := m.runs[id]; ok {
		return false, nil
	}
	m.runs[id] = r
	return true, nil
}

func (m *memory) Requested(_ context.Context, k automation.Key, idem string) (bool, error) {
	_, ok := m.approvals[key(k, idem)]
	return ok, nil
}

func (m *memory) RequestApproval(_ context.Context, a automation.ApprovalRequest) (bool, error) {
	id := key(a.Automation, a.Key)
	if _, ok := m.approvals[id]; ok {
		return false, nil
	}
	m.approvals[id] = a
	return true, nil
}

type orgs []tenancy.ID

func (o orgs) Active(context.Context) ([]tenancy.ID, error) { return o, nil }

// counter is an automation that proposes one thing and counts how often the
// effect actually ran.
type counter struct {
	effects int
	amount  int64
	err     error
}

func (c *counter) definition(k automation.Key) automation.Definition {
	return automation.Definition{
		Key: k, Name: "Counter", Purpose: "Counts.",
		Trigger: automation.TriggerSchedule, EnabledByDefault: true,
		Params: []automation.Param{
			{Name: "after", Purpose: "days", Unit: "days", Default: 3, Min: 1, Max: 30},
		},
		Act: func(ctx context.Context, r *automation.Run) error {
			_, err := r.Propose(ctx, automation.Proposal{
				Subject:     automation.Subject{Kind: automation.SubjectLease, ID: "lease-1"},
				Action:      "counted",
				AmountMinor: c.amount,
				Key:         "lease-1:once",
				Do: func(context.Context) error {
					if c.err != nil {
						return c.err
					}
					c.effects++
					return nil
				},
			})
			if errors.Is(err, automation.ErrApprovalRequired) {
				return nil
			}
			return err
		},
	}
}

func runnerFor(t *testing.T, m *memory, defs ...automation.Definition) *automation.Runner {
	t.Helper()
	r, err := automation.NewRunner(defs, orgs{org}, m, quiet())
	if err != nil {
		t.Fatalf("wiring the runner: %v", err)
	}
	return r
}

func scoped() context.Context { return tenancy.With(context.Background(), org) }

// The story's primary scenario: an organisation that has configured nothing runs
// every automation on its defaults. ADR-0033 §1.
func TestAnOrganisationWithNoRowsRunsEverything(t *testing.T) {
	m := newMemory()
	c := &counter{}
	r := runnerFor(t, m, c.definition("arrears_ladder"))

	res, err := r.RunFor(scoped())
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if res.Automations != 1 || res.Acted != 1 {
		t.Fatalf("ran %d automations and acted %d times, want 1 and 1 — an organisation that "+
			"has configured nothing must still be running", res.Automations, res.Acted)
	}
	if c.effects != 1 {
		t.Fatalf("the effect ran %d times", c.effects)
	}
	if len(m.overrides) != 0 {
		t.Error("running wrote a settings row — settings are overrides, and a default that " +
			"lives in a row is a default somebody can delete")
	}
}

// "Switchable off per organisation without a release" is one row.
func TestSwitchingOneOffStopsOnlyThatOne(t *testing.T) {
	m := newMemory()
	a, b := &counter{}, &counter{}
	r := runnerFor(t, m, a.definition("arrears_ladder"), b.definition("lease_expiry_reminder"))

	off := false
	if err := r.Set(scoped(), "arrears_ladder", automation.Override{Enabled: &off}, ""); err != nil {
		t.Fatalf("switching it off: %v", err)
	}
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if a.effects != 0 {
		t.Error("the automation that was switched off still ran")
	}
	if b.effects != 1 {
		t.Error("switching one automation off stopped another")
	}
}

// A parameter override changes what the automation reads, and the catalogue's
// bounds are what it is checked against.
func TestParametersAreOverriddenAndBounded(t *testing.T) {
	m := newMemory()
	var seen int64
	def := automation.Definition{
		Key: "arrears_ladder", Name: "n", Purpose: "p",
		Trigger: automation.TriggerSchedule, EnabledByDefault: true,
		Params: []automation.Param{{Name: "after", Default: 3, Min: 1, Max: 30}},
		Act: func(_ context.Context, r *automation.Run) error {
			seen = r.Param("after")
			return nil
		},
	}
	r := runnerFor(t, m, def)

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if seen != 3 {
		t.Fatalf("the default resolved to %d, want 3", seen)
	}

	if err := r.Set(scoped(), "arrears_ladder",
		automation.Override{Params: map[string]int64{"after": 9}}, ""); err != nil {
		t.Fatalf("setting a parameter: %v", err)
	}
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if seen != 9 {
		t.Fatalf("the override resolved to %d, want 9", seen)
	}

	// Outside the range, and named parameters only.
	err := r.Set(scoped(), "arrears_ladder",
		automation.Override{Params: map[string]int64{"after": 99}}, "")
	if err == nil || !strings.Contains(err.Error(), "between 1 and 30") {
		t.Errorf("setting a parameter outside its range gave %v — the refusal must say the bounds", err)
	}
	err = r.Set(scoped(), "arrears_ladder",
		automation.Override{Params: map[string]int64{"vibes": 1}}, "")
	if err == nil || !strings.Contains(err.Error(), "vibes") {
		t.Errorf("setting an undeclared parameter gave %v", err)
	}
	if err := r.Set(scoped(), "nonesuch", automation.Override{}, ""); err == nil {
		t.Error("an automation that does not exist was configured")
	}
}

// The idempotency key, which is what makes an overrunning CronJob harmless.
func TestTheSameProposalActsOnce(t *testing.T) {
	m := newMemory()
	c := &counter{}
	r := runnerFor(t, m, c.definition("arrears_ladder"))

	for i := 0; i < 3; i++ {
		if _, err := r.RunFor(scoped()); err != nil {
			t.Fatal(err)
		}
	}
	if c.effects != 1 {
		t.Fatalf("three runs performed the effect %d times, want 1 — a tenancy would have had "+
			"three identical reminders", c.effects)
	}
}

// The story's failure scenario: over the ceiling, nothing performed, an approval
// requested that names what it wanted and what stopped it.
func TestOverTheCeilingItAsksAndDoesNothing(t *testing.T) {
	m := newMemory()
	c := &counter{amount: 50_000}
	def := c.definition("arrears_ladder")
	def.CeilingMinor = 10_000
	r := runnerFor(t, m, def)

	res, err := r.RunFor(scoped())
	if err != nil {
		t.Fatalf("running: %v", err)
	}
	if c.effects != 0 {
		t.Fatal("the effect ran despite exceeding the ceiling")
	}
	if res.Awaiting != 1 {
		t.Fatalf("%d proposals awaiting approval, want 1", res.Awaiting)
	}
	if len(m.approvals) != 1 {
		t.Fatalf("%d approvals requested, want 1 — an automation that stops and says nothing "+
			"is indistinguishable from one that is switched off", len(m.approvals))
	}
	for _, a := range m.approvals {
		if a.Amount != 50_000 || a.Ceiling != 10_000 {
			t.Errorf("the request says %d over %d, want 50000 over 10000 — 'over the limit' "+
				"without the limit is not something anybody can act on", a.Amount, a.Ceiling)
		}
	}

	// And it does not ask twice while somebody is deciding.
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if len(m.approvals) != 1 {
		t.Errorf("a second run produced %d approvals — a manager with two identical requests "+
			"has no idea whether granting one is enough", len(m.approvals))
	}
}

// Raising the ceiling releases the proposal, without a second code path.
func TestRaisingTheCeilingLetsItAct(t *testing.T) {
	m := newMemory()
	c := &counter{amount: 50_000}
	def := c.definition("arrears_ladder")
	def.CeilingMinor = 10_000
	r := runnerFor(t, m, def)

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	raised := int64(100_000)
	if err := r.Set(scoped(), "arrears_ladder",
		automation.Override{CeilingMinor: &raised}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if c.effects != 1 {
		t.Fatalf("the effect ran %d times after the ceiling was raised, want 1", c.effects)
	}
}

// A proposal carrying no money never needs an approval, whatever the ceiling.
func TestAProposalWithNoMoneyIsNeverStopped(t *testing.T) {
	m := newMemory()
	c := &counter{}
	def := c.definition("arrears_ladder")
	def.CeilingMinor = 0
	r := runnerFor(t, m, def)

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if c.effects != 1 {
		t.Fatal("a proposal moving no money was stopped by a zero ceiling")
	}
}

// A failing automation is recorded and not retried. Retrying a failing action
// every hour is how one broken automation becomes an outage.
func TestAFailureIsRecordedAndNotRetried(t *testing.T) {
	m := newMemory()
	c := &counter{err: errors.New("the provider is down")}
	r := runnerFor(t, m, c.definition("arrears_ladder"))

	res, err := r.RunFor(scoped())
	if err != nil {
		t.Fatalf("a failing automation stopped the run: %v", err)
	}
	if res.Failed != 1 {
		t.Errorf("%d failures reported, want 1", res.Failed)
	}
	var found bool
	for _, run := range m.runs {
		if run.Outcome == automation.OutcomeFailed {
			found = true
			if !strings.Contains(run.Detail, "provider is down") {
				t.Errorf("the failed run says %q and does not say why", run.Detail)
			}
		}
	}
	if !found {
		t.Fatal("the failure was not recorded, so nobody can find out it happened")
	}

	c.err = nil
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if c.effects != 0 {
		t.Error("a failed proposal was retried on the next pass")
	}
}

// One automation erroring must not stop the other five.
func TestOneBrokenAutomationDoesNotStopTheRest(t *testing.T) {
	m := newMemory()
	good := &counter{}
	broken := automation.Definition{
		Key: "broken_one", Name: "n", Purpose: "p",
		Trigger: automation.TriggerSchedule, EnabledByDefault: true,
		Act: func(context.Context, *automation.Run) error {
			return errors.New("this one is wrong")
		},
	}
	r := runnerFor(t, m, broken, good.definition("arrears_ladder"))

	res, err := r.RunFor(scoped())
	if err != nil {
		t.Fatalf("a broken automation failed the whole pass: %v", err)
	}
	if good.effects != 1 {
		t.Error("a broken automation stopped a working one")
	}
	if res.Failed != 1 {
		t.Errorf("%d failures reported, want 1", res.Failed)
	}
}

// Every run happens inside an organisation's session, and one with no tenant is
// refused rather than running as nobody.
func TestARunNeedsAnOrganisation(t *testing.T) {
	m := newMemory()
	c := &counter{}
	r := runnerFor(t, m, c.definition("arrears_ladder"))

	if _, err := r.RunFor(context.Background()); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Fatalf("running with no organisation gave %v, want ErrNoTenant", err)
	}
}

// Event-triggered automations run for the event they name and no other.
func TestAnEventTriggeredAutomationRunsOnItsOwnEvent(t *testing.T) {
	m := newMemory()
	var ran []string
	def := automation.Definition{
		Key: "move_in_checklist", Name: "n", Purpose: "p",
		Trigger: automation.TriggerEvent, On: "lease.tenancy.started", EnabledByDefault: true,
		Act: func(_ context.Context, r *automation.Run) error {
			ran = append(ran, r.Event.Type+":"+r.Event.Data["property_id"])
			return nil
		},
	}
	r := runnerFor(t, m, def)

	subject := automation.Subject{Kind: automation.SubjectLease, ID: "lease-1"}
	if err := r.Handle(scoped(), "lease.notice.served", subject, nil); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 0 {
		t.Fatalf("it ran on an event it does not name: %v", ran)
	}
	if err := r.Handle(scoped(), "lease.tenancy.started", subject,
		map[string]string{"property_id": "prop-1"}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0] != "lease.tenancy.started:prop-1" {
		t.Fatalf("ran %v, want one run carrying the event's data", ran)
	}

	// And a scheduled pass does not run it: the trigger is the event.
	if res, err := r.RunFor(scoped()); err != nil || res.Automations != 0 {
		t.Errorf("the scheduled pass ran %d event-triggered automations (%v)", res.Automations, err)
	}
}

// The catalogue is checked at startup, because an automation that never fires is
// invisible and a process that refuses to start is not.
func TestTheCatalogueIsValidatedBeforeAnythingRuns(t *testing.T) {
	cases := map[string]automation.Definition{
		"no purpose": {Key: "a_key", Name: "n", Trigger: automation.TriggerSchedule,
			Act: func(context.Context, *automation.Run) error { return nil }},
		"no action": {Key: "a_key", Name: "n", Purpose: "p", Trigger: automation.TriggerSchedule},
		"event with no event": {Key: "a_key", Name: "n", Purpose: "p",
			Trigger: automation.TriggerEvent,
			Act:     func(context.Context, *automation.Run) error { return nil }},
		"scheduled with an event": {Key: "a_key", Name: "n", Purpose: "p",
			Trigger: automation.TriggerSchedule, On: "lease.tenancy.started",
			Act: func(context.Context, *automation.Run) error { return nil }},
		"a default outside its own bounds": {Key: "a_key", Name: "n", Purpose: "p",
			Trigger: automation.TriggerSchedule,
			Params:  []automation.Param{{Name: "p", Default: 99, Min: 1, Max: 30}},
			Act:     func(context.Context, *automation.Run) error { return nil }},
		"a key the schema would refuse": {Key: "Not A Key", Name: "n", Purpose: "p",
			Trigger: automation.TriggerSchedule,
			Act:     func(context.Context, *automation.Run) error { return nil }},
	}
	for name, def := range cases {
		t.Run(name, func(t *testing.T) {
			if err := (automation.Catalogue{def}).Validate(); err == nil {
				t.Fatalf("a catalogue with %s validated", name)
			}
		})
	}

	duplicate := automation.Catalogue{
		{Key: "a_key", Name: "n", Purpose: "p", Trigger: automation.TriggerSchedule,
			Act: func(context.Context, *automation.Run) error { return nil }},
		{Key: "a_key", Name: "m", Purpose: "q", Trigger: automation.TriggerSchedule,
			Act: func(context.Context, *automation.Run) error { return nil }},
	}
	if err := duplicate.Validate(); err == nil {
		t.Error("two automations with one key validated")
	}
}

// Resolve says what was customised, so a screen can show it.
func TestResolveReportsWhatWasOverridden(t *testing.T) {
	def := automation.Definition{
		Key: "arrears_ladder", EnabledByDefault: true, CeilingMinor: 1000,
		Params: []automation.Param{
			{Name: "after", Default: 3, Min: 1, Max: 30},
			{Name: "floor", Default: 100, Min: 0, Max: 1000},
		},
	}

	plain := automation.Resolve(def, automation.Override{})
	if !plain.Enabled || plain.Param("after") != 3 || len(plain.Overridden) != 0 {
		t.Fatalf("an unconfigured automation resolved to %+v", plain)
	}

	off := false
	ceiling := int64(9000)
	changed := automation.Resolve(def, automation.Override{
		Enabled: &off, CeilingMinor: &ceiling, Params: map[string]int64{"after": 7},
	})
	if changed.Enabled || changed.Param("after") != 7 || changed.CeilingMinor != 9000 {
		t.Fatalf("an overridden automation resolved to %+v", changed)
	}
	want := "after, approval_ceiling_minor, enabled"
	if got := strings.Join(changed.Overridden, ", "); got != want {
		t.Errorf("reported %q as customised, want %q", got, want)
	}
	// The parameter that was not changed is still the default and is not reported.
	if changed.Param("floor") != 100 {
		t.Error("an untouched parameter did not resolve to its default")
	}
}

// A stored value outside bounds tightened by a release is clamped rather than
// refused: refusing would switch the automation off without anybody deciding to.
func TestAStoredValueOutsideNewBoundsIsClamped(t *testing.T) {
	def := automation.Definition{
		Key: "arrears_ladder", EnabledByDefault: true,
		Params: []automation.Param{{Name: "after", Default: 3, Min: 1, Max: 10}},
	}
	s := automation.Resolve(def, automation.Override{Params: map[string]int64{"after": 400}})
	if s.Param("after") != 10 {
		t.Fatalf("a stored 400 against a max of 10 resolved to %d, want 10", s.Param("after"))
	}
	if !s.Enabled {
		t.Error("clamping a parameter switched the automation off")
	}
}

// RunAll enumerates organisations once and does the work inside each one's
// session. ADR-0028 §3.
func TestRunAllVisitsEveryOrganisationInItsOwnSession(t *testing.T) {
	m := newMemory()
	var seen []tenancy.ID
	def := automation.Definition{
		Key: "arrears_ladder", Name: "n", Purpose: "p",
		Trigger: automation.TriggerSchedule, EnabledByDefault: true,
		Act: func(ctx context.Context, _ *automation.Run) error {
			id, ok := tenancy.From(ctx)
			if !ok {
				t.Error("an automation ran with no organisation in its context")
			}
			seen = append(seen, id)
			return nil
		},
	}
	second := tenancy.ID("22222222-2222-2222-2222-222222222222")
	r, err := automation.NewRunner(automation.Catalogue{def}, orgs{org, second}, m, quiet())
	if err != nil {
		t.Fatal(err)
	}

	res, err := r.RunAll(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Organisations != 2 {
		t.Fatalf("visited %d organisations, want 2", res.Organisations)
	}
	if len(seen) != 2 || seen[0] != org || seen[1] != second {
		t.Fatalf("ran for %v, want each organisation once in its own session", seen)
	}
}
