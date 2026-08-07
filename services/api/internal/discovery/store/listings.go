// Package store writes and reads the discovery module's tables: listings,
// prospects, enquiries and contact bridges (ADR-0019).
//
// Two pools, deliberately. Listings belong to an organisation and use the
// request pool under ordinary tenancy. Prospects belong to nobody — a browsing
// stranger — so their store takes the platform pool, and the policies on those
// tables admit only a platform session.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoListing is no such listing — including one hidden by policy, which is
// the same answer to a caller and deliberately so.
var ErrNoListing = errors.New("listing: no such listing")

// ErrAlreadyAdvertised is a second live or paused listing for a unit the
// database refused: two adverts for one flat at different rents is the thing a
// prospect screenshots.
var ErrAlreadyAdvertised = errors.New("listing: the unit already has a live or paused listing")

// Listings is the listing store.
type Listings struct{ pool tenancy.Pool }

// NewListings takes the request pool: a listing is always somebody's.
func NewListings(p tenancy.Pool) *Listings { return &Listings{pool: p} }

// Listing is a row as the owner sees it.
type Listing struct {
	ID           string
	TenantID     string
	PropertyID   string
	UnitID       string
	UnitParentID string
	State        domain.State
	PublishedAt  *time.Time

	Headline  string
	Locality  string
	City      string
	StateCode string

	Costs          domain.Costs
	Bedrooms       int
	CarpetAreaSqft float64
	AvailableFrom  effective.Date
	CreatedAt      time.Time

	ListerKind        string
	AgentRegistration string
}

// Create writes a draft. Nothing is advertised yet, so no event.
func (s *Listings) Create(ctx context.Context, d domain.Draft) (string, error) {
	if err := d.Validate(); err != nil {
		return "", err
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}

	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO listings (tenant_id, property_id, unit_id, unit_parent_id, state,
			                      headline, locality, city, state_code,
			                      rent_minor, deposit_minor, maintenance_minor, parking_minor,
			                      other_monthly_minor, one_time_minor, costs_confirmed,
			                      bedrooms, carpet_area_sqft, available_from,
			                      lister_kind, agent_registration)
			VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7, $8,
			        $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			        coalesce(nullif($19, ''), 'owner'), $20)
			RETURNING id`,
			tenant.String(), d.PropertyID, d.UnitID, nullText(d.UnitParentID),
			d.Headline, d.Locality, d.City, d.StateCode,
			d.Costs.RentMinor, d.Costs.DepositMinor, d.Costs.MaintenanceMinor,
			d.Costs.ParkingMinor, d.Costs.OtherMonthlyMinor, d.Costs.OneTimeMinor,
			d.Costs.Confirmed,
			nullInt(d.Bedrooms), nullFloat(d.CarpetAreaSqft), nullDate(d.AvailableFrom),
			d.ListerKind, nullText(d.AgentRegistration),
		).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("writing the listing: %w", err)
	}
	return id, nil
}

// Get reads one listing under ordinary tenancy — the owner's view, any state.
func (s *Listings) Get(ctx context.Context, id string) (Listing, error) {
	var l Listing
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return scanListing(tx.QueryRow(ctx, selectListing+` WHERE id = $1`, id), &l)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Listing{}, ErrNoListing
	}
	return l, err
}

// ForUnit reads the advert on a unit — the live or paused one, and failing
// that the most recent of whatever else the unit has had. A withdrawn advert
// still carries the bedroom count and the asking rent, which is more than the
// register holds about the flat (#338).
func (s *Listings) ForUnit(ctx context.Context, unitID string) (Listing, error) {
	var l Listing
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return scanListing(tx.QueryRow(ctx, selectListing+`
			 WHERE unit_id = $1::uuid
			 ORDER BY (state IN ('live', 'paused')) DESC, created_at DESC
			 LIMIT 1`, unitID), &l)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Listing{}, ErrNoListing
	}
	return l, err
}

// List reads the organisation's listings, newest first.
func (s *Listings) List(ctx context.Context, state string) ([]Listing, error) {
	var out []Listing
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		q := selectListing + ` WHERE tenant_id = current_tenant_id()`
		args := []any{}
		if state != "" {
			q += ` AND state = $1`
			args = append(args, state)
		}
		q += ` ORDER BY created_at DESC LIMIT 500`
		rows, err := tx.Query(ctx, q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var l Listing
			if err := scanListing(rows, &l); err != nil {
				return err
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	return out, err
}

// Move performs a lifecycle transition and publishes its event in the same
// transaction. The disclosure gate and the occupancy guard have already run in
// the service; what fires here is what the database owns — one live advert per
// unit, and a live listing being a published one.
func (s *Listings) Move(ctx context.Context, id string, to domain.State, actor events.Actor) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}

	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx,
			`SELECT state FROM listings WHERE id = $1 FOR UPDATE`, id).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoListing
		}
		if err != nil {
			return err
		}
		ev, err := domain.Transition(domain.State(from), to)
		if err != nil {
			return err
		}

		// published_at is set on first publication and kept thereafter: pausing
		// keeps the history, and that asymmetry is the two-column design.
		if _, err := tx.Exec(ctx, `
			UPDATE listings
			   SET state = $2,
			       published_at = CASE WHEN $2 = 'live' THEN coalesce(published_at, now())
			                           ELSE published_at END,
			       updated_at = now()
			 WHERE id = $1`, id, string(to)); err != nil {
			return err
		}

		env, err := events.New(string(ev), tenant.String(),
			events.Subject{Kind: "listing", ID: id}, actor, listingEventData(ctx, tx, id))
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})

	if isUniqueViolation(err, "listings_one_live_per_unit") {
		return ErrAlreadyAdvertised
	}
	return err
}

// LetMarker closes listings from the lease consumer. Its own type because it
// holds the platform pool: the consumer serves no request, so there is no
// tenant context — the event names the organisation instead.
type LetMarker struct{ pool tenancy.PlatformPool }

// NewLetMarker takes the platform pool, marked as such at the type level.
func NewLetMarker(p tenancy.PlatformPool) *LetMarker { return &LetMarker{pool: p} }

// MarkLetByUnit closes every live or paused listing for a unit: the moment the
// unit is let the advertisement dies, with no manual step (#135).
func (s *LetMarker) MarkLetByUnit(ctx context.Context, tenant, unitID string) (int, error) {
	var closed []string
	err := tenancy.Platform(ctx, s.pool, "closing listings for a let unit (ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				UPDATE listings SET state = 'let', updated_at = now()
				 WHERE tenant_id = $1 AND unit_id = $2 AND state IN ('live', 'paused')
				 RETURNING id`, tenant, unitID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return err
				}
				closed = append(closed, id)
			}
			if err := rows.Err(); err != nil {
				return err
			}

			for _, id := range closed {
				env, err := events.New(string(domain.EventLet), tenant,
					events.Subject{Kind: "listing", ID: id},
					events.Actor{Kind: events.ActorSystem},
					struct {
						UnitID string `json:"unit_id"`
					}{unitID})
				if err != nil {
					return err
				}
				if err := events.Append(ctx, tx, env); err != nil {
					return err
				}
				if err := closeTheViewings(ctx, tx, tenant, id); err != nil {
					return err
				}
			}
			return nil
		})
	return len(closed), err
}

// closeTheViewings ends what the advertisement was carrying (#332): open times
// stop taking bookings, and every live enquiry on the listing is closed with the
// reason. The cancellation fact per booked prospect is the part that matters —
// somebody expecting to view a flat that is taken must be told, not turned away
// at the door — and it goes to the outbox on this transaction, never inline.
func closeTheViewings(ctx context.Context, tx pgx.Tx, tenant, listingID string) error {
	if _, err := tx.Exec(ctx, `
		UPDATE inspection_slots SET state = 'closed', updated_at = now()
		 WHERE listing_id = $1 AND state = 'open'`, listingID); err != nil {
		return err
	}

	rows, err := tx.Query(ctx, `
		UPDATE enquiries
		   SET state = 'closed', outcome = 'listing_let', outcome_at = now(), updated_at = now()
		 WHERE listing_id = $1 AND state IN ('new', 'owner_responded', 'scheduled')
		 RETURNING id::text, prospect_id::text, coalesce(slot_id::text, ''), scheduled_for`,
		listingID)
	if err != nil {
		return err
	}
	type told struct {
		enquiry, prospect, slot string
		at                      *time.Time
	}
	var telling []told
	for rows.Next() {
		var t told
		if err := rows.Scan(&t.enquiry, &t.prospect, &t.slot, &t.at); err != nil {
			rows.Close()
			return err
		}
		telling = append(telling, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, t := range telling {
		if t.slot == "" {
			continue
		}
		env, err := events.New(string(domain.EventInspectionCancelled), tenant,
			events.Subject{Kind: "enquiry", ID: t.enquiry},
			events.Actor{Kind: events.ActorSystem},
			struct {
				By         string     `json:"by"`
				ListingID  string     `json:"listing_id"`
				ProspectID string     `json:"prospect_id"`
				SlotID     string     `json:"slot_id"`
				StartsAt   *time.Time `json:"starts_at,omitempty"`
			}{"listing_let", listingID, t.prospect, t.slot, t.at})
		if err != nil {
			return err
		}
		if err := events.Append(ctx, tx, env); err != nil {
			return err
		}
	}
	return nil
}

const selectListing = `
	SELECT id::text, tenant_id::text, property_id::text, unit_id::text,
	       coalesce(unit_parent_id::text, ''), state, published_at,
	       headline, locality, city, state_code,
	       rent_minor, deposit_minor, maintenance_minor, parking_minor,
	       other_monthly_minor, one_time_minor, costs_confirmed,
	       coalesce(bedrooms, 0), coalesce(carpet_area_sqft, 0), available_from, created_at,
	       lister_kind, coalesce(agent_registration, '')
	  FROM listings`

type scannable interface{ Scan(dest ...any) error }

func scanListing(row scannable, l *Listing) error {
	var (
		state     string
		available *time.Time
	)
	if err := row.Scan(&l.ID, &l.TenantID, &l.PropertyID, &l.UnitID, &l.UnitParentID,
		&state, &l.PublishedAt, &l.Headline, &l.Locality, &l.City, &l.StateCode,
		&l.Costs.RentMinor, &l.Costs.DepositMinor, &l.Costs.MaintenanceMinor,
		&l.Costs.ParkingMinor, &l.Costs.OtherMonthlyMinor, &l.Costs.OneTimeMinor,
		&l.Costs.Confirmed, &l.Bedrooms, &l.CarpetAreaSqft, &available, &l.CreatedAt,
		&l.ListerKind, &l.AgentRegistration); err != nil {
		return err
	}
	l.State = domain.State(state)
	if available != nil {
		l.AvailableFrom = effective.Day(available.Year(), available.Month(), available.Day())
	}
	return nil
}

// listingEventData carries what a consumer needs without reading the table
// back later: the unit, for the projector and the ops feed.
func listingEventData(ctx context.Context, tx pgx.Tx, id string) any {
	var property, unit string
	_ = tx.QueryRow(ctx,
		`SELECT property_id::text, unit_id::text FROM listings WHERE id = $1`, id).
		Scan(&property, &unit)
	return struct {
		PropertyID string `json:"property_id"`
		UnitID     string `json:"unit_id"`
	}{property, unit}
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(i int) any {
	if i == 0 {
		return nil
	}
	return i
}

func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func nullDate(d effective.Date) any {
	if d.Zero() {
		return nil
	}
	return d.Time()
}

func isUniqueViolation(err error, constraint string) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	return pg.Code == "23505" && pg.ConstraintName == constraint
}
