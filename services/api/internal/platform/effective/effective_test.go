package effective_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Rent in paise, as int64 rather than money.Minor. This package is platform
// infrastructure and the modules sit above it, so it does not import one — the arch
// test enforces that, and the test file is held to the same rule as the code.
type paise = int64

// ADR-0008, tested where it can be tested: the arithmetic of half-open intervals,
// and the distinction between the world changing and our record being wrong.
//
// The exclusion constraint is PostgreSQL's and is asserted in the isolation harness.
// What is here is the part a caller touches, and the part where the boundary bugs
// live — every interesting failure in effective dating happens on the day a
// boundary falls.

func day(y int, m time.Month, d int) effective.Date { return effective.Day(y, m, d) }

// The story's primary scenario, exactly as written. A rent revision from 25,000 to
// 27,000 effective 1 April; as-of 15 March returns 25,000 and as-of 15 April
// returns 27,000, from the same timeline.
func TestARentRevisionIsAnsweredCorrectlyOnBothSidesOfItsEffectiveDate(t *testing.T) {
	const before, after paise = 2_500_000, 2_700_000

	// The lease's original rent, open-ended: it applies until somebody changes it.
	iv, err := effective.Since(day(2026, time.January, 1))
	if err != nil {
		t.Fatalf("Since: %v", err)
	}
	tl := effective.Timeline[paise]{Records: []effective.Record[paise]{
		{ID: "rent-1", Range: iv, Value: before, Kind: effective.KindChange},
	}}

	closed, next, err := effective.Change(tl, day(2026, time.April, 1), after)
	if err != nil {
		t.Fatalf("Change: %v", err)
	}
	closed.ID, next.ID = "rent-1", "rent-2"
	revised := effective.Timeline[paise]{Records: []effective.Record[paise]{closed, next}}
	if err := revised.Validate(); err != nil {
		t.Fatalf("the revised timeline does not validate: %v", err)
	}

	for _, tc := range []struct {
		on   effective.Date
		want paise
	}{
		{day(2026, time.March, 15), before},
		{day(2026, time.April, 15), after},
		// The boundaries, which is where this gets got wrong. 31 March is the last
		// day of the old rent and 1 April is the first day of the new one, because
		// the upper bound is exclusive.
		{day(2026, time.March, 31), before},
		{day(2026, time.April, 1), after},
		{day(2026, time.January, 1), before},
		{day(2030, time.December, 31), after},
	} {
		got, ok := revised.AsOf(tc.on)
		if !ok {
			t.Fatalf("no rent on record for %s", tc.on)
		}
		if got.Value != tc.want {
			t.Errorf("rent on %s is %d, want %d", tc.on, got.Value, tc.want)
		}
	}

	// The predecessor was closed, not deleted: March's answer comes from the same
	// row it always did.
	if closed.Range.Open() {
		t.Error("the superseded row is still open-ended, so two rows are true in April")
	}
	if !closed.Range.Meets(next.Range) {
		t.Errorf("%s and %s neither meet nor overlap — a date between them has no rent",
			closed.Range, next.Range)
	}
	if got, _ := revised.AsOf(day(2026, time.March, 15)); got.ID != "rent-1" {
		t.Errorf("March's answer comes from %s, want the original row", got.ID)
	}
}

// The failure scenario, in Go. The database's exclusion constraint is the authority;
// this reports it before the write and names both rows.
func TestOverlappingLiveIntervalsAreRefused(t *testing.T) {
	a, _ := effective.Between(day(2026, time.January, 1), day(2026, time.July, 1))
	b, _ := effective.Between(day(2026, time.June, 1), day(2026, time.December, 1))

	tl := effective.Timeline[int]{Records: []effective.Record[int]{
		{ID: "a", Range: a, Value: 1, Kind: effective.KindChange},
		{ID: "b", Range: b, Value: 2, Kind: effective.KindChange},
	}}
	err := tl.Validate()
	if err == nil {
		t.Fatal("two rows true through June were accepted")
	}
	if !errors.Is(err, effective.ErrOverlap) {
		t.Errorf("the error is not distinguishable as an overlap: %v", err)
	}
	t.Logf("refused: %v", err)

	// Adjacent is not overlapping — the case a naive check gets wrong.
	c, _ := effective.Between(day(2026, time.January, 1), day(2026, time.July, 1))
	d, _ := effective.Since(day(2026, time.July, 1))
	ok := effective.Timeline[int]{Records: []effective.Record[int]{
		{ID: "c", Range: c, Value: 1, Kind: effective.KindChange},
		{ID: "d", Range: d, Value: 2, Kind: effective.KindChange},
	}}
	if err := ok.Validate(); err != nil {
		t.Errorf("adjacent intervals were called overlapping: %v", err)
	}
}

// Two open-ended intervals always overlap, and it is the check a hand-written
// predicate misses because neither has an upper bound to compare.
func TestOpenEndedIsHandledEverywhere(t *testing.T) {
	open1, _ := effective.Since(day(2026, time.January, 1))
	open2, _ := effective.Since(day(2030, time.January, 1))
	closed, _ := effective.Between(day(2020, time.January, 1), day(2021, time.January, 1))

	if !open1.Overlaps(open2) {
		t.Error("two open-ended intervals were called non-overlapping — both are true in 2030")
	}
	if !open2.Overlaps(open1) {
		t.Error("Overlaps is not symmetric")
	}
	if open1.Overlaps(closed) || closed.Overlaps(open1) {
		t.Error("an open-ended interval starting in 2026 overlaps one that ended in 2021")
	}
	// Contains, at a date no closed interval would reach.
	if !open1.Contains(day(2099, time.December, 31)) {
		t.Error("an open-ended interval does not contain a date far in the future")
	}
	if open1.Contains(day(2025, time.December, 31)) {
		t.Error("an open-ended interval contains a date before it starts")
	}
	if !open1.Open() || closed.Open() {
		t.Error("Open() disagrees with how the interval was built")
	}
	// And Close is the only way to end one, once.
	ended, err := open1.Close(day(2027, time.January, 1))
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := ended.Close(day(2028, time.January, 1)); err == nil {
		t.Error("an already-closed interval was closed again, moving a boundary something " +
			"downstream has already reported on")
	}
}

// The zero value must not mean open-ended. A struct literal with a forgotten field
// would otherwise be a rent that applies forever.
func TestAnIntervalCannotBeBuiltHalfInitialised(t *testing.T) {
	var zero effective.Interval
	if zero.Valid() {
		t.Error("the zero Interval reports itself valid")
	}
	if zero.Open() != true {
		// It has no upper bound, but it is not a usable open-ended interval —
		// Valid() is what a caller must check, and Contains refuses regardless.
		t.Log("the zero interval has no upper bound, which is why Valid() exists")
	}
	if zero.Contains(day(2026, time.March, 15)) {
		t.Error("the zero Interval contains a date — a forgotten field would be true forever")
	}
	if zero.Overlaps(zero) {
		t.Error("the zero Interval overlaps itself, which would let two of them be written")
	}

	// And the constructors refuse the shapes that are not intervals.
	for _, tc := range []struct {
		name string
		call func() (effective.Interval, error)
	}{
		{"no start", func() (effective.Interval, error) { return effective.Since(effective.Date{}) }},
		{"an empty range", func() (effective.Interval, error) {
			return effective.Between(day(2026, time.April, 1), day(2026, time.April, 1))
		}},
		{"a range that ends before it starts", func() (effective.Interval, error) {
			return effective.Between(day(2026, time.April, 1), day(2026, time.March, 1))
		}},
		{"Between with no end, which is what Since is for", func() (effective.Interval, error) {
			return effective.Between(day(2026, time.April, 1), effective.Date{})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.call(); err == nil {
				t.Errorf("accepted: %s", tc.name)
			} else if !errors.Is(err, effective.ErrInterval) {
				t.Errorf("not reported as an interval problem: %v", err)
			}
		})
	}

	// A single-day interval, which is the thing the exclusive bound makes people
	// doubt. A lease for 1 April only ends on 2 April.
	oneDay, err := effective.Between(day(2026, time.April, 1), day(2026, time.April, 2))
	if err != nil {
		t.Fatalf("a single-day interval was refused: %v", err)
	}
	if !oneDay.Contains(day(2026, time.April, 1)) || oneDay.Contains(day(2026, time.April, 2)) {
		t.Error("a single-day interval does not cover exactly one day")
	}
}

// ADR-0008's first edge case, and the one thing that cannot be recovered if it is
// not recorded at the time.
//
// A change says the world changed on a date. A correction says our record was wrong
// and the world was not. They produce different histories on purpose, and the test
// is that an as-of query for the earlier period gives a different answer in each
// case.
func TestACorrectionIsNotAChange(t *testing.T) {
	const typo, actual, revised paise = 2_500_000, 2_600_000, 2_700_000

	iv, _ := effective.Since(day(2026, time.January, 1))
	original := effective.Timeline[paise]{Records: []effective.Record[paise]{
		{ID: "rent-1", Range: iv, Value: typo, Kind: effective.KindChange},
	}}

	// The world changed on 1 April: March keeps the old number, because in March it
	// was the old number.
	closed, next, err := effective.Change(original, day(2026, time.April, 1), revised)
	if err != nil {
		t.Fatalf("Change: %v", err)
	}
	closed.ID, next.ID = "rent-1", "rent-2"
	changed := effective.Timeline[paise]{Records: []effective.Record[paise]{closed, next}}
	got, _ := changed.AsOf(day(2026, time.March, 15))
	if got.Value != typo {
		t.Errorf("after a change, March says %d — a change must not rewrite the past", got.Value)
	}

	// Our record was wrong: the agreement always said 26,000. March must now say
	// 26,000, because it was always 26,000.
	retired, replacement, err := effective.Correct(original, "rent-1", actual)
	if err != nil {
		t.Fatalf("Correct: %v", err)
	}
	replacement.ID = "rent-1c"
	corrected := effective.Timeline[paise]{
		Records: []effective.Record[paise]{retired, replacement},
	}
	if err := corrected.Validate(); err != nil {
		t.Fatalf("the corrected timeline does not validate: %v — a retired row must not count "+
			"as an overlap", err)
	}
	got, ok := corrected.AsOf(day(2026, time.March, 15))
	if !ok {
		t.Fatal("no rent on record for March after a correction")
	}
	if got.Value != actual {
		t.Errorf("after a correction, March says %d, want %d — a correction replaces what we "+
			"believed, over the interval it was believed for", got.Value, actual)
	}

	// The two are distinguishable from the rows alone, which is the criterion.
	if replacement.Kind != effective.KindCorrection || replacement.Corrects != "rent-1" {
		t.Error("the replacement does not record that it is a correction, or what it corrects")
	}
	if next.Kind != effective.KindChange || next.Corrects != "" {
		t.Error("a change claims to correct something")
	}
	// The wrong row is kept and retired, not deleted: "we thought it was 25,000
	// until Tuesday" is occasionally the answer to a dispute.
	if !retired.Retired {
		t.Error("the corrected row was not retired, so two rows are true in March")
	}
	if len(corrected.Live()) != 1 {
		t.Errorf("%d live rows after a correction, want 1", len(corrected.Live()))
	}
}

// A row cannot say it is a correction without naming what it corrects, or name one
// while claiming to be a change. Either way the distinction is lost.
func TestARowMustSayWhyItExists(t *testing.T) {
	iv, _ := effective.Since(day(2026, time.January, 1))
	for _, tc := range []struct {
		name string
		rec  effective.Record[int]
		want string
	}{
		{"no kind at all", effective.Record[int]{ID: "a", Range: iv}, "change or a correction"},
		{"a correction that names nothing", effective.Record[int]{
			ID: "a", Range: iv, Kind: effective.KindCorrection}, "does not name"},
		{"a change that names a predecessor", effective.Record[int]{
			ID: "a", Range: iv, Kind: effective.KindChange, Corrects: "b"}, "names"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tl := effective.Timeline[int]{Records: []effective.Record[int]{tc.rec}}
			err := tl.Validate()
			if err == nil {
				t.Fatalf("accepted: %s", tc.name)
			}
			if got := err.Error(); !strings.Contains(got, tc.want) {
				t.Errorf("error %q does not mention %q", got, tc.want)
			}
		})
	}
}

// A gap is legal and is not zero. A flat is unoccupied between leases, and "no rent
// on record" is a different fact from "rent of nothing".
func TestAGapIsLegalAndIsNotZero(t *testing.T) {
	first, _ := effective.Between(day(2025, time.January, 1), day(2025, time.July, 1))
	second, _ := effective.Since(day(2026, time.January, 1))
	tl := effective.Timeline[paise]{Records: []effective.Record[paise]{
		{ID: "a", Range: first, Value: 2_500_000, Kind: effective.KindChange},
		{ID: "b", Range: second, Value: 2_700_000, Kind: effective.KindChange},
	}}
	if err := tl.Validate(); err != nil {
		t.Fatalf("a timeline with a vacancy in it was refused: %v", err)
	}
	if _, ok := tl.AsOf(day(2025, time.October, 1)); ok {
		t.Error("a date in the vacancy returned a rent")
	}
	if r, ok := tl.AsOf(day(2025, time.June, 30)); !ok || r.ID != "a" {
		t.Error("the last day of the first interval is not covered by it")
	}
}

// Every day in a range is covered by exactly one live row, or by none. Anything else
// means an as-of query has two answers, which is the failure the whole ADR exists to
// prevent — and it is the property the exclusion constraint enforces in the database.
func TestEveryDayHasAtMostOneAnswer(t *testing.T) {
	// Three changes and one correction, over two years.
	iv1, _ := effective.Between(day(2025, time.January, 1), day(2025, time.April, 1))
	iv2, _ := effective.Between(day(2025, time.April, 1), day(2026, time.January, 1))
	iv3, _ := effective.Since(day(2026, time.January, 1))
	retired, _ := effective.Between(day(2025, time.April, 1), day(2026, time.January, 1))

	tl := effective.Timeline[int]{Records: []effective.Record[int]{
		{ID: "a", Range: iv1, Value: 1, Kind: effective.KindChange},
		{ID: "b-wrong", Range: retired, Value: 99, Kind: effective.KindChange, Retired: true},
		{ID: "b", Range: iv2, Value: 2, Kind: effective.KindCorrection, Corrects: "b-wrong"},
		{ID: "c", Range: iv3, Value: 3, Kind: effective.KindChange},
	}}
	if err := tl.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	live := tl.Live()
	for d := day(2024, time.December, 1); d.Before(day(2027, time.January, 1)); d = d.AddDays(1) {
		matches := 0
		for _, r := range live {
			if r.Range.Contains(d) {
				matches++
			}
		}
		if matches > 1 {
			t.Fatalf("%s is covered by %d live rows — an as-of query has %d answers", d, matches, matches)
		}
		got, ok := tl.AsOf(d)
		if (matches == 1) != ok {
			t.Fatalf("%s: %d live rows cover it and AsOf says found=%v", d, matches, ok)
		}
		if ok && got.Value == 99 {
			t.Fatalf("%s returned the retired row", d)
		}
	}
}

// A change effective on the day the current row starts is not a change: it would
// close that row to nothing. It is a correction, and the caller has to say so.
func TestAChangeCannotCloseARowToNothing(t *testing.T) {
	iv, _ := effective.Since(day(2026, time.April, 1))
	tl := effective.Timeline[int]{Records: []effective.Record[int]{
		{ID: "a", Range: iv, Value: 1, Kind: effective.KindChange},
	}}
	if _, _, err := effective.Change(tl, day(2026, time.April, 1), 2); err == nil {
		t.Fatal("a change effective on the current row's own start date was accepted, leaving a " +
			"row true for no days at all")
	} else {
		t.Logf("refused: %v", err)
	}
	// And correcting it is accepted, because that is what it actually was.
	if _, _, err := effective.Correct(tl, "a", 2); err != nil {
		t.Errorf("the correction was refused: %v", err)
	}
}

// A correction may only target a live row. Correcting an already-retired one would
// leave two corrections claiming the same interval, and no way to say which is
// current.
func TestOnlyALiveRowCanBeCorrected(t *testing.T) {
	iv, _ := effective.Since(day(2026, time.January, 1))
	tl := effective.Timeline[int]{Records: []effective.Record[int]{
		{ID: "old", Range: iv, Value: 1, Kind: effective.KindChange, Retired: true},
		{ID: "new", Range: iv, Value: 2, Kind: effective.KindCorrection, Corrects: "old"},
	}}
	if _, _, err := effective.Correct(tl, "old", 3); err == nil {
		t.Error("a retired row was corrected")
	}
	if _, _, err := effective.Correct(tl, "new", 3); err != nil {
		t.Errorf("the live row could not be corrected: %v", err)
	}
	if _, _, err := effective.Correct(tl, "nobody", 3); err == nil {
		t.Error("a row that does not exist was corrected")
	}
}

// DateOf will not guess a timezone, because "which day is this instant on" has no
// answer without one — and defaulting to UTC would file an 11pm payment in Mumbai
// under the previous day.
func TestDateOfNeedsAZoneToBeMeaningful(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skipf("no tzdata: %v", err)
	}
	// 2026-04-01 00:30 IST is 2026-03-31 19:00 UTC. The same instant, two days.
	instant := time.Date(2026, time.April, 1, 0, 30, 0, 0, ist)
	if got := effective.DateOf(instant, ist); got.String() != "2026-04-01" {
		t.Errorf("in IST the instant is on %s, want 2026-04-01", got)
	}
	if got := effective.DateOf(instant, time.UTC); got.String() != "2026-03-31" {
		t.Errorf("in UTC the instant is on %s, want 2026-03-31", got)
	}
	// Which is exactly why a rent effective date is a date and never an instant: a
	// five-and-a-half-hour window in which the rent is legally one number and
	// technically another.
}

func TestParseDateRoundTrips(t *testing.T) {
	for _, s := range []string{"2026-01-01", "2026-04-01", "2026-12-31", "2024-02-29"} {
		d, err := effective.ParseDate(s)
		if err != nil {
			t.Fatalf("ParseDate(%q): %v", s, err)
		}
		if d.String() != s {
			t.Errorf("%q round-tripped as %q", s, d.String())
		}
	}
	for _, s := range []string{"", "01-01-2026", "2026-13-01", "2026-02-30", "2026-04-01T00:00:00Z"} {
		if _, err := effective.ParseDate(s); err == nil {
			t.Errorf("accepted %q as a date", s)
		}
	}
}
