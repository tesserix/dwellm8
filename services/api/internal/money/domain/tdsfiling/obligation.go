// Package tdsfiling turns a TDS deduction into the calendar of things somebody
// must actually do, and states what it costs when they do not.
//
// ADR-0024 chose the section and ADR-0025 found the rate. What neither answers
// is the question a tenant actually has — "so what do I do, and by when" — and
// that answer is different for each section in ways nobody remembers:
//
//   - §194-I and §195 deduct on every payment, deposit by the 7th of the next
//     month, file quarterly, and certify 15 days after that.
//   - §194-IB deducts once, at the end of the year or the tenancy, and its whole
//     filing is one form due 30 days after the month it was deducted in.
//
// And a deadline nobody meets has a price. §201(1A) charges interest monthly —
// 1% for failing to deduct and 1.5% for deducting and not depositing, which is
// the more expensive of the two and the one that surprises people. §234E adds
// ₹200 a day for a late return, capped at the tax. This package states those
// consequences in the words the tenant is warned with, because "overdue" is not
// a warning, it is a colour.
//
// Nothing here files anything, and nothing here computes money: an obligation
// carries the deduction the money module already computed and the dates it is
// owed by.
package tdsfiling

import (
	"errors"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// Step is one thing that has to happen, in the order it happens.
type Step string

const (
	// Deduct is withholding the tax from the payment. Missing this is the
	// cheaper failure and the one that compounds, because the deduction that
	// was never made is still owed.
	Deduct Step = "deduct"
	// Deposit is paying it to the government — a challan, or a Form 24G book
	// entry for a government deductor.
	Deposit Step = "deposit"
	// Report is the return: 26Q for a resident, 27Q for a non-resident, and for
	// §194-IB the challan-cum-statement 26QC, which is both at once.
	Report Step = "report"
	// Certify is the certificate the landlord needs to claim the credit: Form
	// 16A, or 16C under §194-IB. Skipping it does not cost the tenant anything
	// and costs the landlord the credit, which is why it is tracked here rather
	// than left to goodwill.
	Certify Step = "certify"
)

// Steps returns every step, in order.
func Steps() []Step { return []Step{Deduct, Deposit, Report, Certify} }

// Due is one step with its deadline and the artefact that discharges it.
type Due struct {
	Step Step
	// By is the statutory deadline.
	By effective.Date
	// Artefact is what proves it was done.
	Artefact tds.Artefact
	// Reference is the challan number, acknowledgement number or certificate
	// number, once recorded. Empty means outstanding.
	Reference string
	// DoneOn is when it was actually done.
	DoneOn effective.Date
}

// Outstanding reports whether this step is still owed.
func (d Due) Outstanding() bool { return d.Reference == "" || d.DoneOn.Zero() }

// Late reports whether the deadline has passed with the step outstanding.
func (d Due) Late(on effective.Date) bool {
	return d.Outstanding() && on.After(d.By)
}

// Obligation is everything one deduction obliges, on one lease, for one period.
type Obligation struct {
	LeaseID string
	Section tds.Section
	// Period is what the deduction is on, half-open.
	Period effective.Interval
	// PaidOn is the date rent was paid or credited, whichever was earlier —
	// which is what starts every clock below.
	PaidOn effective.Date
	// AmountMinor is the tax deducted, in paise. Computed elsewhere.
	AmountMinor int64
	// Government is whether the deductor deposits by book entry rather than by
	// challan.
	Government bool

	Schedule []Due
}

// ErrObligation is an obligation that cannot be scheduled.
var ErrObligation = errors.New("tdsfiling: the deduction does not say enough to schedule")

// New builds the calendar for a deduction.
//
// leaseEnd is the date the tenancy ends, or the zero date for one still running.
// It matters only under §194-IB, where the deduction falls at the *earlier* of
// the financial year end and the end of the tenancy — a tenant who moves out in
// November does not wait until March.
func New(section tds.Section, leaseID string, period effective.Interval,
	paidOn effective.Date, amountMinor int64, government bool, leaseEnd effective.Date,
) (Obligation, error) {
	switch {
	case leaseID == "":
		return Obligation{}, fmt.Errorf("%w: no lease", ErrObligation)
	case !period.Valid():
		return Obligation{}, fmt.Errorf("%w: no period", ErrObligation)
	case paidOn.Zero():
		return Obligation{}, fmt.Errorf("%w: nothing says when the rent was paid, and every "+
			"deadline runs from that date", ErrObligation)
	case amountMinor <= 0:
		return Obligation{}, fmt.Errorf("%w: a deduction of %d", ErrObligation, amountMinor)
	}

	o := Obligation{
		LeaseID: leaseID, Section: section, Period: period,
		PaidOn: paidOn, AmountMinor: amountMinor, Government: government,
	}

	switch section {
	case tds.Section194IB:
		o.Schedule = schedule194IB(paidOn, leaseEnd)
	case tds.Section194I, tds.Section195:
		o.Schedule = schedulePerPayment(section, paidOn, government)
	default:
		return Obligation{}, fmt.Errorf("%w: %q is not a section this package schedules",
			ErrObligation, section)
	}
	return o, nil
}

// schedulePerPayment is §194-I and §195: deduct on payment, deposit by the 7th
// of the following month, file quarterly, certify 15 days later.
func schedulePerPayment(section tds.Section, paidOn effective.Date, government bool) []Due {
	deposit := seventhOfNextMonth(paidOn)

	challan := tds.Challan
	if government {
		challan = tds.BookEntry24G
	}
	ret, cert := tds.Return26Q, tds.Form16A
	if section == tds.Section195 {
		ret = tds.Return27Q
	}

	returnBy := quarterlyReturnDue(paidOn)
	return []Due{
		{Step: Deduct, By: paidOn, Artefact: challan},
		{Step: Deposit, By: deposit, Artefact: challan},
		{Step: Report, By: returnBy, Artefact: ret},
		{Step: Certify, By: returnBy.AddDays(15), Artefact: cert},
	}
}

// schedule194IB is the once-a-year path: one form, one certificate, and a clock
// that starts at the end of the month the deduction fell in.
func schedule194IB(paidOn, leaseEnd effective.Date) []Due {
	deductOn := deductionPoint194IB(paidOn, leaseEnd)
	// Within 30 days from the end of the month in which the deduction is made.
	form26QC := endOfMonth(deductOn).AddDays(30)
	return []Due{
		{Step: Deduct, By: deductOn, Artefact: tds.Form26QC},
		// No separate deposit step: 26QC is a challan-cum-statement, so
		// depositing and reporting are one act. Modelling them separately would
		// produce a reminder for a challan that does not exist.
		{Step: Report, By: form26QC, Artefact: tds.Form26QC},
		{Step: Certify, By: form26QC.AddDays(15), Artefact: tds.Form16C},
	}
}

// deductionPoint194IB is the earlier of the financial year end and the end of
// the tenancy — the "or of the tenancy" half is the one products forget, and it
// is the half that applies to a tenant moving out in November.
func deductionPoint194IB(paidOn, leaseEnd effective.Date) effective.Date {
	yearEnd := financialYearEnd(paidOn)
	if !leaseEnd.Zero() && leaseEnd.Before(yearEnd) {
		return leaseEnd
	}
	return yearEnd
}

// financialYearEnd is 31 March of the year the date falls in, Indian financial
// years running April to March.
func financialYearEnd(d effective.Date) effective.Date {
	y := d.Time().Year()
	if d.Time().Month() >= time.April {
		y++
	}
	return effective.Day(y, time.March, 31)
}

// seventhOfNextMonth is the deposit deadline, with the one exception that
// catches everybody: tax deducted in March is due on 30 April, not 7 April.
func seventhOfNextMonth(d effective.Date) effective.Date {
	y, m := d.Time().Year(), d.Time().Month()
	if m == time.March {
		return effective.Day(y, time.April, 30)
	}
	next := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	return effective.Day(next.Year(), next.Month(), 7)
}

// quarterlyReturnDue is the return deadline for the quarter a payment falls in.
// Q4 is 31 May rather than 30 April, which is the other date people get wrong.
func quarterlyReturnDue(d effective.Date) effective.Date {
	y, m := d.Time().Year(), d.Time().Month()
	switch {
	case m >= time.April && m <= time.June:
		return effective.Day(y, time.July, 31)
	case m >= time.July && m <= time.September:
		return effective.Day(y, time.October, 31)
	case m >= time.October && m <= time.December:
		return effective.Day(y+1, time.January, 31)
	default: // January to March
		return effective.Day(y, time.May, 31)
	}
}

func endOfMonth(d effective.Date) effective.Date {
	y, m := d.Time().Year(), d.Time().Month()
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	return effective.Day(y, m, last)
}

// Next returns the next step still outstanding, and false when everything is
// done.
func (o Obligation) Next() (Due, bool) {
	for _, d := range o.Schedule {
		if d.Outstanding() {
			return d, true
		}
	}
	return Due{}, false
}

// Overdue returns every step whose deadline has passed and which is still
// outstanding, earliest first.
func (o Obligation) Overdue(on effective.Date) []Due {
	var out []Due
	for _, d := range o.Schedule {
		if d.Late(on) {
			out = append(out, d)
		}
	}
	return out
}

// Record marks a step done, with the reference that proves it.
//
// A step cannot be recorded without one: "deposited, trust me" is the state this
// tracker exists to stop, because the challan number is the only thing that
// links the money to the deduction when the notice arrives two years later.
func (o Obligation) Record(step Step, reference string, on effective.Date) (Obligation, error) {
	if reference == "" {
		return o, fmt.Errorf("%w: %s with no reference — the challan or acknowledgement number "+
			"is the only thing that links the payment to this deduction", ErrObligation, step)
	}
	if on.Zero() {
		return o, fmt.Errorf("%w: %s with no date", ErrObligation, step)
	}
	out := o
	out.Schedule = append([]Due(nil), o.Schedule...)
	for i, d := range out.Schedule {
		if d.Step != step {
			continue
		}
		out.Schedule[i].Reference, out.Schedule[i].DoneOn = reference, on
		return out, nil
	}
	return o, fmt.Errorf("%w: section %s has no %s step", ErrObligation, o.Section, step)
}

// Consequence is what a missed step costs, in the words the tenant is warned
// with.
//
// Stated rather than scored, because "overdue" is a colour and this is a number
// with a statute behind it. The interest is described rather than computed: the
// months are counted from the due date to the day the default ends, and that day
// is in the future until somebody acts.
type Consequence struct {
	Step Step
	// Under is the section that charges it.
	Under string
	// Because is the sentence shown to the deductor.
	Because string
	// MonthsRunning is how many months of interest have accrued, counting part
	// months as whole ones — which is how §201(1A) counts them.
	MonthsRunning int
}

// Consequences returns what each overdue step is costing, worst first.
func (o Obligation) Consequences(on effective.Date) []Consequence {
	var out []Consequence
	for _, d := range o.Overdue(on) {
		months := monthsBetween(d.By, on)
		switch d.Step {
		case Deduct:
			out = append(out, Consequence{
				Step: Deduct, Under: "201(1A)(i)", MonthsRunning: months,
				Because: fmt.Sprintf("Tax that should have been deducted on %s was not. Interest "+
					"runs at 1%% a month from that date — %d month(s) so far — and the tax itself "+
					"is still owed. The liability is the deductor's, not the landlord's.",
					d.By, months),
			})
		case Deposit:
			out = append(out, Consequence{
				Step: Deposit, Under: "201(1A)(ii)", MonthsRunning: months,
				Because: fmt.Sprintf("Tax was deducted and not deposited by %s. Interest runs at "+
					"1.5%% a month from the date of deduction — %d month(s) so far. This is the "+
					"more expensive of the two defaults, and deducting without depositing can be "+
					"prosecuted under section 276B.", d.By, months),
			})
		case Report:
			out = append(out, Consequence{
				Step: Report, Under: "234E", MonthsRunning: months,
				Because: fmt.Sprintf("The return was due on %s. A late fee of ₹200 a day accrues "+
					"until it is filed, capped at the tax deducted, and the landlord cannot see "+
					"the credit in their 26AS until it is.", d.By),
			})
		case Certify:
			out = append(out, Consequence{
				Step: Certify, Under: "272A(2)(g)", MonthsRunning: months,
				Because: fmt.Sprintf("The certificate was due on %s. A penalty of ₹100 a day "+
					"applies, and until it is issued the landlord has no document for the tax "+
					"already taken from their rent.", d.By),
			})
		}
	}
	return out
}

// monthsBetween counts part months as whole ones, which is what section 201(1A)
// does and why a single day late costs a month's interest.
func monthsBetween(from, to effective.Date) int {
	if !to.After(from) {
		return 0
	}
	f, t := from.Time(), to.Time()
	months := (t.Year()-f.Year())*12 + int(t.Month()) - int(f.Month())
	if t.Day() > f.Day() {
		months++
	}
	if months < 1 {
		months = 1
	}
	return months
}
