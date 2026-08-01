package routine_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/routine"
)

// The catalogue's own decisions, without a database: which rung a tenancy has
// reached, whether a checklist is fired once, and what the reminder says. These
// are the parts that would otherwise only be discovered by a customer.

const org = tenancy.ID("11111111-1111-1111-1111-111111111111")

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func scoped() context.Context {
	return tenancy.With(context.Background(), org)
}

func on(t *testing.T, s string) effective.Date {
	t.Helper()
	d, err := effective.ParseDate(s)
	if err != nil {
		t.Fatalf("parsing %s: %v", s, err)
	}
	return d
}

// --- the fakes -------------------------------------------------------------

type fakeTenancies struct {
	live     []routine.Tenancy
	expiring []routine.ExpiringTenancy
	tenant   string
}

func (f *fakeTenancies) Live(context.Context, int) ([]routine.Tenancy, error) { return f.live, nil }
func (f *fakeTenancies) Expiring(context.Context, int) ([]routine.ExpiringTenancy, error) {
	return f.expiring, nil
}
func (f *fakeTenancies) PartiesOf(_ context.Context, _ string, _ effective.Date) (string, string, error) {
	return f.tenant, "owner-1", nil
}

type fakeMoney struct {
	owed      int64
	chargedOn effective.Date
}

func (f fakeMoney) OutstandingMinor(context.Context, string, string) (int64, error) {
	return f.owed, nil
}
func (f fakeMoney) LastChargedOn(context.Context, string) (effective.Date, error) {
	return f.chargedOn, nil
}

type fakeChecklists struct{ started []string }

func (f *fakeChecklists) Start(_ context.Context, process, _, _, _, leaseID string,
	anchor effective.Date) (string, error) {
	f.started = append(f.started, process+":"+leaseID+":"+anchor.String())
	return "checklist-" + process, nil
}

type fakeCompliance struct{ rows []routine.ExpiringCertificate }

func (f fakeCompliance) ExpiringCertificates(context.Context, effective.Date, int) ([]routine.ExpiringCertificate, error) {
	return f.rows, nil
}

type fakeEvents struct{ published []map[string]any }

func (f *fakeEvents) Publish(_ context.Context, typ string, _ automation.Subject, data map[string]any) error {
	e := map[string]any{"type": typ}
	for k, v := range data {
		e[k] = v
	}
	f.published = append(f.published, e)
	return nil
}

// memory is the engine's store, in memory. Copied in shape from the engine's own
// test rather than shared, because a helper shared between two packages' tests is
// a helper that grows options for both.
type memory struct {
	runs      map[string]automation.Record
	approvals map[string]automation.ApprovalRequest
}

func newMemory() *memory {
	return &memory{
		runs:      map[string]automation.Record{},
		approvals: map[string]automation.ApprovalRequest{},
	}
}

func (m *memory) Overrides(context.Context) (map[automation.Key]automation.Override, error) {
	return map[automation.Key]automation.Override{}, nil
}
func (m *memory) Save(context.Context, automation.Key, automation.Override, string) error { return nil }
func (m *memory) Recorded(_ context.Context, k automation.Key, idem string) (bool, error) {
	_, ok := m.runs[k.String()+"/"+idem]
	return ok, nil
}
func (m *memory) Record(_ context.Context, r automation.Record) (bool, error) {
	id := r.Automation.String() + "/" + r.Key
	if _, ok := m.runs[id]; ok {
		return false, nil
	}
	m.runs[id] = r
	return true, nil
}
func (m *memory) Requested(_ context.Context, k automation.Key, idem string) (bool, error) {
	_, ok := m.approvals[k.String()+"/"+idem]
	return ok, nil
}
func (m *memory) RequestApproval(_ context.Context, a automation.ApprovalRequest) (bool, error) {
	m.approvals[a.Automation.String()+"/"+a.Key] = a
	return true, nil
}

type noOrgs struct{}

func (noOrgs) Active(context.Context) ([]tenancy.ID, error) { return []tenancy.ID{org}, nil }

func harness(t *testing.T, d routine.Deps) (*automation.Runner, *memory) {
	t.Helper()
	m := newMemory()
	r, err := automation.NewRunner(routine.Catalogue(d), noOrgs{}, m, quiet())
	if err != nil {
		t.Fatalf("the prebuilt catalogue does not validate: %v", err)
	}
	return r, m
}

// --- the tests -------------------------------------------------------------

// The shipped catalogue is checked the way the process checks it at startup.
func TestThePrebuiltCatalogueValidates(t *testing.T) {
	c := routine.Catalogue(routine.Deps{Now: func() effective.Date { return effective.Day(2026, 8, 1) }})
	if err := c.Validate(); err != nil {
		t.Fatalf("the shipped catalogue does not validate: %v", err)
	}
	if len(c) != 8 {
		t.Errorf("the catalogue ships %d automations, want 8", len(c))
	}
	for _, d := range c {
		if !d.EnabledByDefault {
			t.Errorf("%s ships switched off — the story's primary scenario is that a new "+
				"organisation gets these running with no configuration", d.Key)
		}
	}
	if len(c.Scheduled()) != 5 {
		t.Errorf("%d scheduled automations, want 5", len(c.Scheduled()))
	}
	for _, e := range []string{"lease.tenancy.started", "lease.notice.served", "identity.organisation.created"} {
		if len(c.For(e)) != 1 {
			t.Errorf("%d automations react to %s, want 1", len(c.For(e)), e)
		}
	}
}

// The ladder climbs one rung at a time and stops.
func TestTheArrearsLadderClimbsOneRungAtATime(t *testing.T) {
	today := effective.Day(2026, 8, 1)
	money := fakeMoney{owed: 25_000_00, chargedOn: on(t, "2026-07-29")} // three days ago
	events := &fakeEvents{}
	r, _ := harness(t, routine.Deps{
		Now: func() effective.Date { return today },
		Tenancies: &fakeTenancies{
			live:   []routine.Tenancy{{ID: "lease-1", PropertyID: "p1", UnitID: "u1", StartedOn: on(t, "2026-01-01")}},
			tenant: "party-1",
		},
		Money: money, Checklists: &fakeChecklists{}, Compliance: fakeCompliance{}, Events: events,
	})

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatalf("running: %v", err)
	}
	reminders := ofType(events.published, "notify.arrears.reminded")
	if len(reminders) != 1 {
		t.Fatalf("%d arrears reminders on day three, want 1", len(reminders))
	}
	if reminders[0]["step"] != "first" {
		t.Errorf("the reminder is the %v step, want the first", reminders[0]["step"])
	}
	// The activity feed reads the outbox back, so the event has to say which
	// automation caused it or the record's line is "a reminder was sent".
	if reminders[0]["automation"] != "arrears_ladder" {
		t.Errorf("the event does not name the automation that caused it: %v", reminders[0])
	}

	// A second pass on the same day sends nothing more.
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if got := len(ofType(events.published, "notify.arrears.reminded")); got != 1 {
		t.Fatalf("a second pass produced %d reminders — a tenancy in arrears for two months "+
			"would receive sixty", got)
	}
}

// Seven days out it is on the second rung, and only the second.
func TestTheLadderSendsOnlyTheHighestRungReached(t *testing.T) {
	today := effective.Day(2026, 8, 1)
	events := &fakeEvents{}
	r, _ := harness(t, routine.Deps{
		Now: func() effective.Date { return today },
		Tenancies: &fakeTenancies{
			live:   []routine.Tenancy{{ID: "lease-1", PropertyID: "p1", StartedOn: on(t, "2026-01-01")}},
			tenant: "party-1",
		},
		Money:      fakeMoney{owed: 25_000_00, chargedOn: on(t, "2026-07-18")}, // fourteen days
		Checklists: &fakeChecklists{}, Compliance: fakeCompliance{}, Events: events,
	})

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	reminders := ofType(events.published, "notify.arrears.reminded")
	if len(reminders) != 1 {
		t.Fatalf("%d reminders, want 1 — climbing three rungs in one pass would send three "+
			"messages to somebody who missed a single run", len(reminders))
	}
	if reminders[0]["step"] != "final" {
		t.Errorf("the reminder is the %v step, want the final one", reminders[0]["step"])
	}
}

// A tenancy that owes less than the floor is left alone, and one that owes
// nothing is not chased at all.
func TestTheLadderRespectsTheFloorAndIgnoresWhatIsPaid(t *testing.T) {
	for name, owed := range map[string]int64{"under the floor": 50_00, "nothing": 0} {
		t.Run(name, func(t *testing.T) {
			events := &fakeEvents{}
			r, _ := harness(t, routine.Deps{
				Now: func() effective.Date { return effective.Day(2026, 8, 1) },
				Tenancies: &fakeTenancies{
					live:   []routine.Tenancy{{ID: "lease-1", PropertyID: "p1", StartedOn: on(t, "2026-01-01")}},
					tenant: "party-1",
				},
				Money:      fakeMoney{owed: owed, chargedOn: on(t, "2026-07-01")},
				Checklists: &fakeChecklists{}, Compliance: fakeCompliance{}, Events: events,
			})
			if _, err := r.RunFor(scoped()); err != nil {
				t.Fatal(err)
			}
			if got := len(ofType(events.published, "notify.arrears.reminded")); got != 0 {
				t.Errorf("%d reminders for a tenancy owing %d", got, owed)
			}
		})
	}
}

// A tenancy that owes money against no charge at all is not chased: a ladder
// cannot fix a balance nothing invoiced.
func TestATenancyWithNoChargeIsNotChased(t *testing.T) {
	events := &fakeEvents{}
	r, _ := harness(t, routine.Deps{
		Now: func() effective.Date { return effective.Day(2026, 8, 1) },
		Tenancies: &fakeTenancies{
			live:   []routine.Tenancy{{ID: "lease-1", PropertyID: "p1", StartedOn: on(t, "2026-01-01")}},
			tenant: "party-1",
		},
		Money:      fakeMoney{owed: 25_000_00}, // no charge date
		Checklists: &fakeChecklists{}, Compliance: fakeCompliance{}, Events: events,
	})
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if got := len(ofType(events.published, "notify.arrears.reminded")); got != 0 {
		t.Errorf("%d reminders for a tenancy with no charge behind the balance", got)
	}
}

// The expiry reminder fires once per end date, and the renewal checklist is
// anchored on the end of the term rather than on today.
func TestExpiryRemindsAndRenewalIsAnchoredOnTheTerm(t *testing.T) {
	checklists := &fakeChecklists{}
	events := &fakeEvents{}
	r, _ := harness(t, routine.Deps{
		Now: func() effective.Date { return effective.Day(2026, 8, 1) },
		Tenancies: &fakeTenancies{expiring: []routine.ExpiringTenancy{{
			LeaseID: "lease-1", PropertyID: "p1", UnitID: "u1",
			EndsOn: on(t, "2026-09-15"), DaysRemaining: 45, InsideNoticeWindow: true,
		}}},
		Money: fakeMoney{}, Checklists: checklists, Compliance: fakeCompliance{}, Events: events,
	})

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	reminders := ofType(events.published, "notify.expiry.reminded")
	if len(reminders) != 1 {
		t.Fatalf("%d expiry reminders, want 1", len(reminders))
	}
	if reminders[0]["inside_notice"] != true {
		t.Error("the reminder does not say the tenancy is inside its notice window, which is " +
			"the thing an owner actually needs to know")
	}
	if len(checklists.started) != 1 || checklists.started[0] != "tenancy_renewal:lease-1:2026-09-15" {
		t.Fatalf("started %v, want the renewal anchored on the end of the term — a checklist "+
			"anchored on today dates every step from when the job happened to run",
			checklists.started)
	}

	// Twice does not produce two of either.
	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	if len(checklists.started) != 1 || len(ofType(events.published, "notify.expiry.reminded")) != 1 {
		t.Error("a second pass repeated the reminder or the checklist")
	}
}

// An inspection is proposed once per cycle, and the visit is far enough out for
// the notice the tenant is owed.
func TestInspectionsAreScheduledPerCycleWithNotice(t *testing.T) {
	today := effective.Day(2026, 8, 1)
	events := &fakeEvents{}
	r, _ := harness(t, routine.Deps{
		Now: func() effective.Date { return today },
		Tenancies: &fakeTenancies{live: []routine.Tenancy{
			{ID: "old", PropertyID: "p1", UnitID: "u1", StartedOn: on(t, "2025-01-01")},
			{ID: "new", PropertyID: "p1", UnitID: "u2", StartedOn: on(t, "2026-07-01")},
		}, tenant: "party-1"},
		Money: fakeMoney{}, Checklists: &fakeChecklists{}, Compliance: fakeCompliance{}, Events: events,
	})

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	visits := ofType(events.published, "notify.inspection.scheduled")
	if len(visits) != 1 {
		t.Fatalf("%d inspections, want 1 — a tenancy a month old is not due one", len(visits))
	}
	if visits[0]["lease_id"] != "old" {
		t.Errorf("scheduled an inspection on %v", visits[0]["lease_id"])
	}
	if visits[0]["visit_on_or_after"] != "2026-08-15" {
		t.Errorf("the visit is %v, want fourteen days out — an inspection scheduled for "+
			"tomorrow is one the tenant can refuse", visits[0]["visit_on_or_after"])
	}
}

// A certificate about to lapse is raised while it can still be renewed.
func TestComplianceRaisesALapsingCertificate(t *testing.T) {
	events := &fakeEvents{}
	r, _ := harness(t, routine.Deps{
		Now:        func() effective.Date { return effective.Day(2026, 8, 1) },
		Tenancies:  &fakeTenancies{},
		Money:      fakeMoney{},
		Checklists: &fakeChecklists{},
		Compliance: fakeCompliance{rows: []routine.ExpiringCertificate{{
			PartyID: "party-1", CertificateNumber: "197/2026/00042",
			Section: "194I", ValidTo: on(t, "2026-09-01"), DaysRemaining: 31,
		}}},
		Events: events,
	})

	if _, err := r.RunFor(scoped()); err != nil {
		t.Fatal(err)
	}
	raised := ofType(events.published, "notify.compliance.reminded")
	if len(raised) != 1 {
		t.Fatalf("%d compliance reminders, want 1", len(raised))
	}
	if raised[0]["certificate"] != "197/2026/00042" {
		t.Errorf("the reminder does not name the certificate: %v", raised[0])
	}
}

// The event-triggered ones fire the matching checklist, once, anchored on the
// date the event carries.
func TestAnEventFiresItsChecklistOnce(t *testing.T) {
	cases := []struct {
		event   string
		want    string
		subject automation.Subject
		data    map[string]string
	}{
		{"lease.tenancy.started", "move_in",
			automation.Subject{Kind: automation.SubjectLease, ID: "lease-1"},
			map[string]string{"property_id": "p1", "unit_id": "u1", "anchor_on": "2026-08-04"}},
		{"lease.notice.served", "move_out",
			automation.Subject{Kind: automation.SubjectLease, ID: "lease-2"},
			map[string]string{"property_id": "p1", "unit_id": "u1", "anchor_on": "2026-09-30"}},
		{"identity.organisation.created", "owner_onboarding",
			automation.Subject{Kind: automation.SubjectOrganisation, ID: "org-1"},
			map[string]string{"property_id": "p1"}},
	}

	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			checklists := &fakeChecklists{}
			r, _ := harness(t, routine.Deps{
				Now:       func() effective.Date { return effective.Day(2026, 8, 1) },
				Tenancies: &fakeTenancies{}, Money: fakeMoney{},
				Checklists: checklists, Compliance: fakeCompliance{}, Events: &fakeEvents{},
			})

			for i := 0; i < 2; i++ {
				if err := r.Handle(scoped(), c.event, c.subject, c.data); err != nil {
					t.Fatalf("handling %s: %v", c.event, err)
				}
			}
			if len(checklists.started) != 1 {
				t.Fatalf("started %v, want exactly one %s", checklists.started, c.want)
			}
			if !strings.HasPrefix(checklists.started[0], c.want+":") {
				t.Errorf("started %q, want a %s", checklists.started[0], c.want)
			}
			if anchor, ok := c.data["anchor_on"]; ok && !strings.HasSuffix(checklists.started[0], anchor) {
				t.Errorf("started %q, want it anchored on the event's date %s",
					checklists.started[0], anchor)
			}
		})
	}
}

// An event that names no property starts nothing and says so, rather than being
// dropped where nobody can find out it was ignored.
func TestAnEventWithNoPropertyIsRecordedRatherThanDropped(t *testing.T) {
	checklists := &fakeChecklists{}
	r, m := harness(t, routine.Deps{
		Now:       func() effective.Date { return effective.Day(2026, 8, 1) },
		Tenancies: &fakeTenancies{}, Money: fakeMoney{},
		Checklists: checklists, Compliance: fakeCompliance{}, Events: &fakeEvents{},
	})

	err := r.Handle(scoped(), "lease.tenancy.started",
		automation.Subject{Kind: automation.SubjectLease, ID: "lease-9"}, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(checklists.started) != 0 {
		t.Fatal("a checklist was started against no property")
	}
	var found bool
	for _, run := range m.runs {
		if run.Automation == "move_in_checklist" && run.Outcome == automation.OutcomeSkipped {
			found = true
			if !strings.Contains(run.Detail, "no property") {
				t.Errorf("the run says %q and does not say why nothing happened", run.Detail)
			}
		}
	}
	if !found {
		t.Error("nothing was recorded, so 'the automation is on and nothing happened' has to " +
			"be reconstructed from the event stream")
	}
}

func ofType(published []map[string]any, typ string) []map[string]any {
	var out []map[string]any
	for _, e := range published {
		if e["type"] == typ {
			out = append(out, e)
		}
	}
	return out
}
