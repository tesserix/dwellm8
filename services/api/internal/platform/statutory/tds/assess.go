package tds

import (
	"errors"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory"
)

// Matrix is the decision matrix bound to a registry.
//
// The registry is where the numbers live and this is where the choice lives, and
// they are separate because they change for different reasons: a Budget moves a
// threshold and nothing here changes, while a new section would change this and
// leave the rows alone.
type Matrix struct{ rules *statutory.Table }

// New binds the matrix to a loaded registry.
func New(rules *statutory.Table) (*Matrix, error) {
	if rules == nil {
		return nil, errors.New("tds: a matrix with no rule registry would have to invent its own rates")
	}
	return &Matrix{rules: rules}, nil
}

// Rent is what the threshold is tested against.
//
// Both figures, because which one matters is the path's business and the caller
// does not know the path yet: section 194-I tests the year's aggregate to this
// landlord and section 194-IB tests a single month, and asking the caller to work
// out which to supply is asking them to reimplement the matrix.
type Rent struct {
	// MonthlyMinor is the rent for one month, in paise.
	MonthlyMinor int64
	// AnnualMinor is the aggregate paid or payable to this landlord in the financial
	// year, in paise. Aggregate to the *payee*, not to the property: two flats let by
	// one owner to one tenant are one threshold, which is the classic under-deduction.
	AnnualMinor int64
}

// Assessment is one answer: which section, at what rate, whether the threshold is
// crossed, and which rows said so.
//
// Rule ids are carried so that whatever is computed from this can be recomputed
// years later against the same rows — the same reason ADR-0023 has resolutions
// carry them, and the reason a deduction is defensible rather than merely
// reproducible.
type Assessment struct {
	Path  Path
	Facts Facts
	On    effective.Date

	// RateBps is the rate in basis points: ten per cent is 1000.
	RateBps    int
	RateRuleID string

	// ThresholdMinor is the figure the basis is tested against, in paise, and
	// ThresholdRuleID the row it came from. Both zero on a path with no threshold.
	ThresholdMinor  int64
	ThresholdRuleID string

	// Crossed is whether the threshold is exceeded, so whether tax is deductible at
	// all. Always true where the basis is none.
	Crossed bool

	// Verification and Enforcement are carried up from the weaker of the two rows, so
	// a caller can tell a verified deduction from one resting on a row that still
	// needs a bare-act check. ADR-0023 §4.
	Verification statutory.Verification
	Enforcement  statutory.Enforcement

	// Because is the sentence a user is shown and an auditor is given.
	Because string
}

// Deductible reports whether tax is to be deducted on this payment.
func (a Assessment) Deductible() bool { return a.Crossed }

// Assess selects the section and resolves its numbers as of a date.
//
// The date is the payment or credit date, whichever is earlier, and it is an
// argument for the reason every as-of question in this codebase takes one: a
// deduction for March recomputed in November has to resolve March's rate. The
// threshold that rose on 1 April 2025 is the live example — a tenancy that spans it
// is assessed on either side of it with different numbers, from the same code.
//
// It fails rather than defaults when the registry has no rate. Section 195 is where
// that bites and where it is meant to: the rate is the Act's, or a treaty's, read
// with the landlord's tax residency certificate, and a platform that filled in a
// plausible number would be wrong with authority.
func (m *Matrix) Assess(f Facts, rent Rent, on effective.Date) (Assessment, error) {
	p, err := Select(f)
	if err != nil {
		return Assessment{}, err
	}
	if on.Zero() {
		return Assessment{}, errors.New("tds: an assessment must say which date it is asking about")
	}
	if rent.MonthlyMinor < 0 || rent.AnnualMinor < 0 {
		return Assessment{}, fmt.Errorf("tds: negative rent — %d a month, %d a year",
			rent.MonthlyMinor, rent.AnnualMinor)
	}

	// Income tax is union legislation: there is no state override to look for, and
	// resolving against a state would invite somebody to add one.
	rate, err := m.rules.Resolve(ruleTypes.Rate, statutory.National, p.RateKey, on)
	if err != nil {
		return Assessment{}, fmt.Errorf("tds: section %s has no rate to deduct at: %w", p.Section, err)
	}
	rateBps, err := rate.Rule.Rate()
	if err != nil {
		return Assessment{}, err
	}

	a := Assessment{
		Path: p, Facts: f, On: on,
		RateBps: rateBps, RateRuleID: rate.Rule.ID,
		Verification: rate.Rule.Verification,
		Enforcement:  rate.Rule.Enforcement,
	}

	if p.Basis == BasisNone {
		a.Crossed = true
		a.Because = fmt.Sprintf("Section %s: the landlord is a non-resident, so tax is deducted "+
			"from the first rupee. There is no threshold to cross.", p.Section)
		return a, nil
	}

	threshold, err := m.rules.Resolve(ruleTypes.Threshold, statutory.National, p.ThresholdKey, on)
	if err != nil {
		return Assessment{}, fmt.Errorf("tds: section %s has no threshold to test against: %w", p.Section, err)
	}
	limit, err := threshold.Rule.Amount()
	if err != nil {
		return Assessment{}, err
	}
	a.ThresholdMinor, a.ThresholdRuleID = limit, threshold.Rule.ID
	a.Verification = weaker(a.Verification, threshold.Rule.Verification)
	a.Enforcement = weaker2(a.Enforcement, threshold.Rule.Enforcement)

	var tested int64
	switch p.Basis {
	case BasisAnnual:
		tested = rent.AnnualMinor
	case BasisMonthly:
		tested = rent.MonthlyMinor
	default:
		return Assessment{}, fmt.Errorf("tds: section %s has an unknown threshold basis %q", p.Section, p.Basis)
	}

	// "Exceeds", as both provisos say. Rent exactly at the threshold is below it, and
	// the off-by-one in the other direction deducts tax nobody owed.
	a.Crossed = tested > limit
	if a.Crossed {
		a.Because = fmt.Sprintf("Section %s: %s rent of %s exceeds the threshold of %s in force on %s.",
			p.Section, p.Basis, paise(tested), paise(limit), on)
	} else {
		a.Because = fmt.Sprintf("Section %s: %s rent of %s does not exceed the threshold of %s in "+
			"force on %s, so nothing is deducted.", p.Section, p.Basis, paise(tested), paise(limit), on)
	}
	return a, nil
}

// Payee is one landlord's share of the rent.
//
// Residency is per payee, because joint ownership is where it stops being one fact
// about a lease: a flat owned by a couple, one of whom has moved abroad, is a
// section 194-I payment to one of them and a section 195 payment to the other, in
// the same month, out of the same rent.
type Payee struct {
	Ref          string
	Residency    Residency
	ShareBps     int
	MonthlyMinor int64
	AnnualMinor  int64
}

// ErrShares is an apportionment that does not add up.
var ErrShares = errors.New("tds: the shares do not account for the rent")

// Apportion assesses each co-owner separately.
//
// The threshold is tested per payee, which is the point of the whole method: two
// owners of one flat each receive half the rent, and half may be below a threshold
// the whole is above. That is the correct treatment where the shares are definite
// and ascertainable, and it is also the shape of the most common evasion, so this
// function refuses to do it on anything vaguer than an exact split — shares that
// total ten thousand basis points, amounts that total the rent to the paisa. A
// remainder means somebody rounded, and a rounded apportionment is not an
// ascertainable share.
//
// The rent is not divided here. The caller passes the division it already made,
// because the one place in this product permitted to split money is ADR-0007's
// allocator under internal/money, and a second splitter here would be a second set
// of rounding decisions for the same rupee.
func (m *Matrix) Apportion(f Facts, rent Rent, payees []Payee, on effective.Date) ([]Assessment, error) {
	if len(payees) == 0 {
		return nil, fmt.Errorf("%w: no payees", ErrShares)
	}
	var (
		shares  int
		monthly int64
		annual  int64
		seen    = make(map[string]bool, len(payees))
	)
	for _, p := range payees {
		switch {
		case p.Ref == "":
			return nil, fmt.Errorf("%w: a payee with no identity", ErrShares)
		case seen[p.Ref]:
			return nil, fmt.Errorf("%w: %s appears twice", ErrShares, p.Ref)
		case p.ShareBps <= 0 || p.ShareBps > 10000:
			return nil, fmt.Errorf("%w: %s holds %d basis points", ErrShares, p.Ref, p.ShareBps)
		case p.MonthlyMinor < 0 || p.AnnualMinor < 0:
			return nil, fmt.Errorf("%w: %s is apportioned a negative amount", ErrShares, p.Ref)
		case !p.Residency.Valid():
			return nil, fmt.Errorf("%w: %s has no residency, and a co-owner's residency is not "+
				"inherited from the lease", ErrShares, p.Ref)
		}
		seen[p.Ref] = true
		shares += p.ShareBps
		monthly += p.MonthlyMinor
		annual += p.AnnualMinor
	}
	if shares != 10000 {
		return nil, fmt.Errorf("%w: the shares total %d basis points, not 10000", ErrShares, shares)
	}
	if monthly != rent.MonthlyMinor || annual != rent.AnnualMinor {
		return nil, fmt.Errorf("%w: the apportioned amounts total %s a month and %s a year "+
			"against rent of %s and %s — a share that has been rounded is not ascertainable",
			ErrShares, paise(monthly), paise(annual), paise(rent.MonthlyMinor), paise(rent.AnnualMinor))
	}

	out := make([]Assessment, 0, len(payees))
	for _, p := range payees {
		facts := f
		facts.Residency = p.Residency
		a, err := m.Assess(facts, Rent{MonthlyMinor: p.MonthlyMinor, AnnualMinor: p.AnnualMinor}, on)
		if err != nil {
			return nil, fmt.Errorf("tds: %s: %w", p.Ref, err)
		}
		a.Because = fmt.Sprintf("%s (%s, %d/10000 of the rent)", a.Because, p.Ref, p.ShareBps)
		out = append(out, a)
	}
	return out, nil
}

// weaker returns the less certain of two verification statuses, so an assessment
// resting on one unverified row does not report itself as verified.
func weaker(a, b statutory.Verification) statutory.Verification {
	rank := map[statutory.Verification]int{
		statutory.Conflicting: 0, statutory.Unverified: 1,
		statutory.NeedsBareActCheck: 2, statutory.Verified: 3,
	}
	if rank[b] < rank[a] {
		return b
	}
	return a
}

// weaker2 does the same for enforcement: a deduction cannot block on the strength
// of a rate row if the threshold row is only advisory.
func weaker2(a, b statutory.Enforcement) statutory.Enforcement {
	rank := map[statutory.Enforcement]int{
		statutory.RecordOnly: 0, statutory.Warn: 1, statutory.Block: 2,
	}
	if rank[b] < rank[a] {
		return b
	}
	return a
}

// paise formats minor units as rupees for a message a human reads. Integer
// arithmetic throughout, per ADR-0007 — the decimal point is punctuation here, not
// a float.
func paise(m int64) string {
	sign := ""
	if m < 0 {
		sign, m = "-", -m
	}
	return fmt.Sprintf("%s₹%d.%02d", sign, m/100, m%100)
}
