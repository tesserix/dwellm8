package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// The rent schedule: which days rent falls due, what each charge covers, and
// which charges are for part of a period. ADR-0010 §7.
//
// # Charges are periods, not months
//
// A tenancy from 20 August with rent due on the 5th does not owe "August's
// rent". It owes the part of the 5 August – 5 September period it actually
// occupies, and then whole periods. So a charge is defined by the interval it
// covers, and the month it happens to sit in is incidental — which matters most
// in February, where a due day of the 31st is the 28th and the period either
// side of it is not thirty days long.
//
// # No money is computed here
//
// Period carries the day counts and the lease module does not multiply them by
// anything. ADR-0007 allows one rounding primitive and it is money.Prorate; a
// second one here would be a second answer to what a part-month costs. So this
// produces the fraction and money turns it into rupees.

// Cycle is how often rent falls due.
type Cycle string

const (
	// Monthly is every month on the due day, which is all but universal in
	// Indian residential letting.
	Monthly Cycle = "monthly"
	// Quarterly is every three months. Common for commercial and for some PG
	// arrangements, and modelled now because the alternative is a boolean that
	// grows a third value later.
	Quarterly Cycle = "quarterly"
)

// Months returns how many months a cycle spans.
func (c Cycle) Months() int {
	switch c {
	case Monthly:
		return 1
	case Quarterly:
		return 3
	}
	return 0
}

func (c Cycle) Valid() bool { return c.Months() > 0 }

// Cycles returns every cycle, ordered.
func Cycles() []Cycle { return []Cycle{Monthly, Quarterly} }

// DueDay is the day of the month rent falls due, 1 to 31.
//
// 31 means the last day of the month, whatever that is — which is the only
// sensible reading, because a lease that says "the 31st" cannot mean "skip
// February". The schema stores the same convention.
type DueDay int

// On returns the date this due day falls on in a given month, moved back where
// the month is too short. The 31st is the 28th in February and the 29th in a
// leap February; the 5th is always the 5th.
func (d DueDay) On(year int, month time.Month) effective.Date {
	last := daysIn(year, month)
	day := int(d)
	if day > last {
		day = last
	}
	return effective.Day(year, month, day)
}

func (d DueDay) Valid() bool { return d >= 1 && d <= 31 }

func daysIn(year int, month time.Month) int {
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// Period is one charge: when it falls due, what it covers, and whether it
// covers the whole of its cycle.
type Period struct {
	// DueOn is the day the charge falls due. For a part period it is the day
	// occupancy began, not the cycle's own due day — a tenant moving in on the
	// 20th pays on the 20th and then on the 5th.
	DueOn effective.Date
	// From and To bound what this charge is for, half-open per ADR-0008.
	From, To effective.Date
	// Days is what the tenant occupies, InPeriod is the cycle's full length.
	// Equal for a whole period, and the pair money.Prorate takes.
	Days, InPeriod int
	// Seq is 0 for the first charge of the tenancy.
	Seq int
}

// Partial reports whether this charge is for part of a cycle.
func (p Period) Partial() bool { return p.Days != p.InPeriod }

// Terms are the money side of a lease: what is owed, how often, when, and what
// is held.
type Terms struct {
	// RentMinor is the rent for one whole cycle, in paise. Not per month where
	// the cycle is quarterly — the amount and the cycle are one statement.
	RentMinor int64
	Cycle     Cycle
	DueDay    DueDay

	// DepositMinor is the refundable security deposit. Zero is legitimate.
	DepositMinor int64
	// DepositHeldBy is who holds it: the owner, or the managing firm. Recorded
	// because it decides who returns it, and that is the question at the end of
	// every tenancy.
	DepositHeldBy DepositHolder

	// AdvanceMonths is rent taken in advance, which is not a deposit and is
	// taxable on receipt (india-compliance.md §4). Zero for most tenancies.
	AdvanceMonths int
}

// DepositHolder is who holds the deposit.
type DepositHolder string

const (
	HeldByOwner DepositHolder = "owner"
	HeldByFirm  DepositHolder = "firm"
)

// DepositHolders returns both, ordered.
func DepositHolders() []DepositHolder { return []DepositHolder{HeldByFirm, HeldByOwner} }

func (h DepositHolder) Valid() bool {
	return h == HeldByOwner || h == HeldByFirm
}

// ErrTerms is terms that cannot generate a schedule.
var ErrTerms = errors.New("lease: the terms do not describe a rent that can be charged")

// Validate refuses terms the schedule generator cannot act on.
func (t Terms) Validate() error {
	switch {
	case t.RentMinor <= 0:
		// Not a free tenancy: a licence at no rent is a different arrangement
		// with different tax treatment, and modelling it as rent of zero would
		// have it silently generating zero invoices forever.
		return fmt.Errorf("%w: rent of %d", ErrTerms, t.RentMinor)
	case !t.Cycle.Valid():
		return fmt.Errorf("%w: %q is not a cycle", ErrTerms, t.Cycle)
	case !t.DueDay.Valid():
		return fmt.Errorf("%w: %d is not a day of the month", ErrTerms, t.DueDay)
	case t.DepositMinor < 0:
		return fmt.Errorf("%w: a deposit of %d", ErrTerms, t.DepositMinor)
	case t.DepositMinor > 0 && !t.DepositHeldBy.Valid():
		return fmt.Errorf("%w: a deposit of %d that nobody holds — the question at the end of "+
			"every tenancy is who returns it", ErrTerms, t.DepositMinor)
	case t.AdvanceMonths < 0:
		return fmt.Errorf("%w: %d months of advance", ErrTerms, t.AdvanceMonths)
	}
	return nil
}

// Schedule returns the charges for a tenancy's occupancy, up to and including
// the period containing `through`.
//
// `through` is an argument rather than today's date for the reason every as-of
// question here is: a schedule regenerated in November must produce the same
// charges for August that were raised in August.
//
// An open-ended tenancy is bounded by `through` and nothing else, which is what
// makes a periodic tenancy generatable at all.
func (l Lease) Schedule(t Terms, through effective.Date) ([]Period, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	if through.Zero() {
		return nil, errors.New("lease: a schedule must say how far to generate to")
	}
	occupancy := l.Occupancy()
	if !occupancy.Valid() {
		return nil, errors.New("lease: a tenancy with no term charges nothing")
	}

	start := occupancy.From()
	end := occupancy.To() // zero for open-ended

	var out []Period
	// The cycle boundary at or before the tenancy's start. Walking back one
	// cycle and forward again is what puts a mid-period start in the right
	// period rather than in a period of its own.
	from := cycleStart(start, t)
	for seq := 0; ; seq++ {
		next := advance(from, t)

		coverFrom := from
		if coverFrom.Before(start) {
			coverFrom = start
		}
		coverTo := next
		if !end.Zero() && end.Before(coverTo) {
			coverTo = end
		}
		if !coverFrom.Before(coverTo) {
			break // the tenancy ended before this period began
		}

		due := from
		if from.Before(start) {
			// A part period charges on the day occupancy began: a tenant moving
			// in on the 20th does not owe on a 5th that has already passed.
			due = start
		}
		out = append(out, Period{
			DueOn: due, From: coverFrom, To: coverTo,
			Days: days(coverFrom, coverTo), InPeriod: days(from, next), Seq: seq,
		})

		from = next
		// After, not on: the period *containing* the horizon is generated, so a
		// horizon of 1 September produces September's charge rather than
		// stopping the day before it.
		if from.After(through) {
			break
		}
		if !end.Zero() && !from.Before(end) {
			break
		}
		if seq > 1200 {
			// A hundred years of monthly charges. Unreachable for a real lease
			// and cheaper than a loop that never ends.
			return nil, fmt.Errorf("lease: %s generates more than 1200 periods to %s", l.ID, through)
		}
	}
	return out, nil
}

// cycleStart is the cycle boundary at or before a date.
func cycleStart(d effective.Date, t Terms) effective.Date {
	y, m := d.Time().Year(), d.Time().Month()
	due := t.DueDay.On(y, m)
	if !due.After(d) {
		return due
	}
	// The due day this month is still ahead, so the period began last month —
	// or three months ago for a quarterly cycle.
	back := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, -t.Cycle.Months(), 0)
	return t.DueDay.On(back.Year(), back.Month())
}

// advance moves one cycle on from a due date, re-deriving the day of the month
// rather than adding days — so a 31st that became the 28th in February goes
// back to the 31st in March.
func advance(d effective.Date, t Terms) effective.Date {
	first := time.Date(d.Time().Year(), d.Time().Month(), 1, 0, 0, 0, 0, time.UTC).
		AddDate(0, t.Cycle.Months(), 0)
	return t.DueDay.On(first.Year(), first.Month())
}

func days(from, to effective.Date) int {
	return int(to.Time().Sub(from.Time()).Hours() / 24)
}
