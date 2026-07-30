// Package effective is the effective-dating standard. ADR-0008.
//
// "What was the rent in March?" and "who owned this flat when the deposit was
// taken?" must be answerable from the primary tables. Not from an audit log — an
// audit log records that a row changed, and the question is what the row *said*,
// which is a different thing and is not reliably reconstructible from a diff.
//
// # Half-open intervals of dates
//
// Every effective-dated row carries [valid_from, valid_to): inclusive lower bound,
// exclusive upper bound. The successor's valid_from equals the predecessor's
// valid_to exactly — no gap to leave a date uncovered, no overlap to make two rows
// true at once, and no "the day before" arithmetic anywhere.
//
// Closed intervals are the alternative and they are worse in a way that is easy to
// miss: [1 Jan, 31 Mar] followed by [1 Apr, …] requires the writer to compute
// 31 March from 1 April, and every such computation is a place to be off by one
// across a month boundary, a leap day, or a timezone.
//
// **Dates, not timestamps.** A rent revision is effective from a date, ownership
// changes on a date, a lease starts on a date. A timestamp forces a question with
// no good answer — is 1 April effective at 00:00 IST or 00:00 UTC? — and the two
// differ by five and a half hours during which the rent is legally one number and
// technically another. Indian rental agreements are dated by day, so the column is
// `date` and the interval is `daterange`.
//
// The exception, and it is a real distinction rather than a fudge:
// delegation_grants (ADR-0005) uses timestamptz, because an authorisation window is
// not an effective date. A firm's access begins at a moment — you grant it at 9am
// and it is live at 9am — and no legal document is dated by it. Assertion 14
// enforces the split by column type and names the timestamptz tables, so a new one
// has to argue for itself.
//
// # Nothing is built wrong
//
// Interval has unexported fields and only two constructors. That is deliberate:
// the whole of this ADR's second edge case is "an open-ended interval is handled
// consistently in every helper", and a struct with an exported To field has a zero
// value that silently means open-ended. A forgotten field would then be a row that
// is true forever, which is the worst available default for a rent amount.
package effective

import (
	"errors"
	"fmt"
	"sort"
	"time"
)

// Date is a calendar day with no time and no zone. It exists so that a date cannot
// be accidentally compared against a timestamp, which is where the timezone bug
// this package avoids would otherwise re-enter.
type Date struct {
	t time.Time
}

// Day builds a date.
func Day(year int, month time.Month, day int) Date {
	return Date{t: time.Date(year, month, day, 0, 0, 0, 0, time.UTC)}
}

// DateOf truncates an instant to the calendar day it falls in, in the given
// location. The location is required rather than defaulted: "which day is this
// instant on" has no answer without it, and defaulting to UTC would put an 11pm IST
// payment on the previous day.
func DateOf(t time.Time, loc *time.Location) Date {
	if loc == nil {
		loc = time.UTC
	}
	y, m, d := t.In(loc).Date()
	return Day(y, m, d)
}

// ParseDate reads an ISO date.
func ParseDate(s string) (Date, error) {
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return Date{}, fmt.Errorf("effective: %q is not a date (want YYYY-MM-DD): %w", s, err)
	}
	return Date{t: t}, nil
}

// Zero reports whether this is the unset date. Used only to distinguish an
// open-ended interval's absent upper bound; no caller should need it.
func (d Date) Zero() bool { return d.t.IsZero() }

// Time is the date as an instant at UTC midnight, which is what the driver writes
// into a `date` column.
func (d Date) Time() time.Time { return d.t }

func (d Date) String() string {
	if d.Zero() {
		return "open"
	}
	return d.t.Format("2006-01-02")
}

// Before, After and Equal compare days.
func (d Date) Before(o Date) bool { return d.t.Before(o.t) }
func (d Date) After(o Date) bool  { return d.t.After(o.t) }
func (d Date) Equal(o Date) bool  { return d.t.Equal(o.t) }

// AddDays returns the date n days later. Used for reporting boundaries, never for
// computing an interval's end — see the package comment on closed intervals.
func (d Date) AddDays(n int) Date { return Date{t: d.t.AddDate(0, 0, n)} }

// Interval is a half-open range of days: [From, To), with To absent meaning
// open-ended.
type Interval struct {
	from Date
	to   Date // zero means unbounded above
}

// ErrInterval is what a caller checks for to tell a malformed interval from any
// other failure.
var ErrInterval = errors.New("effective: invalid validity interval")

// Since returns an open-ended interval starting on from. This is the common case:
// a rent amount that applies until somebody changes it.
func Since(from Date) (Interval, error) {
	if from.Zero() {
		return Interval{}, fmt.Errorf("%w: an interval must say when it starts", ErrInterval)
	}
	return Interval{from: from}, nil
}

// Between returns [from, to). to is exclusive: a lease running through 31 March
// ends at Between(…, Day(2026, 4, 1)), and that is the point of the convention —
// the successor starts on the same date the predecessor ends.
func Between(from, to Date) (Interval, error) {
	if from.Zero() {
		return Interval{}, fmt.Errorf("%w: an interval must say when it starts", ErrInterval)
	}
	if to.Zero() {
		return Interval{}, fmt.Errorf("%w: use Since for an open-ended interval, so that "+
			"open-ended is something a caller says rather than something a zero value means", ErrInterval)
	}
	if !from.Before(to) {
		return Interval{}, fmt.Errorf("%w: [%s, %s) is empty — the bound is exclusive, so a "+
			"single-day interval ends on the following day", ErrInterval, from, to)
	}
	return Interval{from: from, to: to}, nil
}

// From and To expose the bounds. To is the zero Date when open-ended, which is why
// Open exists — a caller should ask that rather than test the zero value.
func (i Interval) From() Date { return i.from }
func (i Interval) To() Date   { return i.to }

// Open reports whether this interval has no end yet.
func (i Interval) Open() bool { return i.to.Zero() }

// Valid reports whether this interval was built by a constructor. The zero Interval
// is not a valid interval, and is not an open-ended one.
func (i Interval) Valid() bool { return !i.from.Zero() }

func (i Interval) String() string {
	if !i.Valid() {
		return "[invalid)"
	}
	if i.Open() {
		return fmt.Sprintf("[%s, )", i.from)
	}
	return fmt.Sprintf("[%s, %s)", i.from, i.to)
}

// Contains reports whether d falls in this interval. Half-open: the lower bound is
// included and the upper bound is not.
//
// This is the whole of the as-of question, and it is one function rather than a
// predicate every caller writes, because the hand-written version
// (`valid_from <= d AND (valid_to IS NULL OR valid_to > d)`) has two places to get
// the open-ended case wrong and one to get the boundary wrong.
func (i Interval) Contains(d Date) bool {
	if !i.Valid() || d.Zero() {
		return false
	}
	if d.Before(i.from) {
		return false
	}
	if i.Open() {
		return true
	}
	return d.Before(i.to)
}

// Overlaps reports whether two intervals are ever true on the same day. Two
// open-ended intervals always overlap, which is the case a hand-written check
// usually misses.
func (i Interval) Overlaps(o Interval) bool {
	if !i.Valid() || !o.Valid() {
		return false
	}
	// [a, b) and [c, d) overlap when a < d and c < b, with an absent upper bound
	// treated as +infinity.
	aBeforeD := o.Open() || i.from.Before(o.to)
	cBeforeB := i.Open() || o.from.Before(i.to)
	return aBeforeD && cBeforeB
}

// Meets reports whether o starts exactly where i ends — the adjacency a change
// produces. It is what makes a timeline gapless without overlapping.
func (i Interval) Meets(o Interval) bool {
	return i.Valid() && o.Valid() && !i.Open() && i.to.Equal(o.from)
}

// Close ends an open-ended interval on at, which is the operation a change performs
// on the row it supersedes.
func (i Interval) Close(at Date) (Interval, error) {
	if !i.Valid() {
		return Interval{}, fmt.Errorf("%w: nothing to close", ErrInterval)
	}
	if !i.Open() {
		return Interval{}, fmt.Errorf("%w: %s is already closed — closing it again would move a "+
			"boundary that something downstream has already reported on", ErrInterval, i)
	}
	return Between(i.from, at)
}

// Kind is why a row exists: because the world changed, or because we had it wrong.
//
// The distinction is the one thing about effective dating that cannot be recovered
// later if it is not recorded at the time, and it is ADR-0008's first edge case.
type Kind string

const (
	// KindChange is the world changing. Rent went from 25,000 to 27,000 on 1 April.
	// The old row stays exactly as it was and is closed on that date, so an as-of
	// query for March still says 25,000 — because in March it *was* 25,000.
	KindChange Kind = "change"

	// KindCorrection is us having been wrong. The agreement always said 26,000 and
	// somebody typed 25,000. Closing the old row and opening a new one would assert
	// that rent was 25,000 in March, which was never true of the world — only of
	// our record of it. So the wrong row is retired over the same interval and the
	// right one replaces it.
	KindCorrection Kind = "correction"
)

// Record is one effective-dated row, generically. Value is whatever the table
// holds; this package cares only about when it was true.
type Record[T any] struct {
	ID    string
	Range Interval
	Value T
	// Kind is why this row exists.
	Kind Kind
	// Corrects is the id of the row this one replaces, set only on a correction.
	Corrects string
	// Retired reports whether a later correction replaced this row. A retired row
	// is history about our records rather than about the world, and no as-of query
	// returns it.
	Retired bool
}

// Timeline is every row ever written for one subject — one unit's rent, one flat's
// ownership — retired rows included.
type Timeline[T any] struct {
	Records []Record[T]
}

// Live returns the records an as-of query may return: everything not retired.
func (tl Timeline[T]) Live() []Record[T] {
	out := make([]Record[T], 0, len(tl.Records))
	for _, r := range tl.Records {
		if !r.Retired {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Range.from.Before(out[j].Range.from) })
	return out
}

// ErrOverlap is the failure the database's exclusion constraint also produces. It
// exists in Go so a caller can say what is wrong before the write, and in the
// schema so it is true for anything that did not come through Go.
var ErrOverlap = errors.New("effective: two live rows would be true on the same day")

// Validate asserts the one invariant: no two live rows overlap.
//
// Gaps are permitted and are not an error. A flat is unoccupied between leases, a
// unit has no rent schedule before it is first let, and a timeline that refused
// gaps would force a fictional row to cover them.
func (tl Timeline[T]) Validate() error {
	live := tl.Live()
	for i, r := range live {
		if !r.Range.Valid() {
			return fmt.Errorf("%w: record %s has no interval", ErrInterval, r.ID)
		}
		if r.Kind != KindChange && r.Kind != KindCorrection {
			return fmt.Errorf("effective: record %s does not say whether it is a change or a "+
				"correction, and that cannot be recovered later", r.ID)
		}
		if (r.Kind == KindCorrection) != (r.Corrects != "") {
			return fmt.Errorf("effective: record %s is a %s and %s the row it replaces",
				r.ID, r.Kind, map[bool]string{true: "names", false: "does not name"}[r.Corrects != ""])
		}
		for _, o := range live[i+1:] {
			if r.Range.Overlaps(o.Range) {
				return fmt.Errorf("%w: %s %s and %s %s", ErrOverlap, r.ID, r.Range, o.ID, o.Range)
			}
		}
	}
	return nil
}

// AsOf answers the question this package exists for: what was true on d.
//
// Exactly one live record can match a valid timeline, or none if d falls in a gap.
// The bool is false for a gap rather than returning a zero value, because a rent of
// zero and no rent on record are different facts.
func (tl Timeline[T]) AsOf(d Date) (Record[T], bool) {
	for _, r := range tl.Live() {
		if r.Range.Contains(d) {
			return r, true
		}
	}
	return Record[T]{}, false
}

// Current is AsOf(today), and today is a parameter.
//
// It does not read the clock, for the reason the reconciliation and workflow
// packages do not either: a function that reads the clock cannot be tested at a
// boundary, and every interesting bug in effective dating is at a boundary. It also
// means the caller states which timezone's "today" it means, which is the same
// question DateOf refuses to guess.
func (tl Timeline[T]) Current(today Date) (Record[T], bool) { return tl.AsOf(today) }

// Change is what to write when the world changes on effectiveFrom.
//
// It returns the closed predecessor and the new successor. The predecessor is
// returned rather than mutated so the caller writes both in one transaction and
// the interval is never momentarily open at both ends — the state in which two rows
// are true at once, which the exclusion constraint would refuse and which is
// therefore a failed write rather than a lost history.
func Change[T any](tl Timeline[T], effectiveFrom Date, value T) (closed Record[T], next Record[T], err error) {
	if err := tl.Validate(); err != nil {
		return closed, next, err
	}
	iv, err := Since(effectiveFrom)
	if err != nil {
		return closed, next, err
	}
	next = Record[T]{Range: iv, Value: value, Kind: KindChange}

	current, found := tl.AsOf(effectiveFrom)
	if !found {
		// A gap, or the first row. Nothing to close.
		return Record[T]{}, next, nil
	}
	if current.Range.from.Equal(effectiveFrom) {
		// A change effective on the day the current row starts would close it to an
		// empty interval. That is not a change, it is a correction of a row that was
		// never true for a single day, and the caller has to say which it means.
		return closed, next, fmt.Errorf("effective: a change effective %s would leave row %s "+
			"true for no days at all — if the current row is wrong rather than superseded, "+
			"correct it instead", effectiveFrom, current.ID)
	}
	closedRange, err := current.Range.Close(effectiveFrom)
	if err != nil {
		return closed, next, err
	}
	closed = current
	closed.Range = closedRange
	return closed, next, nil
}

// Correct is what to write when the record was wrong and the world was not.
//
// It returns the row to retire and its replacement, over the *same* interval. The
// wrong row is kept, because "we thought the rent was 25,000 until Tuesday" is
// occasionally the answer to a dispute — but it is retired, so no as-of query
// returns it and no report sums it.
//
// What this does not do is let an as-of query ask "what did we believe last
// Tuesday". That is bitemporality and it is out of scope; the retired rows make it
// answerable by hand and nothing more. ADR-0008 says so explicitly rather than
// leaving somebody to discover it.
func Correct[T any](tl Timeline[T], targetID string, value T) (retired Record[T], replacement Record[T], err error) {
	if err := tl.Validate(); err != nil {
		return retired, replacement, err
	}
	for _, r := range tl.Live() {
		if r.ID != targetID {
			continue
		}
		if r.ID == "" {
			return retired, replacement, errors.New("effective: a correction must name the row it replaces")
		}
		retired = r
		retired.Retired = true
		replacement = Record[T]{
			Range: r.Range, Value: value, Kind: KindCorrection, Corrects: r.ID,
		}
		return retired, replacement, nil
	}
	return retired, replacement, fmt.Errorf("effective: no live row %q to correct — correcting a "+
		"row that is already retired would leave two corrections claiming the same interval", targetID)
}
