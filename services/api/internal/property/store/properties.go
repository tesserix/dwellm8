// Package store is SQL for the property module's own tables — properties,
// blocks, units — and nothing else. Per property/doc.go, no other module may
// read these tables directly.
package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
)

// Properties is the module's store.
type Properties struct{ pool tenancy.Pool }

// New takes the request pool — a property read is always scoped to whoever is
// asking, self-owned or delegated, and row-level security is what tells the
// two apart. Never the platform pool.
func New(p tenancy.Pool) *Properties { return &Properties{pool: p} }

// ErrNoProperty is no such property — including one hidden by policy, which
// reads the same to a caller and is meant to.
var ErrNoProperty = errors.New("property: no such property")

const propertyColumns = `
	p.id::text, p.code::text, p.name, p.kind,
	p.address_line1, coalesce(p.address_line2, ''), p.locality, p.city, p.state_code, p.pin,
	(SELECT count(*) FROM units u
	  WHERE u.property_id = p.id AND u.is_ancillary = false AND u.state = 'active')`

// List reads every property the session may see — its own portfolio, plus
// whatever delegation grants it holds. No tenant_id filter here on purpose:
// a delegated row's tenant_id is the grantor's organisation, not the
// caller's, so filtering by the caller's own id here would hide exactly the
// rows the grant exists to show. RLS (properties_tenant_isolation) is what
// decides which rows exist for this session; the query just asks for all of
// them, same as leaseservice.Live does for leases.
func (s *Properties) List(ctx context.Context) ([]domain.Property, error) {
	var out []domain.Property
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+propertyColumns+`
			  FROM properties p
			 WHERE p.state = 'active'
			 ORDER BY p.created_at`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p domain.Property
			if err := rows.Scan(&p.ID, &p.Code, &p.Name, &p.Kind,
				&p.AddressLine1, &p.AddressLine2, &p.Locality, &p.City, &p.StateCode, &p.Pin,
				&p.UnitCount); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("property: listing: %w", err)
	}
	return out, nil
}

// Get reads one property by id.
func (s *Properties) Get(ctx context.Context, id string) (domain.Property, error) {
	var p domain.Property
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT `+propertyColumns+`
			  FROM properties p
			 WHERE p.id = $1::uuid`, id,
		).Scan(&p.ID, &p.Code, &p.Name, &p.Kind,
			&p.AddressLine1, &p.AddressLine2, &p.Locality, &p.City, &p.StateCode, &p.Pin,
			&p.UnitCount)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Property{}, ErrNoProperty
	case err != nil:
		return domain.Property{}, fmt.Errorf("property: reading %s: %w", id, err)
	}
	return p, nil
}

// Units reads a property's lettable units, in the order a manager reads a
// board: floor, then code. Ancillaries — parking, storage — are excluded for
// the same reason UnitCount excludes them: they are not separately let.
func (s *Properties) Units(ctx context.Context, propertyID string) ([]domain.Unit, error) {
	var out []domain.Unit
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, code::text, unit_kind, coalesce(floor, 0), occupancy,
			       coalesce(carpet_area_sqft, 0)::float8
			  FROM units
			 WHERE property_id = $1::uuid AND is_ancillary = false AND state = 'active'
			 ORDER BY floor, code`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var u domain.Unit
			if err := rows.Scan(&u.ID, &u.Code, &u.Kind, &u.Floor, &u.Occupancy, &u.CarpetSqf); err != nil {
				return err
			}
			out = append(out, u)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("property: listing units of %s: %w", propertyID, err)
	}
	return out, nil
}

// TenantOf returns the organisation id that holds this property — the row's
// tenant_id, which for a delegated read is the agency's organisation, not the
// caller's own. Callers outside this module use it to resolve a display name
// through identity's service, never by reading organisations here.
func (s *Properties) TenantOf(ctx context.Context, propertyID string) (string, error) {
	var tenantID string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT tenant_id::text FROM properties WHERE id = $1::uuid`, propertyID,
		).Scan(&tenantID)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", ErrNoProperty
	case err != nil:
		return "", fmt.Errorf("property: resolving tenant for %s: %w", propertyID, err)
	}
	return tenantID, nil
}
