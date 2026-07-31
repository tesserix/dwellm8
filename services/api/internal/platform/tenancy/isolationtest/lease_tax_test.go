package isolationtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0024 against PostgreSQL: the two facts that decide the TDS section, and the
// tenancy that does not start without them.
//
// Go refuses the same thing in lease/domain, and this is the half that holds when
// the write did not come through Go — a backfill, a support fix, an import.

// The story's failure scenario, in the database. A tenancy whose deductor class and
// landlord residency nobody recorded cannot go live, because every payment made
// under it would be a deduction nobody computed.
func TestATenancyCannotStartWithNoTaxPath(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	err := tenancy.Platform(ctx, plat, "letting with nothing recorded",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, 'active', '2026-04-01', '2027-04-01')`,
				isolationtest.OrgA.String(), prop, unit)
			return err
		})
	if err == nil {
		t.Fatal("a tenancy started with no TDS section governing it — by the time the payout run " +
			"finds that, nine months of interest and penalty have accrued to a tenant nobody asked")
	}
	if !strings.Contains(err.Error(), "no TDS section") {
		t.Errorf("refused for the wrong reason: %v", err)
	}

	// A draft is a document being written and may legitimately be incomplete.
	if err := tenancy.Platform(ctx, plat, "drafting", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'draft', '2026-04-01', '2027-04-01')`,
			isolationtest.OrgA.String(), prop, unit)
		return err
	}); err != nil {
		t.Errorf("a draft with no tax facts was refused: %v — the facts are due when the tenancy "+
			"starts, not when the document is opened", err)
	}
}

// The other half of the failure scenario: a section 195 tenancy does not start
// until the deductor has accepted an obligation that runs from the first rupee and
// is entirely theirs.
func TestASection195TenancyCannotStartUnacknowledged(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	let := func(acknowledge bool) error {
		prop, unit := seedLeaseUnit(t, plat)
		return tenancy.Platform(ctx, plat, "letting to an NRI's flat",
			func(ctx context.Context, tx pgx.Tx) error {
				var lease string
				if err := tx.QueryRow(ctx, `
					INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
					VALUES ($1, $2, $3, 'active', '2026-04-01', '2027-04-01') RETURNING id`,
					isolationtest.OrgA.String(), prop, unit).Scan(&lease); err != nil {
					return err
				}
				if acknowledge {
					return isolationtest.SeedLeaseTaxFactsAs(ctx, tx, isolationtest.OrgA.String(),
						lease, "2026-04-01", "individual_no_audit", "non_resident")
				}
				_, err := tx.Exec(ctx, `
					INSERT INTO lease_tax_facts (tenant_id, lease_id, deductor_class,
					                             landlord_residency, source, valid_from)
					VALUES ($1, $2, 'individual_no_audit', 'non_resident', 'fixture', '2026-04-01')`,
					isolationtest.OrgA.String(), lease)
				return err
			})
	}

	err := let(false)
	if err == nil {
		t.Fatal("a section 195 tenancy started without the tenant accepting the obligation — this " +
			"is the path where an unaware tenant is most exposed")
	}
	if !strings.Contains(err.Error(), "acknowledged") {
		t.Errorf("refused for the wrong reason: %v", err)
	}

	if err := let(true); err != nil {
		t.Errorf("an acknowledged section 195 tenancy was refused: %v", err)
	}
}

// One set of facts true at a time, per lease. Two overlapping sets would mean two
// sections govern the same payment, and whichever sorted first would win.
func TestALeaseHasOneSetOfTaxFactsPerDay(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedLeaseUnit(t, plat)

	var lease string
	if err := tenancy.Platform(ctx, plat, "letting", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to)
			VALUES ($1, $2, $3, 'active', '2026-04-01', '2027-04-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit).Scan(&lease); err != nil {
			return err
		}
		return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgA.String(), lease, "2026-04-01")
	}); err != nil {
		t.Fatalf("letting: %v", err)
	}

	overlap := func(from string) error {
		return tenancy.Platform(ctx, plat, "revising the facts", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO lease_tax_facts (tenant_id, lease_id, deductor_class, landlord_residency,
				                             source, acknowledged_on, acknowledged_by, valid_from)
				VALUES ($1, $2, 'business', 'non_resident', 'landlord declaration',
				        $3::date, 'tenant', $3::date)`,
				isolationtest.OrgA.String(), lease, from)
			return err
		})
	}

	// The open-ended row already covers October, so a second one overlaps it.
	if err := overlap("2026-10-01"); err == nil {
		t.Fatal("two sets of tax facts are true on the same day — two sections govern the same " +
			"payment, and whichever sorted first would win")
	}

	// Closing the first is what makes the change legal, and the earlier months keep
	// the section they were deducted under.
	if err := tenancy.Platform(ctx, plat, "closing the earlier facts",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				UPDATE lease_tax_facts SET valid_to = '2026-10-01'
				 WHERE lease_id = $1 AND valid_to IS NULL`, lease)
			return err
		}); err != nil {
		t.Fatalf("closing the earlier facts: %v", err)
	}
	if err := overlap("2026-10-01"); err != nil {
		t.Errorf("a contiguous change of residency was refused: %v", err)
	}

	var sections int
	if err := tenancy.Platform(ctx, plat, "counting", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT count(*) FROM lease_tax_facts WHERE lease_id = $1 AND retired_at IS NULL`,
			lease).Scan(&sections)
	}); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if sections != 2 {
		t.Errorf("the tenancy has %d sets of facts, want 2 — April's rent was deducted under "+
			"194-I and that is not restated by the landlord leaving in October", sections)
	}
}
