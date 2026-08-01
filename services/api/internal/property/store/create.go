package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
)

// ErrCodeTaken is a second building with the same short code — refused by
// name, because the fix is a different code, not a retry.
var ErrCodeTaken = errors.New("property: that code is already one of your properties")

// ErrUnitCodeTaken is the same refusal one level down (issue #10's scenario).
var ErrUnitCodeTaken = errors.New("property: that unit code already exists in this property")

// Create registers a property and publishes property.property.registered in
// the same transaction. RLS's WITH CHECK is what pins the row to the session's
// organisation — the tenant id is never taken from the caller's body.
func (s *Properties) Create(ctx context.Context, d domain.PropertyDraft) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}

	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO properties (tenant_id, code, name, kind, address_line1, address_line2,
			                        locality, city, district, state_code, pin)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id::text`,
			tenant.String(), d.Code, d.Name, d.Kind, d.AddressLine1, nullable(d.AddressLine2),
			d.Locality, d.City, nullable(d.District), d.StateCode, d.Pin).Scan(&id); err != nil {
			return err
		}
		env, err := events.New("property.property.registered", tenant.String(),
			events.Subject{Kind: "property", ID: id},
			events.Actor{Kind: events.ActorSystem},
			struct {
				Code string `json:"code"`
				Kind string `json:"kind"`
				City string `json:"city"`
			}{d.Code, d.Kind, d.City})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
	if isUnique(err, "properties_code_unique") {
		return "", ErrCodeTaken
	}
	if err != nil {
		return "", fmt.Errorf("property: registering %s: %w", d.Code, err)
	}
	return id, nil
}

// CreateUnit adds a lettable unit (or the parking that comes with one) to a
// property this session may write. The composite foreign keys refuse a parent
// in another building or another organisation without this code saying so.
func (s *Properties) CreateUnit(ctx context.Context, propertyID string, d domain.UnitDraft) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}

	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO units (tenant_id, property_id, unit_kind, code, floor,
			                   carpet_area_sqft, parent_unit_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING id::text`,
			tenant.String(), propertyID, d.Kind, d.Code, d.Floor,
			nullPositive(d.CarpetAreaSqft), nullable(d.ParentUnitID)).Scan(&id); err != nil {
			return err
		}
		env, err := events.New("property.unit.added", tenant.String(),
			events.Subject{Kind: "unit", ID: id},
			events.Actor{Kind: events.ActorSystem},
			struct {
				PropertyID string `json:"property_id"`
				Code       string `json:"code"`
				Kind       string `json:"kind"`
			}{propertyID, d.Code, d.Kind})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
	switch {
	case isUnique(err, "units_code_unique"):
		return "", ErrUnitCodeTaken
	case isForeignKey(err):
		// The property is not this session's to touch, or the parent unit is
		// not in it. One answer, deliberately — see ErrNoProperty.
		return "", ErrNoProperty
	case err != nil:
		return "", fmt.Errorf("property: adding unit %s: %w", d.Code, err)
	}
	return id, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullPositive(f float64) any {
	if f <= 0 {
		return nil
	}
	return f
}

func isUnique(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == constraint
}

func isForeignKey(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "23503" || pg.Code == "42501")
}
