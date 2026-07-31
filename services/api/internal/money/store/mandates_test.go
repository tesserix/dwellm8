package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/mandate"
)

// The drift check between the two copies of ADR-0022's lifecycle, and the price
// of keeping it in Go as well as in PostgreSQL.
//
// The failure it prevents is quiet in both directions. If the schema refuses a
// transition Go performs, an authority stops advancing while every log line says
// the webhook was handled. If the schema allows one Go thinks impossible, a
// path that never went through Go — a fix applied by hand at 2am, a job written
// later — can walk a revoked mandate back to active, which is a standing
// authority to debit somebody who cancelled.

func TestTheGoMandateMachineMatchesTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	statuses := mandate.Statuses()
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}

	rows, err := p.Query(ctx, `
		SELECT f.s, t.s, mandate_transition_allowed(f.s, t.s)
		  FROM unnest($1::text[]) AS f(s)
		  CROSS JOIN unnest($1::text[]) AS t(s)
		 ORDER BY f.s, t.s`, names)
	if err != nil {
		t.Fatalf("evaluating mandate_transition_allowed: %v — ADR-0022 §3 requires it", err)
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
		if inGo := mandate.CanTransition(mandate.Status(from), mandate.Status(to)); inGo != inDB {
			t.Errorf("%s -> %s: Go says %v and the database says %v", from, to, inGo, inDB)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if want := len(statuses) * len(statuses); pairs != want {
		t.Fatalf("checked %d pairs, want %d — the cross product did not run", pairs, want)
	}
}

// The cycle, asserted against the database specifically. It is the one place
// the mandate machine differs from the payment machine, so it is the one most
// likely to be "fixed" by somebody applying ADR-0011's forward-only rule here.
func TestTheDatabaseAllowsAnAuthorityToResume(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var pauseThenResume, revokedRevives bool
	if err := p.QueryRow(ctx, `
		SELECT mandate_transition_allowed('paused', 'active'),
		       mandate_transition_allowed('revoked', 'active')`).
		Scan(&pauseThenResume, &revokedRevives); err != nil {
		t.Fatalf("querying: %v", err)
	}
	if !pauseThenResume {
		t.Error("the database refuses paused -> active, so a tenant off a payment holiday must re-authorise")
	}
	if revokedRevives {
		t.Error("the database allows revoked -> active, which is a cancelled authority coming back to life")
	}
}

func TestTheGoMandateVocabulariesAreAcceptedByTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	def := func(name string) string {
		var out string
		if err := p.QueryRow(ctx,
			`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, name).Scan(&out); err != nil {
			t.Fatalf("reading constraint %s: %v", name, err)
		}
		return out
	}

	rails := def("mandates_rail_check")
	for _, r := range mandate.Rails() {
		if !strings.Contains(rails, "'"+string(r)+"'") {
			t.Errorf("rail %q is producible in Go and refused by mandates_rail_check", r)
		}
	}

	statuses := def("mandates_status_check")
	for _, s := range mandate.Statuses() {
		if !strings.Contains(statuses, "'"+string(s)+"'") {
			t.Errorf("status %q is producible in Go and refused by mandates_status_check", s)
		}
	}
}

// The two indexes that carry a rule rather than a query plan.
func TestTheMandateIndexesThatAreRules(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	indexDef := func(name string) string {
		var def string
		if err := p.QueryRow(ctx,
			`SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = $1`,
			name).Scan(&def); err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		return def
	}

	idem := indexDef("mandates_idempotency_idx")
	if !strings.Contains(idem, "UNIQUE") {
		t.Errorf("mandates_idempotency_idx is not unique: %s", idem)
	}

	// Two active authorities on one flat debit a tenant twice on the first of the
	// month, and both look correct from every screen.
	active := indexDef("mandates_one_active_per_unit_idx")
	if !strings.Contains(active, "UNIQUE") {
		t.Errorf("mandates_one_active_per_unit_idx is not unique: %s", active)
	}
	for _, want := range []string{"tenant_id", "unit_id", "active"} {
		if !strings.Contains(active, want) {
			t.Errorf("mandates_one_active_per_unit_idx does not mention %s: %s", want, active)
		}
	}
}
