package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
)

// What a property is like, and what is near it (#354).

// ErrNoPlace is no such nearby place — including one hidden by policy.
var ErrNoPlace = errors.New("property: no such place")

// ErrNotAllowed is a value the schema's vocabulary does not hold — an amenity
// nobody can filter on, a furnishing that is not one of the three, a distance
// that is not walkable. The manager can fix all of these; a 500 says they
// cannot.
var ErrNotAllowed = errors.New("property: that is not one of the values allowed here")

// ErrPlaceExists is the same school listed twice.
var ErrPlaceExists = errors.New("property: that place is already listed")

// isCheck is a CHECK constraint refusing a value the vocabulary lacks.
func isCheck(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23514"
}

// checkMessage is what the guard said, so the manager reads the rule rather
// than a status code.
func checkMessage(err error) string {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Message
	}
	return err.Error()
}

// SetPropertyDetail writes what the building is like. The amenity vocabulary
// is a CHECK in the schema, so an unknown one comes back as an error rather
// than as a filter that never matches.
func (s *Properties) SetPropertyDetail(ctx context.Context, propertyID string, d domain.PropertyDetail) error {
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE properties
			   SET about = nullif(btrim($2), ''), amenities = $3::text[], updated_at = now()
			 WHERE id = $1::uuid AND state = 'active'`,
			propertyID, d.About, nonNil(d.Amenities))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoProperty
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNoProperty) {
			return err
		}
		if isCheck(err) {
			return ErrNotAllowed
		}
		return fmt.Errorf("property: describing %s: %w", propertyID, err)
	}
	return nil
}

// SetUnitDetail writes what the flat is like.
func (s *Properties) SetUnitDetail(ctx context.Context, unitID string, d domain.UnitDetail) error {
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE units
			   SET about = nullif(btrim($2), ''), features = $3::text[],
			       floor = $4,
			       bathrooms = $5, balconies = $6, covered_parking = $7,
			       facing = nullif($8, ''), furnishing = nullif($9, ''), updated_at = now()
			 WHERE id = $1::uuid AND state = 'active'`,
			unitID, d.About, nonNil(d.Features), d.Floor,
			d.Bathrooms, d.Balconies, d.CoveredParking, d.Facing, d.Furnishing)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoUnit
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNoUnit) {
			return err
		}
		if isCheck(err) {
			return ErrNotAllowed
		}
		return fmt.Errorf("property: describing unit %s: %w", unitID, err)
	}
	return nil
}

// Places reads what is near a property, nearest first within each category —
// the order the question is asked in.
func (s *Properties) Places(ctx context.Context, propertyID string) ([]domain.Place, error) {
	var out []domain.Place
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, category, name, distance_m, travel_mode, tags,
			       coalesce(note, ''), position
			  FROM property_places
			 WHERE property_id = $1::uuid AND state = 'active'
			 ORDER BY position, distance_m, name`, propertyID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var p domain.Place
			if err := rows.Scan(&p.ID, &p.Category, &p.Name, &p.DistanceM,
				&p.TravelMode, &p.Tags, &p.Note, &p.Position); err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("property: reading what is near %s: %w", propertyID, err)
	}
	return out, nil
}

// AddPlace records one place near a property. The tenant is the property's own,
// so a firm adding through its mandate writes into the owner's record rather
// than its own — which is where the renter will read it from.
func (s *Properties) AddPlace(ctx context.Context, propertyID string, p domain.Place) (domain.Place, error) {
	mode := p.TravelMode
	if mode == "" {
		mode = "walk"
	}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO property_places
			       (tenant_id, property_id, category, name, distance_m, travel_mode, tags, note, position)
			SELECT pr.tenant_id, pr.id, $2, btrim($3), $4, $5, $6::text[], nullif(btrim($7), ''), $8
			  FROM properties pr
			 WHERE pr.id = $1::uuid AND pr.state = 'active'
			RETURNING id::text`,
			propertyID, p.Category, p.Name, p.DistanceM, mode, nonNil(p.Tags), p.Note, p.Position,
		).Scan(&p.ID)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Place{}, ErrNoProperty
	case isUnique(err, "property_places_once"):
		return domain.Place{}, ErrPlaceExists
	case isCheck(err):
		return domain.Place{}, ErrNotAllowed
	case err != nil:
		return domain.Place{}, fmt.Errorf("property: adding %s near %s: %w", p.Name, propertyID, err)
	}
	p.TravelMode = mode
	return p, nil
}

// UpdatePlace corrects a place that was recorded wrongly. A correction names
// what changed: anything left out keeps the value the row already holds, so
// re-measuring the walk does not cost the school its name.
func (s *Properties) UpdatePlace(ctx context.Context, p domain.Place) (domain.Place, error) {
	var out domain.Place
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `
			UPDATE property_places
			   SET category    = coalesce(nullif($2, ''), category),
			       name        = coalesce(nullif(btrim($3), ''), name),
			       distance_m  = coalesce(nullif($4, 0), distance_m),
			       travel_mode = coalesce(nullif($5, ''), travel_mode),
			       tags        = coalesce($6::text[], tags),
			       note        = coalesce(nullif(btrim($7), ''), note),
			       position    = coalesce(nullif($8, 0), position),
			       updated_at  = now()
			 WHERE id = $1::uuid AND state = 'active'
			RETURNING id::text, category, name, distance_m, travel_mode, tags,
			          coalesce(note, ''), position`,
			p.ID, p.Category, p.Name, p.DistanceM, p.TravelMode, p.Tags, p.Note, p.Position,
		).Scan(&out.ID, &out.Category, &out.Name, &out.DistanceM, &out.TravelMode,
			&out.Tags, &out.Note, &out.Position)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPlace
		}
		return err
	})
	if err != nil {
		if errors.Is(err, ErrNoPlace) {
			return domain.Place{}, err
		}
		if isCheck(err) {
			return domain.Place{}, ErrNotAllowed
		}
		if isUnique(err, "property_places_once") {
			return domain.Place{}, ErrPlaceExists
		}
		return domain.Place{}, fmt.Errorf("property: correcting place %s: %w", p.ID, err)
	}
	return out, nil
}

// RetirePlace stops a place being listed. Nothing here is deleted: that the
// school was once advertised is part of what was said to a renter.
func (s *Properties) RetirePlace(ctx context.Context, id string) error {
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE property_places SET state = 'retired', updated_at = now()
			 WHERE id = $1::uuid AND state = 'active'`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoPlace
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, ErrNoPlace) {
			return err
		}
		return fmt.Errorf("property: retiring place %s: %w", id, err)
	}
	return nil
}

// A nil slice reaches Postgres as NULL, and the columns are NOT NULL.
func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
