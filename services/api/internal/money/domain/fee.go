package domain

import (
	"errors"
	"fmt"
)

// The platform fee and the split that collects it. Issue #234, ADR-0031.
//
// Two rules shape the arithmetic: the payer is never surcharged, and Dwellm8
// never holds the money (india-compliance.md §9.4). So the fee comes out of the
// bearer's leg, the aggregator retains it at capture, and the two legs must add
// back to the gross exactly — that is what the settlement file reconciles on.

// ErrFee is a fee that cannot be computed, or a schedule that cannot price one.
var ErrFee = errors.New("money: the platform fee cannot be applied")

// FeeSchedule is the price of the platform as of a moment, resolved from an
// effective-dated rule so a change is a row rather than a release.
type FeeSchedule struct {
	Rate    Rate // headline, in basis points: 299 is 2.99%
	TaxRate Rate // GST on the fee — Dwellm8 supplies a service to the bearer
	// TaxInclusive says whether Rate already contains the tax. Worth about
	// fifteen per cent of revenue, so it is stored rather than assumed.
	TaxInclusive bool
	MinMinor     Minor
	MaxMinor     Minor // zero means uncapped
	RuleID       string
}

// Fee is one collection, decomposed. Every field is positive.
type Fee struct {
	Gross    Minor // what the payer paid, unchanged by any of this
	Net      Minor // Dwellm8's income, before tax
	Tax      Minor // GST on the fee, owed onward
	Retained Minor // Net + Tax: what the aggregator holds back
	Vendor   Minor // Gross − Retained: what settles to the bearer
	RuleID   string
}

// Zero reports whether the collection yields no fee at all.
func (f Fee) Zero() bool { return f.Retained == 0 }

// Validate is the invariant the settlement file is reconciled on. A paisa lost
// between the legs is a paisa nothing can account for, on every payment.
func (f Fee) Validate() error {
	switch {
	case f.Gross <= 0:
		return fmt.Errorf("%w: a gross of %s", ErrFee, f.Gross)
	case f.Net < 0 || f.Tax < 0:
		return fmt.Errorf("%w: a negative component (%s fee, %s tax)", ErrFee, f.Net, f.Tax)
	case f.Net+f.Tax != f.Retained:
		return fmt.Errorf("%w: %s fee and %s tax do not make %s retained", ErrFee, f.Net, f.Tax, f.Retained)
	case f.Retained+f.Vendor != f.Gross:
		return fmt.Errorf("%w: %s retained and %s to the vendor do not make %s",
			ErrFee, f.Retained, f.Vendor, f.Gross)
	}
	return nil
}

// Charge prices one collection.
func (s FeeSchedule) Charge(gross Minor) (Fee, error) {
	if err := s.validate(gross); err != nil {
		return Fee{}, err
	}

	retained, err := s.retain(gross)
	if err != nil {
		return Fee{}, err
	}
	if retained > gross {
		return Fee{}, fmt.Errorf("%w: %s retained from %s leaves the bearer nothing", ErrFee, retained, gross)
	}

	// Derived from the bound, not the rate, so a capped fee still decomposes.
	net, tax, err := unbundle(retained, s.TaxRate)
	if err != nil {
		return Fee{}, err
	}

	// The vendor leg is subtraction, never a second rate: two rounded
	// percentages miss the whole by a paisa and a subtraction cannot.
	f := Fee{Gross: gross, Net: net, Tax: tax, Retained: retained, Vendor: gross - retained, RuleID: s.RuleID}
	return f, f.Validate()
}

func (s FeeSchedule) validate(gross Minor) error {
	switch {
	case gross <= 0:
		return fmt.Errorf("%w: a collection of %s", ErrFee, gross)
	case s.Rate < 0 || s.TaxRate < 0:
		return fmt.Errorf("%w: a rate of %s and a tax of %s", ErrFee, s.Rate, s.TaxRate)
	case s.MaxMinor > 0 && s.MaxMinor < s.MinMinor:
		return fmt.Errorf("%w: a cap of %s below the floor of %s", ErrFee, s.MaxMinor, s.MinMinor)
	}
	return nil
}

// retain applies the rate, adds tax where the rate excludes it, and bounds it.
func (s FeeSchedule) retain(gross Minor) (Minor, error) {
	retained, err := s.Rate.Of(gross)
	if err != nil {
		return 0, err
	}
	if !s.TaxInclusive {
		tax, err := s.TaxRate.Of(retained)
		if err != nil {
			return 0, err
		}
		retained += tax
	}
	return bound(retained, s.MinMinor, s.MaxMinor), nil
}

// unbundle splits a tax-inclusive amount. The tax is computed and the net is the
// remainder — rounding both leaves the pair a paisa short of what they decompose.
func unbundle(inclusive Minor, taxRate Rate) (net, tax Minor, err error) {
	if taxRate == 0 {
		return inclusive, 0, nil
	}
	tax, err = mulDivRound(inclusive, int64(taxRate), BasisPointsPerUnit+int64(taxRate))
	if err != nil {
		return 0, 0, err
	}
	return inclusive - tax, tax, nil
}

// bound applies the floor and the cap. A zero cap is no cap.
func bound(amount, min, max Minor) Minor {
	if amount < min {
		amount = min
	}
	if max > 0 && amount > max {
		amount = max
	}
	return amount
}
