package store_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The drift check between the two copies of ADR-0032's rules.
//
// They exist in Go so the refusal names the outstanding step, and in PostgreSQL so a
// path that never went through Go cannot close a tenancy over an unfinished
// move-out. Two copies is a deliberate choice and this file is its price. The
// dangerous direction of drift is the quiet one: the schema permitting something Go
// refuses, because then the guard only works for callers who were already going to
// behave.

// Every test gets its own organisation. The harness commits, so a shared one would
// mean the second test resolving the first test's template — which is not a bug in
// the code and is a very confusing failure.
func newOrg() tenancy.ID { return tenancy.ID(uuid()) }

func uuid() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the checklist contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func platformPool(t *testing.T) tenancy.PlatformPool {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set — skipping the seeded checklist contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting as the platform role: %v", err)
	}
	t.Cleanup(p.Close)
	return tenancy.NewPlatformPool(p)
}

// fixtures seeds the organisation, a property, a flat and a live tenancy, and
// returns the lease. Ids are per-run so a committed fixture cannot collide with the
// next run's.
type fixture struct {
	org                   tenancy.ID
	property, unit, lease string
}

// ctx is a request context scoped to this fixture's organisation.
func (f fixture) ctx() context.Context { return tenancy.With(context.Background(), f.org) }

func seed(t *testing.T, p *pgxpool.Pool, plat tenancy.PlatformPool) fixture {
	t.Helper()
	ctx := context.Background()
	f := fixture{org: newOrg()}
	org := f.org

	err := tenancy.Platform(ctx, plat, "seeding the checklist contract", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO organisations (id, slug, name, kind, state)
			VALUES ($1, $2, 'Checklist Harness', 'agency', 'active')
			ON CONFLICT (id) DO NOTHING`, f.org.String(), "checklist-"+f.org.String()[:8])
		return err
	})
	if err != nil {
		t.Fatalf("seeding the organisation: %v", err)
	}

	err = tenancy.Scoped(tenancy.With(ctx, org), p, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO properties (tenant_id, code, name, kind, address_line1, locality, city, state_code, pin)
			VALUES ($1, 'CHK-' || substr(gen_random_uuid()::text, 1, 8), 'Contract Tower', 'building',
			        '1 Road', 'GK', 'Delhi', 'DL', '110048')
			RETURNING id::text`, f.org.String()).Scan(&f.property); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `
			INSERT INTO units (tenant_id, property_id, unit_kind, code, carpet_area_sqft)
			VALUES ($1, $2, 'flat', substr(gen_random_uuid()::text, 1, 8), 800)
			RETURNING id::text`, f.org.String(), f.property).Scan(&f.unit); err != nil {
			return err
		}
		// draft, not active: a live tenancy needs ADR-0024's tax facts, and this
		// file is about the checklist gate rather than the tax one. It is moved to
		// active in the one test that closes it.
		return tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'draft', current_date, current_date + 365)
			RETURNING id::text`, f.org.String(), f.property, f.unit).Scan(&f.lease)
	})
	if err != nil {
		t.Fatalf("seeding the tree: %v", err)
	}
	return f
}

// seedTemplate writes a move-out template for the organisation and returns it as the
// domain sees it, so the test's expectations and the database's rows have one source.
func seedTemplate(t *testing.T, p *pgxpool.Pool, f fixture, kind domain.PropertyKind) domain.Template {
	t.Helper()
	tpl := domain.Template{
		TenantID: f.org.String(), Process: domain.ProcessMoveOut, Kind: kind,
		Name: "Contract move-out", Version: 1,
		Steps: []domain.Step{
			{Code: "keys", Title: "Collect the keys", Position: 1, Blocking: true, Owner: "field_agent"},
			{Code: "meter", Title: "Final meter reading", Position: 2, Blocking: true,
				Owner: "field_agent", DueOffsetDays: 1, DependsOn: []string{"keys"}},
			{Code: "photos", Title: "Exit photographs", Position: 3,
				Owner: "field_agent", DependsOn: []string{"keys"}},
		},
	}

	err := tenancy.Scoped(f.ctx(), p, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO checklist_templates (tenant_id, process, property_kind, name, version, published_at)
			VALUES ($1, $2, $3, $4, $5, now())
			RETURNING id::text`,
			f.org.String(), string(tpl.Process), nullKind(kind), tpl.Name, tpl.Version).Scan(&tpl.ID); err != nil {
			return err
		}
		for _, s := range tpl.Steps {
			if _, err := tx.Exec(ctx, `
				INSERT INTO checklist_template_steps
				    (tenant_id, template_id, code, title, position, blocking, owner_role,
				     due_offset_days, depends_on)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, coalesce($9::citext[], '{}'))`,
				f.org.String(), tpl.ID, s.Code, s.Title, s.Position, s.Blocking,
				string(s.Owner), s.DueOffsetDays, s.DependsOn); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding the template: %v", err)
	}
	return tpl
}

func nullKind(k domain.PropertyKind) any {
	if k == "" {
		return nil
	}
	return string(k)
}

func fire(t *testing.T, s *store.Checklists, tpl domain.Template, f fixture) domain.Checklist {
	t.Helper()
	c, err := tpl.Trigger(f.org.String(), effective.DateOf(time.Now(), time.Local),
		domain.Subject{PropertyID: f.property, UnitID: f.unit, LeaseID: f.lease})
	if err != nil {
		t.Fatalf("materialising: %v", err)
	}
	out, err := s.Trigger(f.ctx(), c, "")
	if err != nil {
		t.Fatalf("firing: %v", err)
	}
	return out
}

// The story's primary scenario, through the store: one call, every task written.
func TestFiringWritesTheWholeGraphInOneTransaction(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	tpl := seedTemplate(t, p, f, "building")

	c := fire(t, s, tpl, f)
	if c.ID == "" {
		t.Fatal("the checklist came back with no id")
	}

	read, err := s.Read(f.ctx(), c.ID)
	if err != nil {
		t.Fatalf("reading it back: %v", err)
	}
	if len(read.Tasks) != len(tpl.Steps) {
		t.Fatalf("read %d tasks, want %d", len(read.Tasks), len(tpl.Steps))
	}
	for i, task := range read.Tasks {
		if task.StepCode != c.Tasks[i].StepCode || task.State != c.Tasks[i].State ||
			!task.DueOn.Equal(c.Tasks[i].DueOn) {
			t.Errorf("task %d read back as %+v, was written as %+v — the store and the domain "+
				"disagree about what was fired", i, task, c.Tasks[i])
		}
	}
	if read.AnchorOn.Zero() {
		t.Error("the anchor was not stored, so nothing can explain why a task is due when it is")
	}
}

// Two managers pressing the button at the same moment is the case a check-then-insert
// gets wrong. The constraint is the only thing that sees both.
func TestFiringTwiceOverOneTenancyIsRefusedByTheSchema(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	tpl := seedTemplate(t, p, f, "building")
	fire(t, s, tpl, f)

	ctx := f.ctx()
	c, err := tpl.Trigger(f.org.String(), effective.DateOf(time.Now(), time.Local),
		domain.Subject{PropertyID: f.property, UnitID: f.unit, LeaseID: f.lease})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Trigger(ctx, c, "")
	var already store.ErrAlreadyOpen
	if !errors.As(err, &already) {
		t.Fatalf("firing a second move-out gave %v, want ErrAlreadyOpen", err)
	}
	if already.ExistingID == "" {
		t.Error("the refusal does not name the process that is already open, so a client " +
			"cannot show it")
	}
}

// The second copy of the dependency rule. Bypassing the store's Go path entirely,
// because that is the path this guard exists for.
func TestTheSchemaRefusesAStepSettledOutOfOrder(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	c := fire(t, s, seedTemplate(t, p, f, "building"), f)

	ctx := f.ctx()
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE checklist_tasks SET state = 'done', completed_at = now()
			 WHERE checklist_id = $1 AND step_code = 'meter'`, c.ID)
		return err
	})
	if err == nil {
		t.Fatal("the database let the meter reading be done before the keys were collected — " +
			"the Go rule is then the only one, and it only binds callers who use it")
	}
	if !strings.Contains(err.Error(), "Collect the keys") {
		t.Errorf("the database's refusal is %q and does not name the step being waited on", err)
	}
}

// A blocking step that could be skipped would make every gate advisory.
func TestTheSchemaRefusesASkippedBlockingStep(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	c := fire(t, s, seedTemplate(t, p, f, "building"), f)

	ctx := f.ctx()
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE checklist_tasks SET state = 'skipped', skipped_reason = 'no time'
			 WHERE checklist_id = $1 AND step_code = 'keys'`, c.ID)
		return err
	})
	if err == nil {
		t.Fatal("a blocking step was skipped in the database")
	}
	if !strings.Contains(err.Error(), "checklist_tasks_skip_shape") {
		t.Errorf("the refusal is %q, want the skip-shape constraint", err)
	}
}

// Settling through the store releases what was waiting, and the row shows it.
func TestSettlingThroughTheStoreReleasesDependents(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	c := fire(t, s, seedTemplate(t, p, f, "building"), f)
	ctx := f.ctx()

	after, err := s.Settle(ctx, c.ID, "keys", domain.TaskDone, "", "")
	if err != nil {
		t.Fatalf("collecting the keys: %v", err)
	}
	for _, task := range after.Tasks {
		want := domain.TaskPending
		if task.StepCode == "keys" {
			want = domain.TaskDone
		}
		if task.State != want {
			t.Errorf("%s is %s after the keys, want %s", task.StepCode, task.State, want)
		}
	}

	read, err := s.Read(ctx, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range read.Tasks {
		if task.StepCode == "meter" && task.State != domain.TaskPending {
			t.Errorf("the meter reading is %s in the database, want pending — the release did "+
				"not survive the commit", task.State)
		}
	}

	// And the fact was written on the same transaction as the state it reports.
	var events int
	err = tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM outbox
			 WHERE subject_kind = 'checklist' AND subject_id = $1
			   AND type = 'maintenance.checklist_task.completed'`, c.ID).Scan(&events)
	})
	if err != nil {
		t.Fatal(err)
	}
	if events != 1 {
		t.Errorf("%d task-completed events in the outbox, want 1", events)
	}
}

// The gate. This is the acceptance criterion, checked where it cannot be bypassed.
func TestATenancyDoesNotCloseOverAnUnfinishedMoveOut(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	c := fire(t, s, seedTemplate(t, p, f, "building"), f)
	ctx := f.ctx()

	activate(t, p, f, f.lease)

	err := closeTenancy(ctx, p, f.lease)
	if err == nil {
		t.Fatal("the tenancy closed with its move-out unfinished — the deposit is then settled " +
			"against an inspection nobody did")
	}
	for _, want := range []string{"Collect the keys", "Final meter reading"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal is %q and does not name %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "Exit photographs") {
		t.Error("the refusal names a step that is not blocking")
	}

	// And the outstanding list the lease module asks for says the same thing.
	outstanding, err := s.Outstanding(ctx, f.lease, domain.ProcessMoveOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 2 {
		t.Fatalf("Outstanding reported %d steps, want 2 — Go and the trigger disagree about "+
			"what is blocking", len(outstanding))
	}

	for _, step := range []string{"keys", "meter"} {
		if _, err := s.Settle(ctx, c.ID, step, domain.TaskDone, "", ""); err != nil {
			t.Fatalf("settling %s: %v", step, err)
		}
	}
	if _, err := s.Close(ctx, c.ID, domain.StateCompleted, ""); err != nil {
		t.Fatalf("finishing the move-out: %v", err)
	}
	if err := closeTenancy(ctx, p, f.lease); err != nil {
		t.Fatalf("the tenancy still would not close after the move-out finished: %v", err)
	}
}

// An abandoned move-out stops gating: the process was stopped on purpose, and a
// tenancy held open by a checklist nobody is working is worse than no checklist.
func TestAnAbandonedMoveOutStopsGating(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	c := fire(t, s, seedTemplate(t, p, f, "building"), f)
	ctx := f.ctx()
	activate(t, p, f, f.lease)

	if _, err := s.Close(ctx, c.ID, domain.StateAbandoned, "the tenant withdrew their notice"); err != nil {
		t.Fatalf("abandoning: %v", err)
	}
	outstanding, err := s.Outstanding(ctx, f.lease, domain.ProcessMoveOut)
	if err != nil {
		t.Fatal(err)
	}
	if len(outstanding) != 0 {
		t.Errorf("an abandoned move-out still reports %d outstanding steps", len(outstanding))
	}
	if err := closeTenancy(ctx, p, f.lease); err != nil {
		t.Fatalf("the tenancy would not close after the move-out was abandoned: %v", err)
	}
}

// The story's edge case, in the view rather than in a column somebody has to set.
func TestAnOverdueOpenChecklistIsVisibleAsStalled(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	tpl := seedTemplate(t, p, f, "building")
	ctx := f.ctx()

	// Anchored last year, so every step is late.
	c, err := tpl.Trigger(f.org.String(), effective.Day(2025, 1, 1),
		domain.Subject{PropertyID: f.property, UnitID: f.unit, LeaseID: f.lease})
	if err != nil {
		t.Fatal(err)
	}
	fired, err := s.Trigger(ctx, c, "")
	if err != nil {
		t.Fatal(err)
	}

	rows, err := s.Portfolio(ctx, domain.StateOpen, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row.ChecklistID != fired.ID {
			continue
		}
		found = true
		if row.DaysOverdue <= 0 {
			t.Errorf("an open checklist anchored in 2025 reports %d days overdue — a process "+
				"nobody is working reads as one under way", row.DaysOverdue)
		}
		if row.BlockingOutstanding != 2 {
			t.Errorf("%d blocking steps outstanding, want 2", row.BlockingOutstanding)
		}
	}
	if !found {
		t.Fatal("the fired checklist is not in the portfolio view")
	}
}

// The resolution order, against the rows the schema actually seeded: an organisation
// with nothing configured still gets the platform library.
func TestTheDefaultLibraryIsResolvableByAnyOrganisation(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	ctx := f.ctx()

	for _, process := range domain.Processes() {
		candidates, err := s.TemplatesFor(ctx, process)
		if err != nil {
			t.Fatalf("reading candidates for %s: %v", process, err)
		}
		tpl, err := domain.Resolve(candidates, f.org.String(), process, "building")
		if err != nil {
			t.Fatalf("an organisation that has configured nothing cannot fire a %s: %v", process, err)
		}
		if err := tpl.Validate(); err != nil {
			t.Errorf("the seeded %s template is not usable: %v", process, err)
		}
		if !tpl.Default {
			t.Errorf("resolved a non-default template for %s in an organisation that has none", process)
		}
	}

	// And the kind-specific rung: a co-living move-out is not the ordinary one.
	candidates, err := s.TemplatesFor(ctx, domain.ProcessMoveOut)
	if err != nil {
		t.Fatal(err)
	}
	coliving, err := domain.Resolve(candidates, f.org.String(), domain.ProcessMoveOut, "coliving")
	if err != nil {
		t.Fatal(err)
	}
	if coliving.Kind != "coliving" {
		t.Errorf("a co-living property resolved the %q template — the story's edge case is that "+
			"a hostel move-out differs from a commercial one", coliving.Kind)
	}
}

// An organisation's own template outranks the library, which is "configurable per
// organisation" actually working.
func TestAnOrganisationsOwnTemplateWins(t *testing.T) {
	p := pool(t)
	f := seed(t, p, platformPool(t))
	s := store.New(p)
	mine := seedTemplate(t, p, f, "")
	ctx := f.ctx()

	candidates, err := s.TemplatesFor(ctx, domain.ProcessMoveOut)
	if err != nil {
		t.Fatal(err)
	}
	got, err := domain.Resolve(candidates, f.org.String(), domain.ProcessMoveOut, "building")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != mine.ID {
		t.Fatalf("resolved %s, want this organisation's own template %s", got.ID, mine.ID)
	}
}

func activate(t *testing.T, p *pgxpool.Pool, f fixture, lease string) {
	t.Helper()
	ctx := f.ctx()
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		// The tax facts ADR-0024 demands of a live tenancy, so this file's failures
		// are about checklists rather than about section 194-I.
		if _, err := tx.Exec(ctx, `
			INSERT INTO lease_tax_facts (tenant_id, lease_id, deductor_class, landlord_residency,
			                             source, valid_from)
			VALUES ($1, $2, 'business', 'resident', 'fixture', current_date)`,
			f.org.String(), lease); err != nil {
			return err
		}
		// Through pending_signature: ADR-0010's machine has no draft -> active edge,
		// and the schema is where that is enforced.
		if _, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'pending_signature' WHERE id = $1`, lease); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `UPDATE leases SET state = 'active' WHERE id = $1`, lease)
		return err
	})
	if err != nil {
		t.Fatalf("activating the tenancy: %v", err)
	}
}

func closeTenancy(ctx context.Context, p *pgxpool.Pool, lease string) error {
	return tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE leases
			   SET state = 'terminated', ended_on = current_date + 1, terminated_by = 'owner',
			       terminated_reason = 'the contract test', settlement_decision = 'none'
			 WHERE id = $1`, lease)
		return err
	})
}
