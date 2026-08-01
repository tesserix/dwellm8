package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Public is the anonymous read path: the one store in this codebase that runs
// unscoped, on the request pool, against the one public branch in the schema.
// Row-level security is the filter — the SQL here repeats the live-and-
// published condition anyway, so a policy regression fails towards an empty
// page rather than a disclosure.
type Public struct{ pool tenancy.Pool }

// NewPublic takes the request pool. Not the platform pool, ever: a platform
// session sees drafts, and this store serves strangers.
func NewPublic(p tenancy.Pool) *Public { return &Public{pool: p} }

// Query is what a prospect may filter by (#133).
type Query struct {
	City         string
	Locality     string
	MaxRentMinor int64
	MinBedrooms  int
	// AvailableBy keeps out listings whose unit frees up too late.
	AvailableBy time.Time
	// After is the created_at cursor for stable pagination, zero for page one.
	After time.Time
	Limit int
}

// Card is a search result: what a stranger may see, which is the listing's own
// columns and nothing reachable through a join — those all deny.
type Card struct {
	ID                string
	Headline          string
	Locality          string
	City              string
	StateCode         string
	Bedrooms          int
	CarpetAreaSqft    float64
	AvailableFrom     *time.Time
	Costs             domain.Costs
	TotalMonthlyMinor int64
	TotalOneTimeMinor int64
	Currency          string
	PublishedAt       time.Time
	CreatedAt         time.Time
}

// Search lists live, published listings matching the query, newest first.
func (s *Public) Search(ctx context.Context, q Query) ([]Card, error) {
	if q.Limit <= 0 || q.Limit > 50 {
		q.Limit = 20
	}

	where := []string{`state = 'live'`, `published_at IS NOT NULL`}
	args := []any{}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if q.City != "" {
		where = append(where, `lower(city) = lower(`+arg(q.City)+`)`)
	}
	if q.Locality != "" {
		where = append(where, `lower(locality) = lower(`+arg(q.Locality)+`)`)
	}
	if q.MaxRentMinor > 0 {
		where = append(where, `rent_minor <= `+arg(q.MaxRentMinor))
	}
	if q.MinBedrooms > 0 {
		where = append(where, `bedrooms >= `+arg(q.MinBedrooms))
	}
	if !q.AvailableBy.IsZero() {
		where = append(where, `(available_from IS NULL OR available_from <= `+arg(q.AvailableBy)+`)`)
	}
	if !q.After.IsZero() {
		where = append(where, `created_at < `+arg(q.After))
	}

	sql := selectCard + ` WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY created_at DESC LIMIT ` + arg(q.Limit)

	var out []Card
	err := tenancy.Public(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, sql, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Card
			if err := scanCard(rows, &c); err != nil {
				return err
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	return out, err
}

// Detail reads one public listing (#134). A listing that is no longer live is
// ErrNoListing, and the handler says so plainly rather than serving a stale
// advert.
func (s *Public) Detail(ctx context.Context, id string) (Card, error) {
	var c Card
	err := tenancy.Public(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return scanCard(tx.QueryRow(ctx, selectCard+
			` WHERE id = $1 AND state = 'live' AND published_at IS NOT NULL`, id), &c)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Card{}, ErrNoListing
	}
	return c, err
}

const selectCard = `
	SELECT id::text, headline, locality, city, state_code,
	       coalesce(bedrooms, 0), coalesce(carpet_area_sqft, 0), available_from,
	       rent_minor, deposit_minor, maintenance_minor, parking_minor,
	       other_monthly_minor, one_time_minor, currency, published_at, created_at
	  FROM listings`

func scanCard(row scannable, c *Card) error {
	if err := row.Scan(&c.ID, &c.Headline, &c.Locality, &c.City, &c.StateCode,
		&c.Bedrooms, &c.CarpetAreaSqft, &c.AvailableFrom,
		&c.Costs.RentMinor, &c.Costs.DepositMinor, &c.Costs.MaintenanceMinor,
		&c.Costs.ParkingMinor, &c.Costs.OtherMonthlyMinor, &c.Costs.OneTimeMinor,
		&c.Currency, &c.PublishedAt, &c.CreatedAt); err != nil {
		return err
	}
	c.Costs.Confirmed = true // a live listing disclosed, by constraint
	c.TotalMonthlyMinor = c.Costs.TotalMonthlyMinor()
	c.TotalOneTimeMinor = c.Costs.TotalOneTimeMinor()
	return nil
}
