package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The tenancy as the person living in it sees it. Issue #51.
//
// One query rather than four, because the reason it exists is a reminder link
// opened on a phone with two bars of signal: every round trip is a second of
// blank screen, and the four tables involved are all joined by the lease id
// anyway.
//
// Nothing here filters by party. The session does that — ADR-0029 sets the
// renter alongside the organisation, and PostgreSQL refuses a lease that is not
// theirs. A WHERE clause here would be a second, weaker copy of a rule that is
// already enforced, and the one that would be forgotten.

// Tenancy is the lease summary a renter is shown: where they live, on what
// terms, and until when.
type Tenancy struct {
	LeaseID string
	State   domain.State

	// PropertyID and UnitID place the money. Carried because a collection has to
	// name where it happened, and the tenant's request cannot be trusted to say.
	PropertyID   string
	UnitID       string
	PropertyName string
	Locality     string
	City         string
	UnitCode     string

	// Term is what was agreed. EndedOn is when occupancy actually ceased, and is
	// zero while the tenancy runs — the pair ADR-0010 keeps distinct.
	Term    effective.Interval
	EndedOn effective.Date

	NoticeDays  int
	LockInUntil effective.Date

	// RentMinor and DueDay are the schedule in force on the day asked about, not
	// the one the lease opened with: a rent revised in April is what the tenant
	// owes in May, and showing them the opening figure would be showing them a
	// number nobody is going to invoice.
	RentMinor int64
	DueDay    int
}

// Live reports whether this is somewhere they still live, as opposed to history
// they can still read.
func (t Tenancy) Live() bool {
	return t.State == domain.StateActive || t.State == domain.StateInNotice
}

// Tenancy reads one lease's summary, as of a date.
//
// ErrNoLease covers both "no such lease" and "not yours", and deliberately: they
// are the same answer to somebody changing the identifier in the URL, and
// distinguishing them would confirm that a lease exists.
func (s *Leases) Tenancy(ctx context.Context, leaseID string, on effective.Date) (Tenancy, error) {
	var (
		t        Tenancy
		state    string
		from     time.Time
		to       *time.Time
		ended    *time.Time
		lockIn   *time.Time
		rent     *int64
		dueDay   *int
		tenantID = ""
	)
	if id, ok := tenancy.From(ctx); ok {
		tenantID = id.String()
	}

	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT l.id::text, l.state, l.valid_from, l.valid_to, l.ended_on,
			       l.notice_days, l.lock_in_until,
			       l.property_id::text, l.unit_id::text,
			       p.name, p.locality, p.city, u.code::text,
			       r.amount_minor, r.due_day
			  FROM leases l
			  JOIN properties p ON p.id = l.property_id AND p.tenant_id = l.tenant_id
			  JOIN units u      ON u.id = l.unit_id     AND u.tenant_id = l.tenant_id
			  -- The schedule in force on the day, and at most one of them: the
			  -- exclusion constraint on rent_schedule is what makes the LEFT JOIN
			  -- safe without a LIMIT that would hide a second live row.
			  LEFT JOIN rent_schedule r
			         ON r.lease_id = l.id AND r.tenant_id = l.tenant_id
			        AND r.retired_at IS NULL AND r.validity @> $2::date
			 WHERE l.id = $1::uuid`, leaseID, on.Time(),
		).Scan(&t.LeaseID, &state, &from, &to, &ended, &t.NoticeDays, &lockIn,
			&t.PropertyID, &t.UnitID,
			&t.PropertyName, &t.Locality, &t.City, &t.UnitCode, &rent, &dueDay)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Tenancy{}, ErrNoLease
	case err != nil:
		return Tenancy{}, fmt.Errorf("lease: reading the tenancy for %s in %s: %w", leaseID, tenantID, err)
	}

	t.State = domain.State(state)
	start := effective.DateOf(from, time.UTC)
	if to != nil {
		if t.Term, err = effective.Between(start, effective.DateOf(*to, time.UTC)); err != nil {
			return Tenancy{}, err
		}
	} else if t.Term, err = effective.Since(start); err != nil {
		return Tenancy{}, err
	}
	if ended != nil {
		t.EndedOn = effective.DateOf(*ended, time.UTC)
	}
	if lockIn != nil {
		t.LockInUntil = effective.DateOf(*lockIn, time.UTC)
	}
	if rent != nil {
		t.RentMinor, t.DueDay = *rent, *dueDay
	}
	return t, nil
}

// ActiveOnProperty reads the current tenancy on a property, for the owner's
// side of the same question Tenancy answers for the renter's: "who lives here
// and until when". A vacant property has none, which is not an error — an
// owner's home screen shows a vacancy card for that, not a failure.
//
// Ordered by valid_from DESC and capped at one: a property can carry more than
// one active lease across its units (a building with several flats), and this
// answers for the property as a whole the way the mobile card does — one
// headline tenancy — not a list. A caller needing every unit's tenancy reads
// units individually instead.
func (s *Leases) ActiveOnProperty(ctx context.Context, propertyID string, on effective.Date) (Tenancy, bool, error) {
	var (
		t      Tenancy
		state  string
		from   time.Time
		to     *time.Time
		ended  *time.Time
		lockIn *time.Time
		rent   *int64
		dueDay *int
	)
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT l.id::text, l.state, l.valid_from, l.valid_to, l.ended_on,
			       l.notice_days, l.lock_in_until,
			       l.property_id::text, l.unit_id::text,
			       p.name, p.locality, p.city, u.code::text,
			       r.amount_minor, r.due_day
			  FROM leases l
			  JOIN properties p ON p.id = l.property_id AND p.tenant_id = l.tenant_id
			  JOIN units u      ON u.id = l.unit_id     AND u.tenant_id = l.tenant_id
			  LEFT JOIN rent_schedule r
			         ON r.lease_id = l.id AND r.tenant_id = l.tenant_id
			        AND r.retired_at IS NULL AND r.validity @> $2::date
			 WHERE l.property_id = $1::uuid AND l.state IN ('active', 'in_notice')
			 ORDER BY l.valid_from DESC
			 LIMIT 1`, propertyID, on.Time(),
		).Scan(&t.LeaseID, &state, &from, &to, &ended, &t.NoticeDays, &lockIn,
			&t.PropertyID, &t.UnitID,
			&t.PropertyName, &t.Locality, &t.City, &t.UnitCode, &rent, &dueDay)
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return Tenancy{}, false, nil
	case err != nil:
		return Tenancy{}, false, fmt.Errorf("lease: reading the active tenancy on property %s: %w", propertyID, err)
	}

	t.State = domain.State(state)
	start := effective.DateOf(from, time.UTC)
	if to != nil {
		if t.Term, err = effective.Between(start, effective.DateOf(*to, time.UTC)); err != nil {
			return Tenancy{}, false, err
		}
	} else if t.Term, err = effective.Since(start); err != nil {
		return Tenancy{}, false, err
	}
	if ended != nil {
		t.EndedOn = effective.DateOf(*ended, time.UTC)
	}
	if lockIn != nil {
		t.LockInUntil = effective.DateOf(*lockIn, time.UTC)
	}
	if rent != nil {
		t.RentMinor, t.DueDay = *rent, *dueDay
	}
	return t, true, nil
}
