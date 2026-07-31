package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Where a renter lives, across every landlord they rent from. ADR-0029 §3.
//
// This is the one query in the product that deliberately spans organisations on
// a customer's behalf, and it is the reason the resident surface exists at all:
// a tenant with a flat in Pune and another in Bengaluru has two landlords who
// must never learn of each other, and one person who has to pay both.
//
// It runs on the platform pool, for the same reason a sign-in does (ADR-0011
// §5): the request arrives knowing a person and not an organisation, so there is
// no tenant to scope the session by. What keeps it narrow is that it is keyed by
// the party id the token resolved to and returns nothing but lease ids — no
// amounts, no addresses, no other party. Everything the renter then reads is a
// second, tenant-scoped and resident-scoped query per lease.

// Residency is one tenancy a person is a tenant of.
type Residency struct {
	LeaseID string
	// TenantID is the landlord's or managing firm's organisation — whose rows
	// the lease's data is, and the scope every subsequent read runs under.
	TenantID tenancy.ID
	// Organisation is who the renter pays, by name. The only thing they are
	// shown about the other party.
	Organisation string
	// State is the lease's lifecycle state, so an ended tenancy can be shown as
	// history rather than as somewhere they still live.
	State string
}

// Residencies returns every lease the party is a tenant of, most recent first.
//
// Retired rows are excluded and expired ones are not. A tenancy that ended is
// still the person's own history — the receipts they may one day have to produce
// hang off it — whereas a retired row is a correction that said this was never
// their lease, and that must stop granting access at once.
func (s *Principals) Residencies(ctx context.Context, partyID string) ([]Residency, error) {
	if partyID == "" {
		// Not a database question. An empty party would match nothing, but it
		// would match nothing *after* asking, and a lookup for nobody is a bug in
		// the caller rather than an empty result.
		return nil, fmt.Errorf("identity: no party to find residencies for")
	}

	var out []Residency
	err := tenancy.Platform(ctx, s.platform, "resolving which tenancies a renter is on",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT lp.lease_id::text, lp.tenant_id::text, o.name, l.state
				  FROM lease_parties lp
				  JOIN leases l        ON l.id = lp.lease_id AND l.tenant_id = lp.tenant_id
				  JOIN organisations o ON o.id = lp.tenant_id
				 WHERE lp.party_id = $1::uuid
				   AND lp.role     = 'tenant'
				   AND lp.retired_at IS NULL
				 ORDER BY l.valid_from DESC, lp.lease_id`, partyID)
			if err != nil {
				return fmt.Errorf("identity: reading residencies: %w", err)
			}
			defer rows.Close()

			for rows.Next() {
				var r Residency
				var tenantID string
				if err := rows.Scan(&r.LeaseID, &tenantID, &r.Organisation, &r.State); err != nil {
					return err
				}
				r.TenantID = tenancy.ID(tenantID)
				out = append(out, r)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, err
	}
	return out, nil
}
