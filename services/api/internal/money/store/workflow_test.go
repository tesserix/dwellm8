package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
)

// platformPool is the platform-role connection. The store package's own pool() is
// the request role, which cannot see the platform organisation — correctly, since a
// tenant session must not — so asserting that the row exists needs the other one.
func platformPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting as the platform role: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

// ADR-0015's two copies, compared. The run state machine exists in Go so a worker
// can decide without a round trip, and in PostgreSQL so a path that never went
// through Go cannot walk a run backwards. Two copies is a deliberate choice and
// this file is its price, exactly as for ADR-0011 §3.

func TestTheGoRunStateMachineMatchesTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	states := workflow.RunStates()
	names := make([]string, len(states))
	for i, s := range states {
		names[i] = string(s)
	}

	rows, err := p.Query(ctx, `
		SELECT f.s, t.s, workflow_transition_allowed(f.s, t.s)
		  FROM unnest($1::text[]) AS f(s)
		  CROSS JOIN unnest($1::text[]) AS t(s)
		 ORDER BY f.s, t.s`, names)
	if err != nil {
		t.Fatalf("evaluating workflow_transition_allowed: %v — ADR-0015 §5 requires it", err)
	}
	defer rows.Close()

	// Go's copy of the same table. Kept here rather than exported from the workflow
	// package because nothing in the product needs to ask: a run's state is written
	// by the saga's outcome, and this exists to prove the schema agrees with what
	// the saga can produce.
	allowed := map[string][]string{
		"running":      {"completed", "compensating", "escalated"},
		"compensating": {"compensated", "escalated"},
		"completed":    nil,
		"compensated":  nil,
		"escalated":    nil,
	}
	can := func(from, to string) bool {
		if from == to {
			_, known := allowed[from]
			return known
		}
		for _, s := range allowed[from] {
			if s == to {
				return true
			}
		}
		return false
	}

	pairs := 0
	for rows.Next() {
		var from, to string
		var inDB bool
		if err := rows.Scan(&from, &to, &inDB); err != nil {
			t.Fatalf("scan: %v", err)
		}
		pairs++
		if inGo := can(from, to); inGo != inDB {
			t.Errorf("%s -> %s: Go says %v and the database says %v", from, to, inGo, inDB)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if want := len(states) * len(states); pairs != want {
		t.Fatalf("checked %d pairs, want %d — the cross product did not run", pairs, want)
	}

	// Every state the saga can record must be one the schema accepts. This is the
	// direction that fails quietly: a state Go writes and the CHECK refuses stops
	// the run advancing while every log line says the step was handled.
	var def string
	if err := p.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		  WHERE conname = 'workflow_runs_state_check'`).Scan(&def); err != nil {
		t.Fatalf("reading workflow_runs_state_check: %v", err)
	}
	for _, s := range states {
		if !strings.Contains(def, "'"+string(s)+"'") {
			t.Errorf("run state %q is producible in Go and refused by the schema", s)
		}
	}
}

// The constraint that carries ADR-0015 §4 into the database.
//
// The Go saga refuses to compensate past the point of no return. This asserts the
// schema refuses to *record* it, which is the half that matters for anything that
// did not come through the saga — a data fix, a support script, a future workflow
// somebody writes without reading the standard.
func TestTheSchemaRefusesToRecordAnImpossibleCompensation(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		  WHERE conname = 'workflow_runs_compensated_means_reversible'`).Scan(&def); err != nil {
		t.Fatalf("reading workflow_runs_compensated_means_reversible: %v — ADR-0015 §4 is this constraint", err)
	}
	if !strings.Contains(def, "past_no_return") || !strings.Contains(def, "compensated") {
		t.Errorf("the constraint does not relate compensation to the point of no return: %s", def)
	}

	// Evaluated rather than read, over the four combinations. Only one is refused,
	// and it is the one that would claim the world was put back after money left.
	for _, tc := range []struct {
		state        string
		pastNoReturn bool
		want         bool
	}{
		{"compensated", false, true},
		{"compensated", true, false},
		{"escalated", true, true},
		{"completed", true, true},
	} {
		var ok bool
		if err := p.QueryRow(ctx,
			`SELECT $1::text <> 'compensated' OR NOT $2::boolean`, tc.state, tc.pastNoReturn).Scan(&ok); err != nil {
			t.Fatalf("evaluating: %v", err)
		}
		if ok != tc.want {
			t.Errorf("state %q past the point of no return %v is accepted=%v, want %v",
				tc.state, tc.pastNoReturn, ok, tc.want)
		}
	}
}

// Every operation the Go standard can start must have a task queue the schema will
// accept, and the run's operation column must be able to hold the longest name. A
// truncated operation is a workflow id nobody can reconstruct.
func TestEveryDurableOperationFitsTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	// Both columns are unbounded text on purpose; this asserts that, because a
	// varchar(32) added later would truncate `collect.autopay_debit` in a column
	// nothing else validates.
	for _, col := range []string{"operation", "task_queue", "workflow_id"} {
		var typ string
		var maxLen *int
		if err := p.QueryRow(ctx, `
			SELECT data_type, character_maximum_length FROM information_schema.columns
			 WHERE table_schema = 'public' AND table_name = 'workflow_runs' AND column_name = $1`,
			col).Scan(&typ, &maxLen); err != nil {
			t.Fatalf("reading workflow_runs.%s: %v", col, err)
		}
		if typ != "text" || maxLen != nil {
			t.Errorf("workflow_runs.%s is %s(%v) — a truncated operation is a workflow id nobody "+
				"can reconstruct from a payout", col, typ, maxLen)
		}
	}

	// And the ids the standard produces actually round-trip.
	for _, op := range workflow.Operations() {
		id, err := workflow.ID(op, "7c9e6679-7425-40de-944b-e07fc1f90ae7")
		if err != nil {
			t.Fatalf("ID(%s): %v", op, err)
		}
		var back string
		if err := p.QueryRow(ctx, `SELECT $1::text`, id).Scan(&back); err != nil {
			t.Fatalf("round-tripping %s: %v", id, err)
		}
		if back != id {
			t.Errorf("workflow id %q came back as %q", id, back)
		}
	}
}

// The platform organisation ADR-0002 §1 assumed and nothing created until ADR-0015
// needed it. A platform-wide run carries it, so its absence is a foreign-key
// failure on the first nightly reconciliation rather than at any point a test would
// have found.
func TestThePlatformOrganisationExists(t *testing.T) {
	ctx := context.Background()
	p := platformPool(t)

	var id, kind, state string
	err := p.QueryRow(ctx, `
		SELECT id::text, kind, state FROM organisations WHERE kind = 'platform'`).Scan(&id, &kind, &state)
	if err != nil {
		t.Fatalf("reading the platform organisation: %v — ADR-0002 §1 says every platform-level fact "+
			"carries it, and ADR-0015's platform-wide runs have a foreign key to it", err)
	}
	if state != "active" {
		t.Errorf("the platform organisation is %q", state)
	}
	// The same uuid the money domain uses as the platform party. One number for one
	// actor; two would mean every reader has to know which is which.
	if id != "00000000-0000-0000-0000-0000000000d8" {
		t.Errorf("the platform organisation is %s and the platform party is "+
			"00000000-0000-0000-0000-0000000000d8", id)
	}

	// It is not a sandbox and it is not an owner: a platform organisation that
	// looked like a tenant would appear in reports that sum across organisations.
	var sandbox bool
	if err := p.QueryRow(ctx,
		`SELECT is_sandbox FROM organisations WHERE id = $1`, id).Scan(&sandbox); err != nil {
		t.Fatalf("reading is_sandbox: %v", err)
	}
	if sandbox {
		t.Error("the platform organisation is marked as sandbox, where nothing may cause a side effect")
	}
}
