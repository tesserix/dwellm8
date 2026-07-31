package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// The drift check for ADR-0024's two facts.
//
// The vocabulary exists in Go, because Select decides the section without a round
// trip, and in PostgreSQL, because a row that never went through Go must not be able
// to claim a deductor class nothing can act on. Two copies, one test — the same
// trade ADR-0010 makes for the state machine, and the same dangerous direction of
// drift: the schema refusing a class the product offers, so a lease cannot be
// completed and every log line says the form was filled in correctly.

func TestTheGoTDSVocabulariesAreAcceptedByTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	def := func(name string) string {
		var out string
		if err := p.QueryRow(ctx,
			`SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conname = $1`, name).Scan(&out); err != nil {
			t.Fatalf("reading constraint %s: %v — ADR-0024 requires it", name, err)
		}
		return out
	}

	classes := def("lease_tax_facts_deductor_class_check")
	for _, c := range tds.DeductorClasses() {
		if !strings.Contains(classes, "'"+string(c)+"'") {
			t.Errorf("deductor class %q is producible in Go and refused by the schema — a tenant of "+
				"that class cannot have a lease completed", c)
		}
	}

	residencies := def("lease_tax_facts_landlord_residency_check")
	for _, r := range tds.Residencies() {
		if !strings.Contains(residencies, "'"+string(r)+"'") {
			t.Errorf("residency %q is producible in Go and refused by the schema", r)
		}
	}
}

// The guard that makes the story's failure scenario structural: a transaction may
// not end with an active tenancy whose tax path is unknown, or with a section 195
// tenancy the deductor has not accepted.
//
// Deferred to commit, so this asserts the constraint trigger is deferrable — an
// immediate one would force the facts to be written before the lease they point at,
// which their own foreign key forbids.
func TestTheTaxPathGuardIsDeferredToCommit(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var deferrable, deferred bool
	err := p.QueryRow(ctx, `
		SELECT tgdeferrable, tginitdeferred
		  FROM pg_trigger WHERE tgname = 'leases_tax_path_known'`).Scan(&deferrable, &deferred)
	if err != nil {
		t.Fatalf("reading leases_tax_path_known: %v — ADR-0024 requires it: it is what stops a "+
			"tenancy starting with no TDS section governing its first payment", err)
	}
	if !deferrable || !deferred {
		t.Errorf("the guard is deferrable=%v initially deferred=%v: an immediate check would "+
			"require the tax facts to exist before the lease they reference", deferrable, deferred)
	}
}
