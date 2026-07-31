package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0022. A standing authority is a new data path, so it gets ADR-0003's
// five-part contract like everything else that holds tenant data.
//
// It is worth having separately from the payments contract for the reason
// ADR-0009's assertion 6 exists: mandates carries a NOT NULL unit_id, so a
// policy written at property granularity would let a firm delegated one flat
// read every standing authority in the tower — and a mandate is more than a
// disclosure, it is the record of what somebody is authorised to be debited.
func TestMandatesIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "mandates",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			unit, err := leaseUnit(ctx, tx, tenant, token)
			if err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `
				INSERT INTO mandates (tenant_id, property_id, unit_id, payer_kind, payer_id,
				                      rail, max_amount_minor, provider, idempotency_key)
				VALUES ($1, $2, $3, 'tenant', gen_random_uuid(), 'upi_autopay', 1500000,
				        'cashfree', $4)`,
				tenant.String(), collectionProperty(tenant), unit, token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM mandates WHERE idempotency_key = $1`, token).Scan(&n)
			return n, err
		},
	})
}

// Two live authorities on one flat is a tenant debited twice on the first of the
// month, and it is the kind of duplicate that looks correct from every screen:
// both mandates are real, both were authorised by somebody, and only the index
// says the second must not exist.
func TestAUnitHasAtMostOneLiveAuthority(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	token := randomToken(t)
	property, unit := seedLeaseUnit(t, plat)

	register := func(key, status string) error {
		return tenancy.Scoped(tenancy.With(ctx, isolationtest.OrgA), p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO mandates (tenant_id, property_id, unit_id, payer_kind, payer_id,
				                      rail, max_amount_minor, provider, provider_mandate_id,
				                      status, idempotency_key)
				VALUES ($1, $2, $3, 'tenant', gen_random_uuid(), 'upi_autopay', 1500000,
				        'cashfree', $4, $5, $6)`,
				isolationtest.OrgA.String(), property, unit, "mnd-"+key, status, key)
			return err
		})
	}

	if err := register(token+"-first", "active"); err != nil {
		t.Fatalf("registering the first authority: %v", err)
	}
	if err := register(token+"-second", "active"); err == nil {
		t.Error("a second active mandate was registered on the same unit — that tenant is now debited twice")
	}

	// A revoked one does not block a replacement, which is the whole point of the
	// index being partial: a tenancy that changes bank must be able to re-register.
	if err := register(token+"-revoked", "revoked"); err != nil {
		t.Errorf("a revoked authority was refused alongside a live one: %v", err)
	}
}
