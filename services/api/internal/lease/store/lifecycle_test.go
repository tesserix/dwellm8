package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
)

// The drift check between the two copies of ADR-0010's state machine.
//
// It exists in Go so a handler can decide without a round trip, and in PostgreSQL so a
// path that never went through Go cannot move a lease sideways. Two copies is a
// deliberate choice and this file is its price — the same trade ADR-0011 §3 makes, and
// the dangerous direction of drift is the same quiet one: the schema refusing a
// transition Go performs, so a tenancy silently stops advancing while every log line
// says the request succeeded.

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the lease lifecycle contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestTheGoLeaseStateMachineMatchesTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	states := domain.States()
	names := make([]string, len(states))
	for i, s := range states {
		names[i] = string(s)
	}

	rows, err := p.Query(ctx, `
		SELECT f.s, t.s, lease_transition_allowed(f.s, t.s)
		  FROM unnest($1::text[]) AS f(s)
		  CROSS JOIN unnest($1::text[]) AS t(s)
		 ORDER BY f.s, t.s`, names)
	if err != nil {
		t.Fatalf("evaluating lease_transition_allowed: %v — ADR-0010 §3 requires it", err)
	}
	defer rows.Close()

	pairs := 0
	for rows.Next() {
		var from, to string
		var inDB bool
		if err := rows.Scan(&from, &to, &inDB); err != nil {
			t.Fatalf("scan: %v", err)
		}
		pairs++
		if inGo := domain.CanTransition(domain.State(from), domain.State(to)); inGo != inDB {
			t.Errorf("%s -> %s: Go says %v and the database says %v", from, to, inGo, inDB)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if want := len(states) * len(states); pairs != want {
		t.Fatalf("checked %d pairs, want %d — the cross product did not run", pairs, want)
	}
}

// Every vocabulary Go can produce, against the CHECK that would refuse it. A state or
// a decision the schema rejects is a tenancy that cannot be moved on the day it needs
// to be.
func TestTheGoLeaseVocabulariesAreAcceptedByTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	def := func(name string) string {
		var out string
		if err := p.QueryRow(ctx,
			`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, name).Scan(&out); err != nil {
			t.Fatalf("reading constraint %s: %v — ADR-0010 requires it", name, err)
		}
		return out
	}

	states := def("leases_state_check")
	for _, s := range domain.States() {
		if !strings.Contains(states, "'"+string(s)+"'") {
			t.Errorf("state %q is producible in Go and refused by leases_state_check", s)
		}
	}

	decisions := def("leases_settlement_decision_check")
	for _, d := range domain.Decisions() {
		if !strings.Contains(decisions, "'"+string(d)+"'") {
			t.Errorf("settlement decision %q is producible in Go and refused by the schema", d)
		}
	}

	actors := def("leases_terminated_by_check")
	for _, a := range domain.Actors() {
		if !strings.Contains(actors, "'"+string(a)+"'") {
			t.Errorf("actor %q is producible in Go and refused by the schema", a)
		}
	}
}

// The states that count as a tenancy, which is what the no-double-let constraint is
// scoped to. Go decides it with State.Tenancy(); the schema decides it with the
// constraint's WHERE clause; a disagreement means either two tenancies of one flat are
// possible, or an owner cannot prepare a renewal.
func TestTheNoDoubleLetScopeMatchesTenancy(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx,
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = 'leases_no_double_let'`).
		Scan(&def); err != nil {
		t.Fatalf("reading leases_no_double_let: %v — it is the constraint that stops a flat being "+
			"let twice", err)
	}
	if !strings.Contains(def, "&&") || !strings.Contains(def, "validity") {
		t.Errorf("the constraint does not exclude overlapping validity: %s", def)
	}

	for _, s := range domain.States() {
		inSchema := strings.Contains(def, "'"+string(s)+"'")
		if inGo := s.Tenancy(); inGo != inSchema {
			t.Errorf("state %q: Go says tenancy=%v and the constraint's scope says %v — either two "+
				"tenancies of one flat are possible, or an owner cannot draft a renewal while the "+
				"current one runs", s, inGo, inSchema)
		}
	}
	t.Logf("scope: %s", def)
}

// The occupancy interval, evaluated by PostgreSQL against Go's Occupancy(). Both
// compute "the agreement, cut short if occupancy ceased early", and the schema does it
// in a generated column with LEAST — which ignores NULLs, and that is the behaviour the
// two copies actually rely on.
func TestTheOccupancyIntervalMatchesTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	for _, tc := range []struct {
		name              string
		from, to, endedOn string
		wantUpper         string // "" = unbounded
	}{
		{"running to an agreed end", "2025-07-01", "2026-07-01", "", "2026-07-01"},
		{"ceased early", "2025-07-01", "2026-07-01", "2026-06-21", "2026-06-21"},
		{"ran to term", "2025-07-01", "2026-07-01", "2026-07-01", "2026-07-01"},
		{"periodic, still running", "2025-07-01", "", "", ""},
		{"periodic, ceased", "2025-07-01", "", "2026-06-21", "2026-06-21"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var upper string
			err := p.QueryRow(ctx, `
				SELECT coalesce(upper(daterange($1::date, LEAST($2::date, $3::date), '[)'))::text, '')`,
				nullable(tc.from), nullable(tc.to), nullable(tc.endedOn)).Scan(&upper)
			if err != nil {
				t.Fatalf("evaluating: %v", err)
			}
			if upper != tc.wantUpper {
				t.Errorf("the database's occupancy ends %q, want %q", upper, tc.wantUpper)
			}
		})
	}
}

// The expiring view, which is the derived state ADR-0010 §6 refuses to store.
func TestTheExpiringViewIsDerivedAndSecurityInvoker(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx,
		`SELECT pg_get_viewdef('lease_expiring'::regclass, true)`).Scan(&def); err != nil {
		t.Fatalf("reading lease_expiring: %v — ADR-0010 §6 is this view", err)
	}
	// current_date rather than a stored column: that is what "derived" means, and it is
	// why there is no job to keep it right. Matched case-insensitively because
	// pg_get_viewdef renders it as CURRENT_DATE, which the first version of this check
	// missed.
	if !strings.Contains(strings.ToLower(def), "current_date") {
		t.Errorf("the view does not read the clock, so something has to maintain it: %s", def)
	}
	// Only live tenancies. A draft with a past end date is not a tenancy running out.
	for _, s := range []string{"active", "in_notice"} {
		if !strings.Contains(def, "'"+s+"'") {
			t.Errorf("the view does not cover %s leases", s)
		}
	}
	for _, s := range []string{"draft", "lapsed", "settled"} {
		if strings.Contains(def, "'"+s+"'") {
			t.Errorf("the view covers %s leases, which are not tenancies running out", s)
		}
	}
	// Assertion 10 requires this of every view; asserting it here as well means the
	// failure names this view rather than the whole bootstrap.
	var invoker bool
	if err := p.QueryRow(ctx, `
		SELECT coalesce(c.reloptions @> ARRAY['security_invoker=true'], false)
		  FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		 WHERE n.nspname = 'public' AND c.relname = 'lease_expiring'`).Scan(&invoker); err != nil {
		t.Fatalf("reading reloptions: %v", err)
	}
	if !invoker {
		t.Error("lease_expiring is not security_invoker, so it would under-report for every " +
			"delegated session")
	}
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}
