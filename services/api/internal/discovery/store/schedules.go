package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrNoSchedule is no such recurring viewing time.
var ErrNoSchedule = errors.New("inspection: no such schedule")

// ScheduleDraft is a recurring viewing time as the manager states it (#330).
type ScheduleDraft struct {
	Weekdays     []int // 0 = Sunday
	StartTime    string
	Zone         string
	DurationMins int
	Capacity     int
	AssignedTo   string
	MeetingPoint string
	StartsOn     time.Time
	EndsOn       *time.Time
}

// Pattern is the recurrence rule the draft describes.
func (d ScheduleDraft) Pattern() (domain.Schedule, error) {
	zone, err := time.LoadLocation(d.Zone)
	if err != nil {
		return domain.Schedule{}, fmt.Errorf("%w: %q is not a time zone", domain.ErrScheduleUnsound, d.Zone)
	}
	return domain.Schedule{
		Weekdays: weekdays(d.Weekdays), StartTime: d.StartTime, DurationMins: d.DurationMins,
		Capacity: d.Capacity, Zone: zone, StartsOn: d.StartsOn, EndsOn: d.EndsOn,
	}, nil
}

// Schedule is a stored series.
type Schedule struct {
	ID        string
	ListingID string
	State     string
	ScheduleDraft
}

// CreateSchedule records a recurring viewing time. It publishes nothing on its
// own: Materialise turns the pattern into the slots a prospect can book.
func (s *Inspections) CreateSchedule(ctx context.Context, listingID string, d ScheduleDraft) (string, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return "", tenancy.ErrNoTenant
	}
	if d.DurationMins == 0 {
		d.DurationMins = domain.DefaultSlotMins
	}
	if d.Capacity == 0 {
		d.Capacity = domain.DefaultSlotCapacity
	}
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var state string
		if err := tx.QueryRow(ctx, `SELECT state FROM listings WHERE id = $1`, listingID).Scan(&state); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoListing
			}
			return err
		}
		// A draft may hold viewing times before it is advertised; a let or
		// withdrawn one is finished, and times on it would be a phantom.
		if state == string(domain.StateLet) || state == string(domain.StateWithdrawn) ||
			state == string(domain.StateSuspended) {
			return ErrListingNotLive
		}
		return tx.QueryRow(ctx, `
			INSERT INTO inspection_schedules (tenant_id, listing_id, weekdays, start_time, zone,
			                                  duration_mins, capacity, assigned_to, meeting_point,
			                                  starts_on, ends_on)
			VALUES ($1, $2, $3::int[]::smallint[], $4::time, $5, $6, $7, $8, $9, $10::date, $11::date)
			RETURNING id`,
			tenant.String(), listingID, d.Weekdays, d.StartTime, d.Zone, d.DurationMins, d.Capacity,
			nullText(d.AssignedTo), nullText(d.MeetingPoint), d.StartsOn, d.EndsOn).Scan(&id)
	})
	return id, err
}

// Schedules are a listing's series, newest last.
func (s *Inspections) Schedules(ctx context.Context, listingID string) ([]Schedule, error) {
	var out []Schedule
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, selectSchedule+`
			 WHERE listing_id = $1 ORDER BY starts_on, created_at`, listingID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			sc, err := scanSchedule(rows)
			if err != nil {
				return err
			}
			out = append(out, sc)
		}
		return rows.Err()
	})
	return out, err
}

// Materialise turns the series into bookable slots up to the horizon, and says
// how many it added. Running it again adds nothing: the occurrence key is the
// series and the instant it fell on, so a slot the manager has since moved or
// cancelled is not put back.
func (s *Inspections) Materialise(ctx context.Context, scheduleID string, until time.Time) (int, error) {
	var added int
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, selectSchedule+` WHERE id = $1`, scheduleID)
		if err != nil {
			return err
		}
		sc, err := oneSchedule(rows)
		if err != nil {
			return err
		}
		if sc.State != "active" {
			return nil
		}
		pattern, err := sc.Pattern()
		if err != nil {
			return err
		}
		now := time.Now()
		for _, at := range pattern.Occurrences(now, until) {
			if !at.After(now) {
				continue
			}
			tag, err := tx.Exec(ctx, `
				INSERT INTO inspection_slots (tenant_id, listing_id, starts_at, duration_mins,
				                              capacity, assigned_to, meeting_point,
				                              schedule_id, series_at)
				SELECT tenant_id, listing_id, $2, duration_mins, capacity, assigned_to,
				       meeting_point, id, $2
				  FROM inspection_schedules WHERE id = $1
				ON CONFLICT DO NOTHING`, scheduleID, at)
			if err != nil {
				return err
			}
			added += int(tag.RowsAffected())
		}
		return nil
	})
	return added, err
}

// CancelOccurrence drops one viewing from the series without touching the rest.
func (s *Inspections) CancelOccurrence(ctx context.Context, slotID string) error {
	return s.overrideOccurrence(ctx, `
		UPDATE inspection_slots SET state = 'cancelled', overridden = true, updated_at = now()
		 WHERE id = $1 AND state <> 'cancelled'`, slotID)
}

// MoveOccurrence shifts one viewing. It keeps series_at, so the series still
// knows that date is spoken for and does not re-create the original time.
func (s *Inspections) MoveOccurrence(ctx context.Context, slotID string, to time.Time) error {
	return s.overrideOccurrence(ctx, `
		UPDATE inspection_slots SET starts_at = $2, overridden = true, updated_at = now()
		 WHERE id = $1 AND state = 'open'`, slotID, to)
}

func (s *Inspections) overrideOccurrence(ctx context.Context, sql, slotID string, args ...any) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, sql, append([]any{slotID}, args...)...)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSlot
		}
		return nil
	})
}

// EndSeries stops a series from a date forward. Future slots nobody booked are
// closed; a confirmed viewing stands, because an administrative change is not a
// reason to turn a prospect away at the door — that takes a cancellation, with
// notice, one by one.
func (s *Inspections) EndSeries(ctx context.Context, scheduleID string, from time.Time) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE inspection_schedules
			   SET ends_on = GREATEST(starts_on, ($2::timestamptz - interval '1 day')::date),
			       state   = CASE WHEN $2::timestamptz::date <= starts_on THEN 'ended' ELSE state END,
			       updated_at = now()
			 WHERE id = $1`, scheduleID, from)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoSchedule
		}
		_, err = tx.Exec(ctx, `
			UPDATE inspection_slots SET state = 'closed', updated_at = now()
			 WHERE schedule_id = $1 AND state = 'open' AND booked = 0 AND starts_at >= $2`,
			scheduleID, from)
		return err
	})
}

// AmendSchedule is the calendar's "this one and all after it": the old series
// ends the day before, the new one starts on the date, and every slot before it
// — booked or not — is left exactly as it was advertised.
func (s *Inspections) AmendSchedule(ctx context.Context, scheduleID string, d ScheduleDraft,
	from time.Time) (string, error) {
	var listingID string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`SELECT listing_id::text FROM inspection_schedules WHERE id = $1`, scheduleID).Scan(&listingID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoSchedule
		}
		return err
	})
	if err != nil {
		return "", err
	}
	if err := s.EndSeries(ctx, scheduleID, from); err != nil {
		return "", err
	}
	zone, err := time.LoadLocation(d.Zone)
	if err != nil {
		return "", err
	}
	local := from.In(zone)
	d.StartsOn = domain.Day(local.Year(), local.Month(), local.Day())
	return s.CreateSchedule(ctx, listingID, d)
}

const selectSchedule = `
	SELECT id::text, listing_id::text, weekdays::int[], to_char(start_time, 'HH24:MI'), zone,
	       duration_mins, capacity, coalesce(assigned_to, ''), coalesce(meeting_point, ''),
	       starts_on, ends_on, state
	  FROM inspection_schedules`

func scanSchedule(rows pgx.Rows) (Schedule, error) {
	var sc Schedule
	err := rows.Scan(&sc.ID, &sc.ListingID, &sc.Weekdays, &sc.StartTime, &sc.Zone,
		&sc.DurationMins, &sc.Capacity, &sc.AssignedTo, &sc.MeetingPoint,
		&sc.StartsOn, &sc.EndsOn, &sc.State)
	return sc, err
}

func oneSchedule(rows pgx.Rows) (Schedule, error) {
	defer rows.Close()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return Schedule{}, err
		}
		return Schedule{}, ErrNoSchedule
	}
	return scanSchedule(rows)
}

func weekdays(days []int) []time.Weekday {
	out := make([]time.Weekday, len(days))
	for i, d := range days {
		out[i] = time.Weekday(d)
	}
	return out
}
