package domain_test

import (
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
)

// A recurring viewing time is a pattern in the property's own zone (#330). What
// it produces is a list of instants, because a slot is booked at an instant —
// the pattern itself is never the thing a prospect holds.

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	z, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}
	return z
}

func weekly(t *testing.T, zone string, days ...time.Weekday) domain.Schedule {
	t.Helper()
	return domain.Schedule{
		Weekdays:  days,
		StartTime: "10:00",
		Zone:      mustZone(t, zone),
		StartsOn:  domain.Day(2026, time.August, 1),
	}
}

func TestOccurrencesRepeatWeeklyOnTheNamedDays(t *testing.T) {
	s := weekly(t, "Asia/Kolkata", time.Saturday)

	got := s.Occurrences(domain.Day(2026, time.August, 1), domain.Day(2026, time.August, 29))

	want := []string{
		"2026-08-01T10:00:00+05:30", "2026-08-08T10:00:00+05:30",
		"2026-08-15T10:00:00+05:30", "2026-08-22T10:00:00+05:30",
		"2026-08-29T10:00:00+05:30",
	}
	assertOccurrences(t, got, want)
}

func TestOccurrencesCoverEveryNamedDayInAWeek(t *testing.T) {
	s := weekly(t, "Asia/Kolkata", time.Saturday, time.Wednesday)

	got := s.Occurrences(domain.Day(2026, time.August, 1), domain.Day(2026, time.August, 9))

	assertOccurrences(t, got, []string{
		"2026-08-01T10:00:00+05:30", "2026-08-05T10:00:00+05:30",
		"2026-08-08T10:00:00+05:30",
	})
}

func TestOccurrencesStayInsideTheSeriesOwnDates(t *testing.T) {
	s := weekly(t, "Asia/Kolkata", time.Saturday)
	s.StartsOn = domain.Day(2026, time.August, 10)
	ends := domain.Day(2026, time.August, 20)
	s.EndsOn = &ends

	// The horizon asked for a whole month; the series answers for its own part.
	got := s.Occurrences(domain.Day(2026, time.August, 1), domain.Day(2026, time.August, 31))

	assertOccurrences(t, got, []string{"2026-08-15T10:00:00+05:30"})
}

func TestOccurrencesOfAnEndedSeriesAreNone(t *testing.T) {
	s := weekly(t, "Asia/Kolkata", time.Saturday)
	ends := domain.Day(2026, time.July, 1)
	s.EndsOn = &ends

	if got := s.Occurrences(domain.Day(2026, time.August, 1), domain.Day(2026, time.August, 31)); len(got) != 0 {
		t.Fatalf("an ended series still produced %d occurrences", len(got))
	}
}

// Saturdays at ten stays Saturdays at ten. Storing an instant and adding seven
// days would move the viewing an hour when the clocks change, which is how a
// manager ends up alone outside a building.
func TestOccurrencesKeepTheirWallClockAcrossADSTChange(t *testing.T) {
	s := weekly(t, "America/New_York", time.Sunday)
	s.StartsOn = domain.Day(2026, time.October, 25)

	got := s.Occurrences(domain.Day(2026, time.October, 25), domain.Day(2026, time.November, 8))

	assertOccurrences(t, got, []string{
		"2026-10-25T10:00:00-04:00", // before the change
		"2026-11-01T10:00:00-05:00", // after it — same wall clock, different offset
		"2026-11-08T10:00:00-05:00",
	})
}

func TestASeriesWithoutDaysProducesNothing(t *testing.T) {
	s := domain.Schedule{StartTime: "10:00", Zone: time.UTC, StartsOn: domain.Day(2026, time.August, 1)}
	if got := s.Occurrences(domain.Day(2026, time.August, 1), domain.Day(2026, time.August, 31)); len(got) != 0 {
		t.Fatalf("expected no occurrences, got %d", len(got))
	}
}

func TestAScheduleIsRefusedBeforeItCanBePublished(t *testing.T) {
	base := weekly(t, "Asia/Kolkata", time.Saturday)

	for _, tc := range []struct {
		name string
		edit func(*domain.Schedule)
		want string
	}{
		{"no days", func(s *domain.Schedule) { s.Weekdays = nil }, "at least one day"},
		{"unreadable time", func(s *domain.Schedule) { s.StartTime = "10am" }, "HH:MM"},
		{"hour out of the day", func(s *domain.Schedule) { s.StartTime = "25:00" }, "HH:MM"},
		{"no zone", func(s *domain.Schedule) { s.Zone = nil }, "time zone"},
		{"ends before it starts", func(s *domain.Schedule) {
			d := domain.Day(2026, time.July, 1)
			s.EndsOn = &d
		}, "before it starts"},
		{"a viewing nobody can attend", func(s *domain.Schedule) { s.DurationMins = 5 }, "duration"},
		{"a coachload", func(s *domain.Schedule) { s.Capacity = 50 }, "capacity"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.edit(&s)
			err := s.Validate()
			if err == nil {
				t.Fatalf("expected a refusal, got none")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("refusal %q does not say %q", err, tc.want)
			}
		})
	}

	if err := base.Validate(); err != nil {
		t.Fatalf("a sound schedule was refused: %v", err)
	}
}

func assertOccurrences(t *testing.T, got []time.Time, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d occurrences %v, want %d %v", len(got), format(got), len(want), want)
	}
	for i, w := range want {
		if got[i].Format(time.RFC3339) != w {
			t.Errorf("occurrence %d is %s, want %s", i, got[i].Format(time.RFC3339), w)
		}
	}
}

func format(ts []time.Time) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Format(time.RFC3339)
	}
	return out
}
