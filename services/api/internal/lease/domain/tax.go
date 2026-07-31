package domain

import (
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
)

// The lease's side of the TDS decision matrix. ADR-0024.
//
// The matrix itself is in platform/statutory/tds, because money needs it too and a
// module may not reach into another module's domain (ADR-0001 §3). What belongs
// here is when the two facts are demanded and what happens when they are missing:
// at activation, and the tenancy does not start.
//
// Activation is the right gate rather than draft creation. A draft is a document
// being written and may legitimately be incomplete; the moment it becomes a
// tenancy, rent will be paid under it, and a payment made under facts nobody
// recorded is a deduction nobody made.

// ErrTaxFacts is a lease that cannot say which TDS section governs it.
var ErrTaxFacts = tds.ErrFacts

// TaxPathOn returns the section that governs a payment made on a date, with the
// facts it was selected from.
//
// It is dated because the facts are: a landlord who becomes non-resident in
// October leaves April's payment on section 194-I, deducted, deposited and
// certified as it was, and restating it under October's facts would be a
// correction of something that was correct.
func (l Lease) TaxPathOn(on effective.Date) (tds.Path, tds.Facts, error) {
	p, f, err := l.Tax.PathOn(on)
	if err != nil {
		return tds.Path{}, tds.Facts{}, fmt.Errorf("lease %s: %w", l.ID, err)
	}
	return p, f, nil
}

// Activate starts the tenancy, and refuses to start one whose tax path is unknown
// or unacknowledged.
//
// The two refusals are different failures and stay distinguishable: ErrTaxFacts is
// missing data and the answer is a form, tds.ErrNotAcknowledged is a tenant who has
// not been told what they are taking on and the answer is a screen with the
// obligation on it.
func (l Lease) Activate(by Actor) (Lease, Event, error) {
	if !MayTransition(l.State, StateActive, by) {
		return l, "", fmt.Errorf("%w: a %s may not activate a %s lease", ErrTransition, by, l.State)
	}
	path, facts, err := l.TaxPathOn(l.Term.From())
	if err != nil {
		return l, "", fmt.Errorf("%w — the deductor class and the landlord's residency are captured "+
			"before the tenancy starts, because a tenancy that discovers them at the first payout has "+
			"already under-deducted", err)
	}
	if err := path.RequireAcknowledgement(facts); err != nil {
		return l, "", err
	}

	out := l
	out.State = StateActive
	return out, EventStarted, nil
}

// TaxSectionChanges is the dates within the tenancy on which the governing section
// changes.
//
// Empty for almost every lease. Where it is not, it is the fact the owner statement
// and the payout run both have to know before they meet it: two sections in one
// financial year means two rates, two return series and two certificates.
func (l Lease) TaxSectionChanges() ([]effective.Date, error) {
	changes, err := l.Tax.Changes()
	if err != nil {
		return nil, fmt.Errorf("lease %s: %w", l.ID, err)
	}
	occupancy := l.Occupancy()
	var within []effective.Date
	for _, d := range changes {
		if occupancy.Contains(d) {
			within = append(within, d)
		}
	}
	return within, nil
}
