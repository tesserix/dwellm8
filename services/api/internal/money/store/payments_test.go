package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// The drift check between the two copies of ADR-0011's state machine.
//
// It exists in Go, so a handler can decide without a round trip, and in
// PostgreSQL, so an out-of-order webhook is refused even by a path that never
// went through Go. Two copies is a deliberate choice and this file is its price.
//
// The failure this prevents has no crash in it. The schema would accept a
// transition Go thinks is impossible, or refuse one Go performs — and the second
// is worse, because the payment silently stops advancing while every log line
// says the webhook was handled.

func TestTheGoStateMachineMatchesTheDatabase(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	statuses := collect.Statuses()
	names := make([]string, len(statuses))
	for i, s := range statuses {
		names[i] = string(s)
	}

	// Every ordered pair, including each status with itself — the no-op that
	// makes a redelivered webhook safe, and the one most likely to be dropped by
	// somebody tidying the function up.
	rows, err := p.Query(ctx, `
		SELECT f.s, t.s, payment_transition_allowed(f.s, t.s)
		  FROM unnest($1::text[]) AS f(s)
		  CROSS JOIN unnest($1::text[]) AS t(s)
		 ORDER BY f.s, t.s`, names)
	if err != nil {
		t.Fatalf("evaluating payment_transition_allowed: %v — ADR-0011 §3 requires it", err)
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
		if inGo := collect.CanTransition(collect.Status(from), collect.Status(to)); inGo != inDB {
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

// The vocabularies. A method or a status Go can produce and the schema's CHECK
// refuses is an error nobody sees until the collection that needs it is taken.
func TestTheGoPaymentVocabulariesAreAcceptedByTheSchema(t *testing.T) {
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

	methods := def("payments_method_check")
	for _, m := range collect.Methods() {
		if !strings.Contains(methods, "'"+string(m)+"'") {
			t.Errorf("method %q is producible in Go and refused by payments_method_check", m)
		}
	}

	statuses := def("payments_status_check")
	for _, s := range collect.Statuses() {
		if !strings.Contains(statuses, "'"+string(s)+"'") {
			t.Errorf("status %q is producible in Go and refused by payments_status_check", s)
		}
	}

	reasons := def("payment_events_park_reason_check")
	for _, r := range []collect.ParkReason{
		collect.ParkUnknownPayment, collect.ParkSignatureInvalid,
		collect.ParkStaleTransition, collect.ParkUnsupportedEvent,
	} {
		if !strings.Contains(reasons, "'"+string(r)+"'") {
			t.Errorf("park reason %q is producible in Go and refused by the schema", r)
		}
	}
}

// The idempotency guarantee is an index, not a handler. If it is ever dropped,
// three retries become three collections and nothing else in the system notices.
func TestTheIdempotencyIndexExists(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx,
		`SELECT indexdef FROM pg_indexes
		  WHERE schemaname = 'public' AND indexname = 'payments_idempotency_idx'`).Scan(&def); err != nil {
		t.Fatalf("reading payments_idempotency_idx: %v — ADR-0011 §2 is this index", err)
	}
	if !strings.Contains(def, "UNIQUE") {
		t.Errorf("payments_idempotency_idx is not unique: %s", def)
	}
	for _, col := range []string{"tenant_id", "idempotency_key"} {
		if !strings.Contains(def, col) {
			t.Errorf("payments_idempotency_idx does not cover %s: %s", col, def)
		}
	}
}
