package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Due is the next rent a live tenancy owes, and where it is owed from.
type Due struct {
	LeaseID      string
	PropertyID   string
	UnitID       string
	PropertyName string
	Locality     string
	UnitCode     string
	DueOn        effective.Date
	AmountMinor  int64
}

// Upcoming is the next rent falling due on every live tenancy, between two
// dates, soonest first.
//
// One query over the book rather than a schedule computed per lease: the
// reminder list is read on every visit to the manager's home screen, and
// walking five hundred tenancies for their terms and parties was two thousand
// round trips to say what one join already knows (#337, and #306's argument
// about pages of a roster).
//
// The rent is the one in force on the day it falls due, not today's: a
// revision effective next month is what next month's reminder is for.
func (s *Leases) Upcoming(ctx context.Context, from, through effective.Date, limit int) ([]Due, error) {
	if limit <= 0 {
		limit = 500
	}
	var out []Due
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			WITH months AS (
				SELECT generate_series(date_trunc('month', $1::date),
				                       date_trunc('month', $2::date),
				                       interval '1 month')::date AS m
			),
			falling_due AS (
				SELECT l.id, l.property_id, l.unit_id, l.valid_from AS term_from,
				       l.valid_to AS term_to, r.amount_minor, r.validity AS rent_validity,
				       -- A due day of 31 means the last day, whatever that month's
				       -- length is; the schema says so and the arithmetic is here.
				       (months.m + (least(r.due_day,
				            extract(day FROM (months.m + interval '1 month - 1 day'))::int) - 1)
				            * interval '1 day')::date AS due_on
				  FROM leases l
				  JOIN rent_schedule r
				    ON r.lease_id = l.id AND r.tenant_id = l.tenant_id AND r.retired_at IS NULL
				  CROSS JOIN months
				 WHERE l.state IN ('active', 'in_notice')
			)
			SELECT next.id::text, next.property_id::text, next.unit_id::text,
			       p.name, p.locality, u.code::text, next.due_on, next.amount_minor
			  FROM (
				SELECT DISTINCT ON (d.id) d.*
				  FROM falling_due d
				 WHERE d.due_on BETWEEN $1::date AND $2::date
				   AND d.rent_validity @> d.due_on
				   AND d.due_on >= d.term_from
				   AND (d.term_to IS NULL OR d.due_on <= d.term_to)
				 ORDER BY d.id, d.due_on
			  ) next
			  JOIN properties p ON p.id = next.property_id
			  JOIN units u      ON u.id = next.unit_id
			 ORDER BY next.due_on, next.id
			 LIMIT $3`, from.Time(), through.Time(), limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				d  Due
				on time.Time
			)
			if err := rows.Scan(&d.LeaseID, &d.PropertyID, &d.UnitID,
				&d.PropertyName, &d.Locality, &d.UnitCode, &on, &d.AmountMinor); err != nil {
				return err
			}
			d.DueOn = effective.DateOf(on, on.Location())
			out = append(out, d)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("lease: reading what falls due: %w", err)
	}
	return out, nil
}
