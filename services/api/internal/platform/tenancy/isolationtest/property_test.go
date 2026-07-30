package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0009. The property tree is where scope stops being a vocabulary and starts
// being a policy: these are the first tables a grant reaches into, and the first
// place where "two of the five units" has to mean exactly two.
func TestPropertyScope(t *testing.T) {
	p := pool(t)
	isolationtest.RunPropertyScope(t, p, platformPool(t))
}

// The ADR-0003 five-part contract, for each table of the tree. The delegated
// branch of each policy widens what a session can see, so the base isolation
// property — organisation A cannot see organisation B's rows — has to be
// re-asserted rather than assumed to survive.
func TestPropertyTreeIsolation(t *testing.T) {
	p := pool(t)
	seedOrganisations(t, platformPool(t))

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "properties",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO properties (tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin)
				VALUES ($1, $2, 'Isolation Harness', 'standalone',
				        '1 Test Road', 'Locality', 'Bengaluru', 'KA', '560001')`,
				tenant.String(), token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `SELECT count(*) FROM properties WHERE code = $1`, token).Scan(&n)
			return n, err
		},
	})

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "units",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			// A unit needs a property, and the property must belong to the same
			// organisation — units_property_fkey is composite for exactly that
			// reason, so this insert cannot cheat by reusing another one's.
			var propertyID string
			if err := tx.QueryRow(ctx, `
				INSERT INTO properties (tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin)
				VALUES ($1, $2, 'Isolation Harness', 'building',
				        '1 Test Road', 'Locality', 'Bengaluru', 'KA', '560001')
				RETURNING id`, tenant.String(), "p-"+token).Scan(&propertyID); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO units (tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES ($1, $2, 'flat', $3, 500)`, tenant.String(), propertyID, token)
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `SELECT count(*) FROM units WHERE code = $1`, token).Scan(&n)
			return n, err
		},
	})
}
