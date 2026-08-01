package automation_test

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The engine against the schema. What this checks is the half the in-memory store
// cannot: that the idempotency key is the database's constraint rather than the
// engine's map, that a run cannot be rewritten, and that an override survives a
// round trip in the shape the resolver expects.

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the automation store contract")
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
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set — skipping the seeded automation contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting as the platform role: %v", err)
	}
	t.Cleanup(p.Close)
	return tenancy.NewPlatformPool(p)
}

// Each test gets its own organisation, because the harness commits and a shared
// one would have the second test reading the first's overrides.
func seedOrg(t *testing.T, plat tenancy.PlatformPool) tenancy.ID {
	t.Helper()
	id := tenancy.ID(uuid())
	err := tenancy.Platform(context.Background(), plat, "seeding the automation contract",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO organisations (id, slug, name, kind, state)
				VALUES ($1, $2, 'Automation Harness', 'agency', 'active')
				ON CONFLICT (id) DO NOTHING`, id.String(), "auto-"+id.String()[:8])
			return err
		})
	if err != nil {
		t.Fatalf("seeding the organisation: %v", err)
	}
	return id
}

func uuid() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func TestAnOverrideSurvivesARoundTrip(t *testing.T) {
	p := pool(t)
	id := seedOrg(t, platformPool(t))
	s := automation.NewStore(p)
	ctx := tenancy.With(context.Background(), id)

	overrides, err := s.Overrides(ctx)
	if err != nil {
		t.Fatalf("reading overrides: %v", err)
	}
	if len(overrides) != 0 {
		t.Fatalf("a new organisation has %d overrides, want none — settings are differences, "+
			"and an organisation that changed nothing has none", len(overrides))
	}

	off := false
	ceiling := int64(250_00)
	if err := s.Save(ctx, "arrears_ladder", automation.Override{
		Enabled: &off, CeilingMinor: &ceiling,
		Params: map[string]int64{"first_reminder_after": 5},
	}, ""); err != nil {
		t.Fatalf("saving: %v", err)
	}

	overrides, err = s.Overrides(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o, ok := overrides["arrears_ladder"]
	if !ok {
		t.Fatal("the override was not read back")
	}
	if o.Enabled == nil || *o.Enabled || o.CeilingMinor == nil || *o.CeilingMinor != 250_00 ||
		o.Params["first_reminder_after"] != 5 {
		t.Fatalf("read back %+v", o)
	}

	// Saved twice is one row, not two: two people on the settings screen at once
	// is not exotic.
	if err := s.Save(ctx, "arrears_ladder", automation.Override{
		Params: map[string]int64{"first_reminder_after": 9},
	}, ""); err != nil {
		t.Fatal(err)
	}
	overrides, _ = s.Overrides(ctx)
	if len(overrides) != 1 || overrides["arrears_ladder"].Params["first_reminder_after"] != 9 {
		t.Fatalf("saving twice produced %+v", overrides)
	}
	if overrides["arrears_ladder"].Enabled != nil {
		t.Error("the second save did not replace the first — a settings screen that sends the " +
			"whole row would otherwise leave a stale switch behind")
	}
}

// The idempotency key is the database's, which is what makes it hold across
// processes rather than within one.
func TestTheKeyIsTheDatabases(t *testing.T) {
	p := pool(t)
	id := seedOrg(t, platformPool(t))
	s := automation.NewStore(p)
	ctx := tenancy.With(context.Background(), id)

	rec := automation.Record{
		Automation: "arrears_ladder",
		Subject:    automation.Subject{Kind: automation.SubjectLease, ID: uuid()},
		Outcome:    automation.OutcomeActed, Action: "arrears.reminder.first",
		Params: map[string]int64{"first_reminder_after": 3},
		Key:    "lease-1:2026-08:first",
	}

	written, err := s.Record(ctx, rec)
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	if !written {
		t.Fatal("the first record was not written")
	}
	done, err := s.Recorded(ctx, rec.Automation, rec.Key)
	if err != nil || !done {
		t.Fatalf("Recorded reported %v (%v) after a write", done, err)
	}

	written, err = s.Record(ctx, rec)
	if err != nil {
		t.Fatalf("recording twice errored rather than reporting a conflict: %v", err)
	}
	if written {
		t.Fatal("the same proposal was recorded twice, so a second run would act again")
	}
}

// A run is history. The privilege is withheld and the trigger refuses, so a
// session never reaches the second lock — this asserts the pair.
func TestARunCannotBeRewritten(t *testing.T) {
	p := pool(t)
	id := seedOrg(t, platformPool(t))
	s := automation.NewStore(p)
	ctx := tenancy.With(context.Background(), id)

	if _, err := s.Record(ctx, automation.Record{
		Automation: "arrears_ladder",
		Subject:    automation.Subject{Kind: automation.SubjectLease, ID: uuid()},
		Outcome:    automation.OutcomeActed, Action: "arrears.reminder.first",
		Key: "immutable:1",
	}); err != nil {
		t.Fatal(err)
	}

	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE automation_runs SET outcome = 'skipped' WHERE idempotency_key = 'immutable:1'`)
		return err
	})
	if err == nil {
		t.Fatal("a run was rewritten — the provenance trail is worth nothing if the record of " +
			"what was done can be edited")
	}
}

// The story's failure scenario, through the store: over the ceiling, an approval
// that names both numbers, and no second request while it is pending.
func TestAnApprovalIsRequestedOnceAndDecidedOnce(t *testing.T) {
	p := pool(t)
	id := seedOrg(t, platformPool(t))
	s := automation.NewStore(p)
	ctx := tenancy.With(context.Background(), id)

	req := automation.ApprovalRequest{
		Automation: "arrears_ladder",
		Subject:    automation.Subject{Kind: automation.SubjectLease, ID: uuid()},
		Action:     "arrears.waive", Amount: 50_000, Ceiling: 10_000,
		Key: "lease-1:waive",
	}
	written, err := s.RequestApproval(ctx, req)
	if err != nil || !written {
		t.Fatalf("requesting: written=%v err=%v", written, err)
	}
	asked, err := s.Requested(ctx, req.Automation, req.Key)
	if err != nil || !asked {
		t.Fatalf("Requested reported %v (%v) after a request", asked, err)
	}
	if written, _ := s.RequestApproval(ctx, req); written {
		t.Error("the same proposal asked twice")
	}

	pending, err := s.Pending(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].Amount != 50_000 || pending[0].Ceiling != 10_000 {
		t.Fatalf("pending is %+v, want one request naming the amount and the ceiling", pending)
	}

	if err := s.Decide(ctx, pending[0].ID, "approved", "", ""); err != nil {
		t.Fatalf("approving: %v", err)
	}
	if err := s.Decide(ctx, pending[0].ID, "declined", "changed my mind", ""); !errors.Is(err, automation.ErrNoApproval) {
		t.Errorf("re-deciding gave %v, want ErrNoApproval — a decision made once is a decision", err)
	}
	if left, _ := s.Pending(ctx, 0); len(left) != 0 {
		t.Errorf("%d requests still pending after a decision", len(left))
	}
}

// A decline says why, because the automation will ask again.
func TestADeclineSaysWhy(t *testing.T) {
	p := pool(t)
	id := seedOrg(t, platformPool(t))
	s := automation.NewStore(p)
	ctx := tenancy.With(context.Background(), id)

	if _, err := s.RequestApproval(ctx, automation.ApprovalRequest{
		Automation: "arrears_ladder",
		Subject:    automation.Subject{Kind: automation.SubjectLease, ID: uuid()},
		Action:     "arrears.waive", Amount: 50_000, Ceiling: 10_000, Key: "why:1",
	}); err != nil {
		t.Fatal(err)
	}
	pending, _ := s.Pending(ctx, 0)
	if err := s.Decide(ctx, pending[0].ID, "declined", "", ""); err == nil {
		t.Error("a request was declined with no reason")
	}
	if err := s.Decide(ctx, pending[0].ID, "sideways", "", ""); err == nil {
		t.Error("a request was decided into a state that is not a decision")
	}
}

// The provenance read: what was automated on one record, which is the story's
// edge case.
func TestHistoryAnswersWhatWasAutomatedOnARecord(t *testing.T) {
	p := pool(t)
	id := seedOrg(t, platformPool(t))
	s := automation.NewStore(p)
	ctx := tenancy.With(context.Background(), id)

	lease := automation.Subject{Kind: automation.SubjectLease, ID: uuid()}
	other := automation.Subject{Kind: automation.SubjectLease, ID: uuid()}
	for i, subject := range []automation.Subject{lease, lease, other} {
		if _, err := s.Record(ctx, automation.Record{
			Automation: "arrears_ladder", Subject: subject,
			Outcome: automation.OutcomeActed, Action: "arrears.reminder.first",
			Detail: "chased", Key: fmt.Sprintf("history:%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}

	rows, err := s.History(ctx, lease, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("%d rows of history on the record, want 2 — and none of the other lease's", len(rows))
	}
	for _, r := range rows {
		if r.Automation != "arrears_ladder" || r.Action != "arrears.reminder.first" {
			t.Errorf("a history line says %+v and does not name what caused it", r)
		}
	}

	activity, err := s.Activity(ctx)
	if err != nil {
		t.Fatal(err)
	}
	a, ok := activity["arrears_ladder"]
	if !ok || a.Runs != 3 || a.Acted != 3 {
		t.Fatalf("activity is %+v, want 3 runs and 3 acted — the line beside the switch is what "+
			"turns a toggle into something a manager can believe", a)
	}
	if a.LastRunAt.IsZero() {
		t.Error("the activity does not say when it last ran")
	}
}

// Every read is scoped, and one with no organisation reaches nothing.
func TestTheStoreNeedsAnOrganisation(t *testing.T) {
	p := pool(t)
	s := automation.NewStore(p)

	if _, err := s.Record(context.Background(), automation.Record{
		Automation: "arrears_ladder",
		Subject:    automation.Subject{Kind: automation.SubjectLease, ID: uuid()},
		Outcome:    automation.OutcomeActed, Action: "a", Key: "k",
	}); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Errorf("recording with no organisation gave %v, want ErrNoTenant", err)
	}
	if err := s.Save(context.Background(), "arrears_ladder", automation.Override{}, ""); !errors.Is(err, tenancy.ErrNoTenant) {
		t.Errorf("saving with no organisation gave %v, want ErrNoTenant", err)
	}
}
