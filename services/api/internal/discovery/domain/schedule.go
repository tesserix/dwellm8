package domain

import (
	"errors"
	"fmt"
	"time"
)

// Default shape of a viewing, when the manager states only a day and a time.
const (
	DefaultSlotMins     = 30
	DefaultSlotCapacity = 4
	// MaterialiseWeeks is how far ahead an open-ended series is turned into
	// bookable slots. A rolling horizon, extended by the job that re-runs it.
	MaterialiseWeeks = 8
)

// ErrScheduleUnsound rejects a series before anything is written from it.
var ErrScheduleUnsound = errors.New("schedule")

// Schedule is a recurring viewing time on a listing (#330): a weekly pattern in
// the property's own zone, not a list of instants. "Saturdays at ten" has to
// survive the clocks changing, and an instant plus seven days does not.
type Schedule struct {
	Weekdays     []time.Weekday
	StartTime    string // local wall clock, HH:MM
	DurationMins int
	Capacity     int
	Zone         *time.Location
	StartsOn     time.Time
	EndsOn       *time.Time
}

// Day is a calendar date, which is what a series runs between.
func Day(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// Validate refuses a series that could not produce a sensible viewing.
func (s Schedule) Validate() error {
	if len(s.Weekdays) == 0 {
		return fmt.Errorf("%w: name at least one day it repeats on", ErrScheduleUnsound)
	}
	if _, _, err := s.clock(); err != nil {
		return err
	}
	if s.Zone == nil {
		return fmt.Errorf("%w: the property's time zone is required", ErrScheduleUnsound)
	}
	if s.EndsOn != nil && s.EndsOn.Before(s.StartsOn) {
		return fmt.Errorf("%w: it ends before it starts", ErrScheduleUnsound)
	}
	if s.DurationMins != 0 && (s.DurationMins < 10 || s.DurationMins > 240) {
		return fmt.Errorf("%w: duration must be between 10 and 240 minutes", ErrScheduleUnsound)
	}
	if s.Capacity != 0 && (s.Capacity < 1 || s.Capacity > 20) {
		return fmt.Errorf("%w: capacity must be between 1 and 20 people", ErrScheduleUnsound)
	}
	return nil
}

// Occurrences are the instants the series falls on within the window, in order.
// The window and the series both bound the answer; neither alone does.
func (s Schedule) Occurrences(from, to time.Time) []time.Time {
	hour, min, err := s.clock()
	if err != nil || s.Zone == nil || len(s.Weekdays) == 0 {
		return nil
	}
	repeats := map[time.Weekday]bool{}
	for _, d := range s.Weekdays {
		repeats[d] = true
	}

	start := latest(dateOf(from), dateOf(s.StartsOn))
	end := dateOf(to)
	if s.EndsOn != nil {
		end = earliest(end, dateOf(*s.EndsOn))
	}

	var out []time.Time
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		if repeats[d.Weekday()] {
			out = append(out, time.Date(d.Year(), d.Month(), d.Day(), hour, min, 0, 0, s.Zone))
		}
	}
	return out
}

func (s Schedule) clock() (hour, min int, err error) {
	t, perr := time.Parse("15:04", s.StartTime)
	if perr != nil {
		return 0, 0, fmt.Errorf("%w: the start time must read as HH:MM", ErrScheduleUnsound)
	}
	return t.Hour(), t.Minute(), nil
}

func dateOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func latest(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func earliest(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
