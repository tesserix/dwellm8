package isolationtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0010 against PostgreSQL. The state machine is compared against Go's in
// internal/lease/store; what is here is the set of things only the database can
// promise, and the most important one in a rental product: you cannot let the same
// flat twice over the same days.

func TestLeasesIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "leases",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			// A unit per organisation per run, because the no-double-let constraint is
			// real and would otherwise refuse the harness's second insert — which would
			// pass the isolation test for the wrong reason.
			unit, err := leaseUnit(ctx, tx, tenant, token)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to,
				                    notice_days)
				VALUES ($1, $2, $3, 'draft', '2030-01-01', '2031-01-01', 30)`,
				tenant.String(), collectionProperty(tenant), unit)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `
				SELECT count(*) FROM leases l JOIN units u ON u.id = l.unit_id
				 WHERE u.code LIKE $1`, "LEASE-"+token[:8]+"%").Scan(&n)
			return n, err
		},
	})
}

// The constraint that matters most in a rental product. Two live tenancies of one flat
// over the same days is a double-let: two families with keys, two rent rolls, and a
// dispute nothing in the data can settle.
//
// It is scoped to states that are actually tenancies, so drafting a competing offer is
// still legal — which is the half that would be lost by making the constraint simpler.
func TestAFlatCannotBeLetTwiceOverTheSameDays(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	let := func(state, from, to string) error {
		return tenancy.Platform(ctx, plat, "letting a flat", func(ctx context.Context, tx pgx.Tx) error {
			var id string
			if err := tx.QueryRow(ctx, `
				INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, $4, $5::date, $6::date) RETURNING id`,
				isolationtest.OrgA.String(), prop, unit, state, from, to).Scan(&id); err != nil {
				return err
			}
			return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), id, from)
		})
	}

	if err := let("active", "2025-07-01", "2026-07-01"); err != nil {
		t.Fatalf("the first tenancy was refused: %v", err)
	}

	// Overlapping, and live. Refused.
	err := let("active", "2026-01-01", "2027-01-01")
	if err == nil {
		t.Fatal("the same flat was let to two live tenancies over the same days — two families " +
			"with keys, and nothing in the data says which is right")
	}
	if !strings.Contains(err.Error(), "leases_no_double_let") {
		t.Errorf("refused, but not by the constraint that means it: %v", err)
	}
	t.Logf("refused: %v", err)

	// Adjacent is fine: the successor starts the day the predecessor's term ends,
	// because the upper bound is exclusive. This is the renewal case.
	if err := let("active", "2026-07-01", "2027-07-01"); err != nil {
		t.Errorf("a tenancy starting the day the previous one ended was refused: %v — the bound is "+
			"exclusive, so they meet rather than overlap", err)
	}

	// And a competing draft over the same days is legal, because a draft is not a
	// tenancy. Refusing this would mean an owner cannot prepare a renewal while the
	// current tenancy runs.
	if err := let("draft", "2026-01-01", "2027-01-01"); err != nil {
		t.Errorf("a draft overlapping a live tenancy was refused: %v — two competing offers on one "+
			"flat is how letting works", err)
	}
	if err := let("pending_signature", "2026-02-01", "2027-02-01"); err != nil {
		t.Errorf("an unsigned lease overlapping a live tenancy was refused: %v", err)
	}
}

// The primary acceptance scenario, in the database. Terminating with effect from 20 June
// shortens the occupancy interval and leaves the agreement as signed.
func TestTerminatingShortensOccupancyAndLeavesTheAgreement(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	var id string
	if err := tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2025-07-01', '2026-07-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&id); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), id, "2025-07-01")
	}); err != nil {
		t.Fatalf("letting: %v", err)
	}

	if err := tenancy.Platform(ctx, plat, "terminating", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'terminated', ended_on = '2026-06-21',
			                  terminated_by = 'owner', terminated_reason = 'tenant relocating',
			                  settlement_decision = 'adjust'
			 WHERE id = $1`, id)
		return err
	}); err != nil {
		t.Fatalf("terminating: %v", err)
	}

	// The agreement is untouched and the occupancy is shorter. The generated column is
	// what billing reads, so this is the whole of "charges stop".
	var agreedTo, occupancyTo string
	if err := tenancy.Platform(ctx, plat, "reading back", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT to_char(valid_to, 'YYYY-MM-DD'), upper(validity)::text FROM leases WHERE id = $1`,
			id).Scan(&agreedTo, &occupancyTo)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if agreedTo != "2026-07-01" {
		t.Errorf("the agreed term now ends %s — terminating rewrote what the parties signed", agreedTo)
	}
	if occupancyTo != "2026-06-21" {
		t.Errorf("occupancy ends %s, want 2026-06-21", occupancyTo)
	}

	// So the flat is free from 21 June, and a new tenancy may start then even though the
	// old agreement ran to July.
	if err := tenancy.Platform(ctx, plat, "re-letting", func(ctx context.Context, tx pgx.Tx) error {
		var relet string
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2026-06-21', '2027-06-21') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&relet); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), relet, "2026-06-21")
	}); err != nil {
		t.Errorf("the flat could not be re-let from the day occupancy ceased: %v — the agreement "+
			"ran to July, and what matters is when the tenant left", err)
	}

	// A terminated tenancy must say who, why, and what happened to the money.
	prop2, unit2 := seedLeaseUnit(t, plat)
	var id2 string
	_ = tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2025-07-01', '2026-07-01') RETURNING id`,
			isolationtest.OrgA.String(), prop2, unit2).Scan(&id2); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), id2, "2025-07-01")
	})
	err := tenancy.Platform(ctx, plat, "terminating carelessly", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE leases SET state = 'terminated', ended_on = '2026-06-21' WHERE id = $1`, id2)
		return err
	})
	if err == nil {
		t.Error("a tenancy was terminated with no actor, no reason and no settlement decision")
	}
}

// The failure scenario, and the half of it the database enforces: a termination
// effective before the last invoiced period, with no decision, is refused by asking the
// ledger what has been billed.
func TestARetrospectiveTerminationIsRefusedByAskingTheLedger(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	var id string
	if err := tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2025-07-01', '2026-07-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&id); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), id, "2025-07-01")
	}); err != nil {
		t.Fatalf("letting: %v", err)
	}

	// A rent charge raised through June, naming the lease it bills.
	//
	// lease_id is a foreign key rather than a string convention, which is the whole
	// point: the first version of this trigger matched on source_kind = 'lease_charge'
	// AND source_id, so it depended on invoicing following a convention nobody had
	// agreed to — and until they did, it found nothing and permitted everything.
	// journal_entries_lease_charge_shape now makes the pairing structural.
	if err := tenancy.Platform(ctx, plat, "raising a charge", func(ctx context.Context, tx pgx.Tx) error {
		var entry string
		if err := tx.QueryRow(ctx, `
			INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind, source_id,
			                             lease_id, idempotency_key, memo)
			VALUES ($1, 'invoice', '2026-06-01', 'lease_charge', $2, $4::uuid, $3, 'June rent')
			RETURNING id`,
			isolationtest.OrgA.String(), id, "charge-"+id, id).Scan(&entry); err != nil {
			return err
		}
		for _, p := range []struct{ account, side, party, partyID string }{
			{"tenant_receivable", "debit", "tenant", "aaaaaaaa-0000-4000-8000-00000000000a"},
			{"rent_income", "credit", "owner", "bbbbbbbb-0000-4000-8000-00000000000b"},
		} {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ledger_postings (entry_id, tenant_id, property_id, unit_id, account_code,
				                             side, amount_minor, party_kind, party_id)
				VALUES ($1, $2, $3, $4, $5, $6, 2500000, $7, $8)`,
				entry, isolationtest.OrgA.String(), prop, unit,
				p.account, p.side, p.party, p.partyID); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatalf("raising a charge: %v", err)
	}

	// Ending in May, when June has already been charged, with no decision.
	err := tenancy.Platform(ctx, plat, "ending retrospectively", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'terminated', ended_on = '2026-05-21',
			                  terminated_by = 'owner', terminated_reason = 'left early',
			                  settlement_decision = 'none'
			 WHERE id = $1`, id)
		return err
	})
	if err == nil {
		t.Fatal("a termination leaving an over-billed period was accepted with no decision — the " +
			"charge sits against a tenant who does not occupy the flat, and nobody chose that")
	}
	if !strings.Contains(err.Error(), "over-billed") {
		t.Errorf("refused, but not by the retrospective-end trigger: %v", err)
	}
	t.Logf("refused: %v", err)

	// The half of the pairing that is enforced: a charge names its lease, or the
	// trigger goes inert again.
	t.Run("a lease charge that names no lease", func(t *testing.T) {
		err := tenancy.Platform(ctx, plat, "a mispaired entry", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind,
				                             source_id, lease_id, idempotency_key, memo)
				VALUES ($1, 'invoice', '2026-06-01', 'lease_charge', $2, NULL, $3, 'x')`,
				isolationtest.OrgA.String(), id, "mispaired-"+id)
			return err
		})
		if err == nil {
			t.Fatal("accepted a lease charge naming no lease — the trigger goes inert again the " +
				"moment that pairing is optional")
		}
		if !strings.Contains(err.Error(), "journal_entries_lease_charge_shape") {
			t.Errorf("refused, but not by the pairing constraint: %v", err)
		}
	})

	// With a decision it is accepted, because somebody has now decided.
	if err := tenancy.Platform(ctx, plat, "ending with a decision", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'terminated', ended_on = '2026-05-21',
			                  terminated_by = 'owner', terminated_reason = 'left early',
			                  settlement_decision = 'refund'
			 WHERE id = $1`, id)
		return err
	}); err != nil {
		t.Errorf("a termination with an explicit refund decision was refused: %v", err)
	}
}

// ADR-0006 §5 amendment: a receipt names the lease it pays, and the guard reads
// charges only.
//
// The defect this exists to catch is a quiet one. Drop the source_kind filter and
// the last payment becomes the billed-through date, so a tenant who clears their
// arrears on the 1st of July makes a termination on the 15th of June look
// over-billed — a refusal nobody can explain, on a lease that owes nothing.
func TestTheRetrospectiveGuardReadsChargesNotReceipts(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	var id string
	if err := tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2025-07-01', '2026-07-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&id); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), id, "2025-07-01")
	}); err != nil {
		t.Fatalf("letting: %v", err)
	}

	// Charged for June, paid on the first of July.
	charge := leaseEntry{kind: "invoice", source: "lease_charge", on: "2026-06-01",
		lines: [2]string{"tenant_receivable:debit", "rent_income:credit"}}
	receipt := leaseEntry{kind: "payment", source: "payment", on: "2026-07-01",
		lines: [2]string{"gateway_clearing:debit", "tenant_receivable:credit"}}
	for _, e := range []leaseEntry{charge, receipt} {
		if err := e.write(ctx, plat, id, prop, unit); err != nil {
			t.Fatalf("writing the %s: %v", e.kind, err)
		}
	}

	// Ending after the last charge and before the receipt. Nothing is over-billed.
	if err := tenancy.Platform(ctx, plat, "ending after the last charge", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'terminated', ended_on = '2026-06-15',
			                  terminated_by = 'tenant', terminated_reason = 'moved',
			                  settlement_decision = 'none'
			 WHERE id = $1`, id)
		return err
	}); err != nil {
		t.Fatalf("a termination after the last charge was refused: %v — the guard is reading "+
			"receipts as if they were charges", err)
	}
}

// leaseEntry is one two-line entry against a lease, for the guard's fixtures.
type leaseEntry struct {
	kind, source, on string
	lines            [2]string
}

func (e leaseEntry) write(ctx context.Context, plat tenancy.PlatformPool, lease, prop, unit string) error {
	return tenancy.Platform(ctx, plat, "writing a lease entry", func(ctx context.Context, tx pgx.Tx) error {
		var entry string
		if err := tx.QueryRow(ctx, `
			INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind, source_id,
			                             lease_id, idempotency_key)
			VALUES ($1, $2, $3::date, $4, $5, $6::uuid, $7) RETURNING id`,
			isolationtest.OrgA.String(), e.kind, e.on, e.source, lease, lease,
			e.source+"-"+e.on+"-"+lease).Scan(&entry); err != nil {
			return err
		}
		for _, line := range e.lines {
			account, side, _ := strings.Cut(line, ":")
			party, partyID := "tenant", "aaaaaaaa-0000-4000-8000-00000000000a"
			switch account {
			case "rent_income":
				party, partyID = "owner", "bbbbbbbb-0000-4000-8000-00000000000b"
			case "gateway_clearing":
				party, partyID = "platform", "00000000-0000-0000-0000-0000000000d8"
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO ledger_postings (entry_id, tenant_id, property_id, unit_id, account_code,
				                             side, amount_minor, party_kind, party_id)
				VALUES ($1, $2, $3, $4, $5, $6, 2500000, $7, $8)`,
				entry, isolationtest.OrgA.String(), prop, unit, account, side, party, partyID); err != nil {
				return err
			}
		}
		return nil
	})
}

// Renewal is a new lease that names its predecessor and starts exactly where it ends.
// The story's edge case: the predecessor keeps its id, so its ledger history stays
// attached to it.
func TestRenewalIsContiguousAndAtMostOnce(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	var first string
	if err := tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'renewed', '2025-07-01', '2026-07-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&first)
	}); err != nil {
		t.Fatalf("letting: %v", err)
	}

	renew := func(from, to string) error {
		return tenancy.Platform(ctx, plat, "renewing", func(ctx context.Context, tx pgx.Tx) error {
			var successor string
			if err := tx.QueryRow(ctx, `
				INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to,
				                    renews_lease_id)
				VALUES ($1, $2, $3, 'active', $4::date, $5::date, $6) RETURNING id`,
				isolationtest.OrgA.String(), prop, unit, from, to, first).Scan(&successor); err != nil {
				return err
			}
			return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), successor, from)
		})
	}

	// A gap leaves a day unbilled.
	if err := renew("2026-08-01", "2027-08-01"); err == nil {
		t.Error("a renewal starting a month after the previous tenancy ended was accepted, leaving " +
			"July unbilled and the flat notionally empty")
	}
	// An overlap is two tenancies of one flat — caught by the contiguity trigger before
	// the exclusion constraint even sees it.
	if err := renew("2026-06-01", "2027-06-01"); err == nil {
		t.Error("a renewal overlapping the previous tenancy was accepted")
	}
	// Exactly contiguous is right.
	if err := renew("2026-07-01", "2027-07-01"); err != nil {
		t.Fatalf("a contiguous renewal was refused: %v", err)
	}
	// And a lease is renewed at most once: two successors would each claim the tenancy
	// continued, and the deposit would have two places to go.
	err := renew("2026-07-01", "2027-07-01")
	if err == nil {
		t.Error("the same tenancy was renewed twice")
	} else if !strings.Contains(err.Error(), "leases_one_renewal") &&
		!strings.Contains(err.Error(), "leases_no_double_let") {
		t.Errorf("refused, but not by the one-renewal index or the double-let constraint: %v", err)
	}
}

// The agreement is what the parties signed. Only ended_on may shorten the tenancy, and
// only once.
func TestTheAgreedTermCannotBeEdited(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	var id string
	if err := tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2025-07-01', '2026-07-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&id); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), id, "2025-07-01")
	}); err != nil {
		t.Fatalf("letting: %v", err)
	}

	edit := func(set string) error {
		return tenancy.Platform(ctx, plat, "editing", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE leases SET `+set+` WHERE id = $1`, id)
			return err
		})
	}

	for _, tc := range []struct{ name, set, want string }{
		{"extending the agreed term", "valid_to = '2027-07-01'", "agreed term edited"},
		{"moving the start date", "valid_from = '2025-01-01'", "agreed term edited"},
		{"moving the tenancy to another unit", "unit_id = gen_random_uuid()", "another unit"},
		{"walking the state backwards", "state = 'draft'", "cannot go from active to draft"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := edit(tc.set)
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("refused for the wrong reason: %v", err)
			}
		})
	}

	// Notice served, then withdrawn — the one backward edge, and it needs a reason.
	if err := edit("state = 'in_notice'"); err != nil {
		t.Fatalf("serving notice was refused: %v", err)
	}
	if err := edit("state = 'active'"); err == nil {
		t.Error("notice was withdrawn with no reason recorded, and it is the one transition that " +
			"makes the history non-monotonic")
	}
	if err := edit("state = 'active', notice_withdrawn_reason = 'terms renegotiated'"); err != nil {
		t.Errorf("withdrawing notice with a reason was refused: %v", err)
	}
}

// One rent at a time, and the story's own scenario: 25,000 revised to 27,000 effective
// 1 April, answered from the same table on both sides of the boundary.
func TestARentRevisionIsTwoRowsAndOneAnswerPerDay(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	var id string
	if err := tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2026-01-01', '2027-01-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&id); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), id, "2026-01-01")
	}); err != nil {
		t.Fatalf("letting: %v", err)
	}

	if err := tenancy.Platform(ctx, plat, "the revision", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, valid_from, valid_to)
			VALUES ($1, $2, 2500000, '2026-01-01', '2026-04-01')`,
			isolationtest.OrgA.String(), id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, valid_from)
			VALUES ($1, $2, 2700000, '2026-04-01')`, isolationtest.OrgA.String(), id)
		return err
	}); err != nil {
		t.Fatalf("the revision was refused: %v", err)
	}

	rent := func(on string) int64 {
		t.Helper()
		var amount int64
		if err := tenancy.Platform(ctx, plat, "as-of", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT amount_minor FROM rent_schedule
				 WHERE lease_id = $1 AND retired_at IS NULL AND validity @> $2::date`,
				id, on).Scan(&amount)
		}); err != nil {
			t.Fatalf("as-of %s: %v", on, err)
		}
		return amount
	}

	for _, tc := range []struct {
		on   string
		want int64
	}{
		{"2026-03-15", 2500000},
		{"2026-04-15", 2700000},
		{"2026-03-31", 2500000},
		{"2026-04-01", 2700000},
	} {
		if got := rent(tc.on); got != tc.want {
			t.Errorf("rent on %s is %d, want %d", tc.on, got, tc.want)
		}
	}

	// And a third row overlapping either of them is refused, so no day ever has two
	// answers.
	err := tenancy.Platform(ctx, plat, "an overlapping rent", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, valid_from, valid_to)
			VALUES ($1, $2, 2600000, '2026-02-01', '2026-06-01')`,
			isolationtest.OrgA.String(), id)
		return err
	})
	if err == nil {
		t.Fatal("two rents were live on the same day, so an invoice would bill whichever row the " +
			"planner reached first")
	}
	if !strings.Contains(err.Error(), "rent_schedule_no_overlap") {
		t.Errorf("refused, but not by the exclusion constraint: %v", err)
	}
}

// seedLeaseUnit creates a property and a lettable unit for one test.
func seedLeaseUnit(t *testing.T, plat tenancy.PlatformPool) (property, unit string) {
	t.Helper()
	ctx := context.Background()
	tok := randomToken(t)
	err := tenancy.Platform(ctx, plat, "seeding a lease unit", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO properties (id, tenant_id, code, name, kind, address_line1,
			                        locality, city, state_code, pin)
			VALUES (gen_random_uuid(), $1, $2, 'Lease Harness', 'building', '1 Harness Road',
			        'Indiranagar', 'Bengaluru', 'KA', '560038')
			RETURNING id`, isolationtest.OrgA.String(), "LEASEP-"+tok).Scan(&property); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
			VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 650) RETURNING id`,
			isolationtest.OrgA.String(), property, "LEASE-"+tok).Scan(&unit)
	})
	if err != nil {
		t.Fatalf("seeding a lease unit: %v", err)
	}
	return property, unit
}

// leaseUnit creates a unit inside the harness's own transaction, for the five-part
// contract. It has to be in the same transaction so the scoped session can see it.
func leaseUnit(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) (string, error) {
	var unit string
	err := tx.QueryRow(ctx, `
		INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
		VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 650) RETURNING id`,
		tenant.String(), collectionProperty(tenant),
		"LEASE-"+token[:8]+"-"+string(tenant)[:1]).Scan(&unit)
	return unit, err
}
