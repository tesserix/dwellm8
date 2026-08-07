package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
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

// The facts were written when the lease was created and never read back, so the
// section governing a payment could not be resolved at the moment money moved
// (#318). Read as at a date, because a landlord who moves abroad in October
// leaves April's payment on the section it was deducted under.
func TestTheFactsALeaseWasCreatedUnderCanBeReadBackAsAtADate(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	property, unitID := unit(t, plat, "TX-"+token())
	from := effective.Day(2029, 8, 5)

	s := store.New(req)
	created, err := s.Create(ctx, draft(t, property, unitID, from, effective.Day(2030, 8, 5), resident(from)))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	got, err := s.TaxFacts(ctx, created.ID, effective.Day(2029, 12, 1))
	if err != nil {
		t.Fatalf("reading the facts back: %v", err)
	}
	if got.Deductor != tds.Business || got.Residency != tds.Resident {
		t.Errorf("read back as %+v, and those two facts select the section", got)
	}
}

// A landlord of this test's own, so the aggregate is over tenancies this test
// created and not over whatever else the fixture has let.
func ownerParty(t *testing.T, plat tenancy.PlatformPool) string {
	t.Helper()
	var id string
	err := tenancy.Platform(context.Background(), plat, "minting a landlord",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id)
		})
	if err != nil {
		t.Fatalf("minting a landlord: %v", err)
	}
	return id
}

func ownUnit(t *testing.T, plat tenancy.PlatformPool, unitID, owner string) {
	t.Helper()
	err := tenancy.Platform(context.Background(), plat, "recording ownership",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id, valid_from)
				VALUES ($1, $2, $3, $4, date '2029-01-01')`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unitID, owner)
			return err
		})
	if err != nil {
		t.Fatalf("recording ownership: %v", err)
	}
}

// Section 194-I's threshold is tested against the year's rent to the *payee*,
// not to the property: two flats let by one owner are one threshold, and testing
// them separately is the classic under-deduction (#318). Two tenancies here, one
// owner, and the answer is their sum over the financial year.
func TestTheYearsRentToAnOwnerIsAggregatedAcrossTheirTenancies(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	from := effective.Day(2029, 8, 5)
	owner := ownerParty(t, plat)

	s := store.New(req)
	for _, code := range []string{"TA-" + token(), "TB-" + token()} {
		property, unitID := unit(t, plat, code)
		ownUnit(t, plat, unitID, owner)
		created, err := s.Create(ctx, draft(t, property, unitID, from,
			effective.Day(2030, 8, 5), resident(from)))
		if err != nil {
			t.Fatalf("creating a tenancy on %s: %v", code, err)
		}
		if err := send(ctx, req, created.ID); err != nil {
			t.Fatalf("sending the tenancy on %s out for signature: %v", code, err)
		}
		if err := s.Activate(ctx, created.ID, domain.ActorOwner); err != nil {
			t.Fatalf("activating the tenancy on %s: %v", code, err)
		}
	}

	// The financial year holding December 2029 opens on 1 April 2029. The
	// tenancies run from 5 August, so eight of its months are let, at ₹27,500
	// each on two flats.
	got, err := s.AnnualRentToOwner(ctx, owner, effective.Day(2029, 12, 1))
	if err != nil {
		t.Fatalf("aggregating the year: %v", err)
	}
	const want = 2 * 8 * 27_500_00
	if got != want {
		t.Errorf("the year to this owner came to %d paise, want %d — the threshold is tested "+
			"against this figure, so a low answer under-deducts", got, want)
	}
}

func TestAnOwnerWithNoTenancyInTheYearIsAggregatedToNothing(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)

	got, err := store.New(req).AnnualRentToOwner(ctx, ownerParty(t, plat), effective.Day(2029, 12, 1))
	if err != nil {
		t.Fatalf("aggregating the year: %v", err)
	}
	if got != 0 {
		t.Errorf("an owner with nothing let was aggregated to %d paise", got)
	}
}

func TestALeaseHasNoTaxFactsBeforeItsTermBegins(t *testing.T) {
	req, plat := pools(t)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	property, unitID := unit(t, plat, "TX-"+token())
	from := effective.Day(2029, 8, 5)

	s := store.New(req)
	created, err := s.Create(ctx, draft(t, property, unitID, from, effective.Day(2030, 8, 5), resident(from)))
	if err != nil {
		t.Fatalf("creating: %v", err)
	}

	if _, err := s.TaxFacts(ctx, created.ID, effective.Day(2029, 1, 1)); err == nil {
		t.Error("a date before the facts began answered with facts rather than saying it had none")
	}
}
