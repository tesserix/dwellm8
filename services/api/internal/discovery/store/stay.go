package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// HomeStay (#233): a unit let by the night. Same funnel identity as the rest
// of discovery — the guest is a verified prospect — and the double-booking
// story is one exclusion constraint, not application bookkeeping.

// ErrDatesTaken is two guests wanting the same nights; exactly one wins.
var ErrDatesTaken = errors.New("stay: those nights are no longer available")

// ErrNoStay is no such listing or booking, including one hidden by policy.
var ErrNoStay = errors.New("stay: not found")

// ErrHoldExpired is a confirmation arriving after the hold released.
var ErrHoldExpired = errors.New("stay: the hold has expired — book again")

// ErrStayRules is a request outside the listing's own rules.
var ErrStayRules = errors.New("stay: outside the listing's rules")

// holdTTL is how long a hold blocks the calendar with nobody committed.
const holdTTL = 15 * time.Minute

// Stay is the HomeStay store.
type Stay struct {
	pool     tenancy.Pool
	platform tenancy.PlatformPool
}

// NewStay builds the store.
func NewStay(p tenancy.Pool, plat tenancy.PlatformPool) *Stay {
	return &Stay{pool: p, platform: plat}
}

// StayListing is a row of stay_listings.
type StayListing struct {
	ID            string
	PropertyID    string
	UnitID        string
	State         string
	Headline      string
	Locality      string
	City          string
	StateCode     string
	NightlyMinor  int64
	CleaningMinor int64
	DepositMinor  int64
	MaxGuests     int
	MinNights     int
	MaxNights     int
}

// StayDraft is what a host publishes.
type StayDraft struct {
	PropertyID    string
	UnitID        string
	Headline      string
	Locality      string
	City          string
	StateCode     string
	NightlyMinor  int64
	CleaningMinor int64
	DepositMinor  int64
	MaxGuests     int
	MinNights     int
	MaxNights     int
}

// CreateListing writes a draft stay listing.
func (s *Stay) CreateListing(ctx context.Context, d StayDraft) (string, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}
	if d.MaxGuests == 0 {
		d.MaxGuests = 2
	}
	if d.MinNights == 0 {
		d.MinNights = 1
	}
	if d.MaxNights == 0 {
		d.MaxNights = 30
	}
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO stay_listings (tenant_id, property_id, unit_id, headline, locality,
			                           city, state_code, nightly_minor, cleaning_minor,
			                           deposit_minor, max_guests, min_nights, max_nights)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
			RETURNING id::text`,
			tenant.String(), d.PropertyID, d.UnitID, d.Headline, d.Locality, d.City,
			d.StateCode, d.NightlyMinor, d.CleaningMinor, d.DepositMinor,
			d.MaxGuests, d.MinNights, d.MaxNights).Scan(&id)
	})
	if err != nil {
		return "", fmt.Errorf("stay: creating the listing: %w", err)
	}
	return id, nil
}

// MoveListing runs the small lifecycle: draft→live, live↔paused, →withdrawn.
func (s *Stay) MoveListing(ctx context.Context, id, to string) error {
	moves := map[string]map[string]bool{
		"draft":  {"live": true, "withdrawn": true},
		"live":   {"paused": true, "withdrawn": true},
		"paused": {"live": true, "withdrawn": true},
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx, `SELECT state FROM stay_listings WHERE id = $1 FOR UPDATE`, id).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoStay
		}
		if err != nil {
			return err
		}
		if !moves[from][to] {
			return fmt.Errorf("%w: a %s stay listing cannot become %s", ErrNoStay, from, to)
		}
		if _, err := tx.Exec(ctx, `
			UPDATE stay_listings
			   SET state = $2,
			       published_at = CASE WHEN $2 = 'live' THEN coalesce(published_at, now())
			                           ELSE published_at END,
			       updated_at = now()
			 WHERE id = $1`, id, to); err != nil {
			return err
		}
		if to != "live" {
			return nil
		}
		env, err := events.New("discovery.stay.published", tenant.String(),
			events.Subject{Kind: "stay_listing", ID: id},
			events.Actor{Kind: events.ActorSystem}, struct{}{})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// HostListings lists the organisation's stay listings.
func (s *Stay) HostListings(ctx context.Context) ([]StayListing, error) {
	var out []StayListing
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, selectStayListing+`
			 WHERE tenant_id = current_tenant_id() ORDER BY created_at DESC LIMIT 200`)
		if err != nil {
			return err
		}
		return scanStayListings(rows, &out)
	})
	return out, err
}

// Search is the guest's browse: live listings in a city. Platform session,
// slots' reasoning — no public RLS branch exists for this table.
func (s *Stay) Search(ctx context.Context, city string) ([]StayListing, error) {
	var out []StayListing
	err := tenancy.Platform(ctx, s.platform, "anonymous stay search (#233, ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, selectStayListing+`
				 WHERE state = 'live' AND published_at IS NOT NULL
				   AND ($1 = '' OR lower(city) = lower($1))
				 ORDER BY nightly_minor LIMIT 50`, city)
			if err != nil {
				return err
			}
			return scanStayListings(rows, &out)
		})
	return out, err
}

// Live reads one live listing for a guest.
func (s *Stay) Live(ctx context.Context, id string) (StayListing, error) {
	var l StayListing
	err := tenancy.Platform(ctx, s.platform, "anonymous stay detail (#233, ADR-0019)",
		func(ctx context.Context, tx pgx.Tx) error {
			return scanStayListing(tx.QueryRow(ctx, selectStayListing+`
				 WHERE id = $1 AND state = 'live' AND published_at IS NOT NULL`, id), &l)
		})
	if errors.Is(err, pgx.ErrNoRows) {
		return StayListing{}, ErrNoStay
	}
	return l, err
}

// Booking is a row of stay_bookings.
type Booking struct {
	ID            string
	ListingID     string
	UnitID        string
	ProspectID    string
	State         string
	Guests        int
	CheckIn       time.Time
	CheckOut      time.Time
	NightlyMinor  int64
	CleaningMinor int64
	TotalMinor    int64
	HoldExpiresAt *time.Time
}

// Hold takes the nights for a verified prospect, priced at the listing's
// current rate, expiring in fifteen minutes unless confirmed. The exclusion
// constraint settles every race; the trigger re-checks verification.
func (s *Stay) Hold(ctx context.Context, listingID, prospectID string, checkIn, checkOut time.Time,
	guests int) (Booking, error) {
	var b Booking
	err := tenancy.Platform(ctx, s.platform, "holding a stay for a prospect (#233)",
		func(ctx context.Context, tx pgx.Tx) error {
			var l StayListing
			var tenant string
			err := tx.QueryRow(ctx, `
				SELECT tenant_id::text, unit_id::text, nightly_minor, cleaning_minor,
				       max_guests, min_nights, max_nights
				  FROM stay_listings
				 WHERE id = $1 AND state = 'live' AND published_at IS NOT NULL
				 FOR UPDATE`, listingID).
				Scan(&tenant, &l.UnitID, &l.NightlyMinor, &l.CleaningMinor,
					&l.MaxGuests, &l.MinNights, &l.MaxNights)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoStay
			}
			if err != nil {
				return err
			}

			nights := int(checkOut.Sub(checkIn).Hours() / 24)
			switch {
			case nights < l.MinNights:
				return fmt.Errorf("%w: at least %d nights", ErrStayRules, l.MinNights)
			case nights > l.MaxNights:
				return fmt.Errorf("%w: at most %d nights", ErrStayRules, l.MaxNights)
			case guests < 1 || guests > l.MaxGuests:
				return fmt.Errorf("%w: up to %d guests", ErrStayRules, l.MaxGuests)
			}
			total := int64(nights)*l.NightlyMinor + l.CleaningMinor

			err = tx.QueryRow(ctx, `
				INSERT INTO stay_bookings (tenant_id, listing_id, unit_id, prospect_id, guests,
				                           check_in, check_out, nightly_minor, cleaning_minor,
				                           total_minor, hold_expires_at)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, now() + $11::interval)
				RETURNING id::text, listing_id::text, unit_id::text, prospect_id::text, state,
				          guests, check_in, check_out, nightly_minor, cleaning_minor,
				          total_minor, hold_expires_at`,
				tenant, listingID, l.UnitID, prospectID, guests, checkIn, checkOut,
				l.NightlyMinor, l.CleaningMinor, total, holdTTL.String()).
				Scan(&b.ID, &b.ListingID, &b.UnitID, &b.ProspectID, &b.State, &b.Guests,
					&b.CheckIn, &b.CheckOut, &b.NightlyMinor, &b.CleaningMinor,
					&b.TotalMinor, &b.HoldExpiresAt)
			if err != nil {
				return err
			}
			env, err := events.New("discovery.stay.booked", tenant,
				events.Subject{Kind: "stay_booking", ID: b.ID},
				events.Actor{Kind: events.ActorSystem},
				struct {
					ListingID  string `json:"listing_id"`
					CheckIn    string `json:"check_in"`
					CheckOut   string `json:"check_out"`
					TotalMinor int64  `json:"total_minor"`
				}{listingID, checkIn.Format("2006-01-02"), checkOut.Format("2006-01-02"), total})
			if err != nil {
				return err
			}
			return events.Append(ctx, tx, env)
		})
	switch {
	case isExclusion(err, "stay_bookings_no_double_book"):
		return Booking{}, ErrDatesTaken
	case isCheckViolation(err):
		return Booking{}, ErrNotVerified
	}
	return b, err
}

// Confirm commits a held booking, host-side, refusing an expired hold.
func (s *Stay) Confirm(ctx context.Context, bookingID string) error {
	return s.hostMove(ctx, bookingID, "confirmed", "discovery.stay.confirmed",
		`state = 'held' AND hold_expires_at > now()`, ErrHoldExpired)
}

// Complete closes a confirmed stay after checkout.
func (s *Stay) Complete(ctx context.Context, bookingID string) error {
	return s.hostMove(ctx, bookingID, "completed", "discovery.stay.completed",
		`state = 'confirmed'`, ErrNoStay)
}

// hostMove is the shared host-side transition with its event.
func (s *Stay) hostMove(ctx context.Context, id, to, event, guard string, miss error) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE stay_bookings SET state = $2, updated_at = now()
			 WHERE id = $1 AND `+guard, id, to)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return miss
		}
		env, err := events.New(event, tenant.String(),
			events.Subject{Kind: "stay_booking", ID: id},
			events.Actor{Kind: events.ActorSystem}, struct{}{})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// CancelByGuest releases the nights, prospect-scoped on the platform pool.
func (s *Stay) CancelByGuest(ctx context.Context, prospectID, bookingID string) error {
	return tenancy.Platform(ctx, s.platform, "guest cancelling a stay (#233)",
		func(ctx context.Context, tx pgx.Tx) error {
			var tenant string
			err := tx.QueryRow(ctx, `
				UPDATE stay_bookings
				   SET state = 'cancelled', cancelled_by = 'guest', updated_at = now()
				 WHERE id = $1 AND prospect_id = $2 AND state IN ('held', 'confirmed')
				 RETURNING tenant_id::text`, bookingID, prospectID).Scan(&tenant)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoStay
			}
			if err != nil {
				return err
			}
			env, err := events.New("discovery.stay.cancelled", tenant,
				events.Subject{Kind: "stay_booking", ID: bookingID},
				events.Actor{Kind: events.ActorSystem},
				struct {
					By string `json:"by"`
				}{"guest"})
			if err != nil {
				return err
			}
			return events.Append(ctx, tx, env)
		})
}

// CancelByHost releases the nights from the host's side.
func (s *Stay) CancelByHost(ctx context.Context, bookingID string) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE stay_bookings
			   SET state = 'cancelled', cancelled_by = 'host', updated_at = now()
			 WHERE id = $1 AND state IN ('held', 'confirmed')`, bookingID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoStay
		}
		env, err := events.New("discovery.stay.cancelled", tenant.String(),
			events.Subject{Kind: "stay_booking", ID: bookingID},
			events.Actor{Kind: events.ActorSystem},
			struct {
				By string `json:"by"`
			}{"host"})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// HostBookings is the host's calendar, soonest first.
func (s *Stay) HostBookings(ctx context.Context) ([]Booking, error) {
	var out []Booking
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, listing_id::text, unit_id::text, prospect_id::text, state, guests,
			       check_in, check_out, nightly_minor, cleaning_minor, total_minor, hold_expires_at
			  FROM stay_bookings
			 WHERE tenant_id = current_tenant_id() AND state IN ('held', 'confirmed')
			 ORDER BY check_in LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b Booking
			if err := rows.Scan(&b.ID, &b.ListingID, &b.UnitID, &b.ProspectID, &b.State,
				&b.Guests, &b.CheckIn, &b.CheckOut, &b.NightlyMinor, &b.CleaningMinor,
				&b.TotalMinor, &b.HoldExpiresAt); err != nil {
				return err
			}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}

const selectStayListing = `
	SELECT id::text, property_id::text, unit_id::text, state, headline, locality, city,
	       state_code, nightly_minor, cleaning_minor, deposit_minor,
	       max_guests, min_nights, max_nights
	  FROM stay_listings`

func scanStayListing(row scannable, l *StayListing) error {
	return row.Scan(&l.ID, &l.PropertyID, &l.UnitID, &l.State, &l.Headline, &l.Locality,
		&l.City, &l.StateCode, &l.NightlyMinor, &l.CleaningMinor, &l.DepositMinor,
		&l.MaxGuests, &l.MinNights, &l.MaxNights)
}

func scanStayListings(rows pgx.Rows, out *[]StayListing) error {
	defer rows.Close()
	for rows.Next() {
		var l StayListing
		if err := scanStayListing(rows, &l); err != nil {
			return err
		}
		*out = append(*out, l)
	}
	return rows.Err()
}

func isExclusion(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23P01" && pg.ConstraintName == constraint
}
