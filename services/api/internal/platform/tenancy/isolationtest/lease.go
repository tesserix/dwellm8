package isolationtest

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SeedLeaseTaxFacts records the two facts ADR-0024 requires before a tenancy may
// start: what kind of payer the tenant is, and whether the landlord is a resident.
//
// Every fixture that lets a flat needs it, because the deferred constraint trigger
// on leases refuses a transaction that ends with an active tenancy whose tax path
// is unknown. That is the guard working: before it existed, a lease could go live
// with nothing recorded and the first payout would discover a section nobody had
// chosen.
//
// A business tenant and a resident landlord — section 194-I, the ordinary case —
// unless a test says otherwise. It is called inside the same transaction as the
// lease insert, and order does not matter there because the check is deferred to
// commit.
func SeedLeaseTaxFacts(ctx context.Context, tx pgx.Tx, tenant, leaseID, from string) error {
	return SeedLeaseTaxFactsAs(ctx, tx, tenant, leaseID, from, "business", "resident")
}

// SeedLeaseTaxFactsAs is the same for a test that cares which section it gets. A
// non-resident landlord is section 195, so it is acknowledged here — an
// unacknowledged one is refused, and a fixture asserting that refusal writes the
// row itself rather than going through this.
func SeedLeaseTaxFactsAs(ctx context.Context, tx pgx.Tx, tenant, leaseID, from, class, residency string) error {
	acknowledged := "NULL::date"
	by := "NULL"
	if residency == "non_resident" {
		acknowledged, by = "$3::date", "'fixture'"
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(`
		INSERT INTO lease_tax_facts (tenant_id, lease_id, deductor_class, landlord_residency,
		                             source, acknowledged_on, acknowledged_by, valid_from)
		VALUES ($1, $2, $4, $5, 'fixture', %s, %s, $3::date)`, acknowledged, by),
		tenant, leaseID, from, class, residency)
	if err != nil {
		return fmt.Errorf("seeding the lease's tax facts: %w", err)
	}
	return nil
}

// SeedConsent writes a consent artefact and returns its id.
//
// ADR-0026 gave kyc_verifications the foreign key its NOT NULL
// consent_artefact_id column had been promising since ADR-0013, so a fixture can
// no longer invent one. That is the constraint working: a verification with no
// consent behind it is one that should not have happened.
func SeedConsent(ctx context.Context, tx pgx.Tx, tenant, purpose string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO consent_artefacts (tenant_id, party_id, purpose, notice_version, language, channel)
		VALUES ($1, gen_random_uuid(), $2, 'fixture-1', 'en', 'app')
		RETURNING id`, tenant, purpose).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("seeding a consent artefact: %w", err)
	}
	return id, nil
}
