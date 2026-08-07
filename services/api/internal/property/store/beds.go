package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
)

// Beds reads a property's beds — the lettable thing in a hostel or a PG
// (#299). Sharing is the count of beds in the room, so a manager reading a
// board sees "3-share" without a second query.
func (s *Properties) Beds(ctx context.Context, propertyID string) ([]domain.Bed, error) {
	var out []domain.Bed
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT b.id::text, b.label::text, u.id::text, u.code::text,
			       coalesce(u.floor, 0), b.rent_amount_minor, b.state,
			       coalesce(b.lease_id::text, ''),
			       (SELECT count(*) FROM beds sib WHERE sib.unit_id = b.unit_id)
			  FROM beds b JOIN units u ON u.id = b.unit_id
			 WHERE b.property_id = $1::uuid
			 ORDER BY u.floor, u.code, b.label`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b domain.Bed
			if err := rows.Scan(&b.ID, &b.Label, &b.UnitID, &b.Room, &b.Floor,
				&b.RentMinor, &b.State, &b.LeaseID, &b.Sharing); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("property: listing beds of %s: %w", propertyID, err)
	}
	return out, nil
}

// AddBed puts a bed in a room. The property is read from the room rather than
// taken from the caller: a bed in one building filed against another is a
// board that never reconciles.
func (s *Properties) AddBed(ctx context.Context, unitID, label string, rentMinor int64) (domain.Bed, error) {
	var b domain.Bed
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO beds (tenant_id, property_id, unit_id, label, rent_amount_minor)
			SELECT u.tenant_id, u.property_id, u.id, $2, $3 FROM units u WHERE u.id = $1::uuid
			RETURNING id::text, label::text, unit_id::text, rent_amount_minor, state`,
			unitID, label, rentMinor).
			Scan(&b.ID, &b.Label, &b.UnitID, &b.RentMinor, &b.State)
	})
	if isUnique(err, "beds_label_unique") {
		return domain.Bed{}, fmt.Errorf("%w: %s", ErrBedExists, label)
	}
	if err != nil {
		return domain.Bed{}, fmt.Errorf("property: adding bed %s to unit %s: %w", label, unitID, err)
	}
	return b, nil
}

// AllocateBed moves a bed to a state, holding it against a lease where the
// state needs one. An empty lease id vacates it.
func (s *Properties) AllocateBed(ctx context.Context, bedID, state, leaseID string) error {
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE beds SET state = $2, lease_id = nullif($3, '')::uuid, updated_at = now()
			 WHERE id = $1::uuid`, bedID, state, leaseID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoBed
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("property: allocating bed %s: %w", bedID, err)
	}
	return nil
}
