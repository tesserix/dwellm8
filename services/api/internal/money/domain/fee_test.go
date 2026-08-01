package domain_test

import (
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// The platform fee, without a database. Issue #234.
//
// Every case here is arithmetic, and the property that matters in all of them is
// the same: the two settlement legs add back to the gross. A paisa lost between
// them is a paisa the reconciliation cannot account for, on every payment.

func schedule() domain.FeeSchedule {
	return domain.FeeSchedule{Rate: 299, TaxRate: 1800, RuleID: "rule-1"}
}

// The headline case, and the one the issue quotes. 2.99% of ₹25,000 is ₹747.50
// exactly; GST at 18% on that is ₹134.55; the owner settles ₹24,117.95.
func TestTheHeadlineFee(t *testing.T) {
	got, err := schedule().Charge(2_500_000)
	if err != nil {
		t.Fatalf("charging: %v", err)
	}
	for _, c := range []struct {
		what string
		got  domain.Minor
		want domain.Minor
	}{
		{"fee", got.Net, 74_750},
		{"tax", got.Tax, 13_455},
		{"retained", got.Retained, 88_205},
		{"vendor", got.Vendor, 2_411_795},
	} {
		if c.got != c.want {
			t.Errorf("%s = %s, want %s", c.what, c.got, c.want)
		}
	}
}

// The invariant, over a wide spread of amounts and rates. This is the test that
// would catch a second rounding creeping into the vendor leg.
func TestTheLegsAlwaysAddBackToTheGross(t *testing.T) {
	rates := []domain.Rate{0, 1, 50, 299, 390, 1000, 9999}
	taxes := []domain.Rate{0, 500, 1200, 1800}
	amounts := []domain.Minor{1, 7, 99, 100, 333, 2_500_000, 2_500_001, 999_999_999}

	for _, r := range rates {
		for _, tax := range taxes {
			for _, amt := range amounts {
				s := domain.FeeSchedule{Rate: r, TaxRate: tax}
				f, err := s.Charge(amt)
				if err != nil {
					t.Fatalf("rate %s tax %s on %s: %v", r, tax, amt, err)
				}
				if f.Retained+f.Vendor != f.Gross {
					t.Fatalf("rate %s tax %s on %s: %s + %s != %s",
						r, tax, amt, f.Retained, f.Vendor, f.Gross)
				}
				if f.Net+f.Tax != f.Retained {
					t.Fatalf("rate %s tax %s on %s: fee %s + tax %s != retained %s",
						r, tax, amt, f.Net, f.Tax, f.Retained)
				}
			}
		}
	}
}

// Tax-inclusive is the fifteen-per-cent decision. The same 2.99% retains the
// same amount and yields materially less income, and the difference is the
// government's either way — so it must be a stored decision, not a habit.
func TestTaxInclusiveIsADifferentFee(t *testing.T) {
	exclusive, err := domain.FeeSchedule{Rate: 299, TaxRate: 1800}.Charge(2_500_000)
	if err != nil {
		t.Fatalf("exclusive: %v", err)
	}
	inclusive, err := domain.FeeSchedule{Rate: 299, TaxRate: 1800, TaxInclusive: true}.Charge(2_500_000)
	if err != nil {
		t.Fatalf("inclusive: %v", err)
	}

	if inclusive.Retained != 74_750 {
		t.Errorf("a tax-inclusive 2.99%% retained %s, want the flat 74750", inclusive.Retained)
	}
	if inclusive.Net != 63_347 {
		t.Errorf("a tax-inclusive 2.99%% earned %s, want 63347", inclusive.Net)
	}
	if inclusive.Net >= exclusive.Net {
		t.Errorf("tax-inclusive earned %s and exclusive earned %s — inclusive must be less",
			inclusive.Net, exclusive.Net)
	}
}

// A cap still decomposes. This is where a naive implementation breaks: it caps
// the total and leaves a fee and a tax computed from the uncapped rate, which no
// longer sum to what was retained.
func TestACapStillDecomposesExactly(t *testing.T) {
	s := domain.FeeSchedule{Rate: 299, TaxRate: 1800, MaxMinor: 50_000}
	f, err := s.Charge(2_500_000)
	if err != nil {
		t.Fatalf("charging: %v", err)
	}
	if f.Retained != 50_000 {
		t.Fatalf("retained %s, want the cap of 50000", f.Retained)
	}
	if f.Net+f.Tax != f.Retained {
		t.Fatalf("a capped fee decomposed to %s + %s, which is not %s", f.Net, f.Tax, f.Retained)
	}
	if f.Vendor != 2_450_000 {
		t.Fatalf("vendor leg %s, want 2450000", f.Vendor)
	}
}

func TestAFloorLiftsASmallCollection(t *testing.T) {
	s := domain.FeeSchedule{Rate: 299, TaxRate: 1800, MinMinor: 5_000}
	f, err := s.Charge(10_000) // ₹100, on which 2.99% is ₹2.99
	if err != nil {
		t.Fatalf("charging: %v", err)
	}
	if f.Retained != 5_000 {
		t.Fatalf("retained %s, want the floor of 5000", f.Retained)
	}
	if f.Retained+f.Vendor != f.Gross {
		t.Fatalf("the legs stopped adding up under a floor")
	}
}

// A fee that would take the whole collection is a pricing mistake, and the
// bearer settling nothing is not a state to arrive at quietly.
func TestAFeeLargerThanTheCollectionIsRefused(t *testing.T) {
	s := domain.FeeSchedule{Rate: 299, TaxRate: 1800, MinMinor: 20_000}
	if _, err := s.Charge(10_000); !errors.Is(err, domain.ErrFee) {
		t.Fatalf("a fee floor above the collection was accepted: %v", err)
	}
}

// A rate of nothing is a legitimate arrangement — a waived fee, a promotional
// period — and must produce a whole vendor leg rather than an error.
func TestAZeroRateYieldsNoFeeAndTheWholeCollection(t *testing.T) {
	f, err := domain.FeeSchedule{Rate: 0, TaxRate: 1800}.Charge(2_500_000)
	if err != nil {
		t.Fatalf("charging at zero: %v", err)
	}
	if !f.Zero() || f.Vendor != 2_500_000 {
		t.Fatalf("a zero rate produced %+v", f)
	}
}

// A collection so small the rate rounds to nothing. The fee is zero, the vendor
// gets everything, and nothing is refused — this is a ₹1 test payment, not an
// error.
func TestACollectionTooSmallToChargeIsNotAnError(t *testing.T) {
	f, err := domain.FeeSchedule{Rate: 299, TaxRate: 1800}.Charge(1)
	if err != nil {
		t.Fatalf("charging a paisa: %v", err)
	}
	if f.Vendor != 1 {
		t.Fatalf("a one-paisa collection settled %s to the vendor", f.Vendor)
	}
}

func TestAnEmptyCollectionIsRefused(t *testing.T) {
	for _, amount := range []domain.Minor{0, -1, -2_500_000} {
		if _, err := schedule().Charge(amount); !errors.Is(err, domain.ErrFee) {
			t.Errorf("a collection of %s was priced", amount)
		}
	}
}

// A cap below the floor is a rule nobody can satisfy, and it is caught when the
// fee is charged rather than when somebody notices the number.
func TestACapBelowTheFloorIsRefused(t *testing.T) {
	s := domain.FeeSchedule{Rate: 299, TaxRate: 1800, MinMinor: 10_000, MaxMinor: 5_000}
	if _, err := s.Charge(2_500_000); !errors.Is(err, domain.ErrFee) {
		t.Fatalf("a cap below the floor priced a fee")
	}
}

// The rule that priced the fee travels with it, so a posting can be traced to
// the row rather than to a number somebody remembers.
func TestTheRuleTravelsWithTheFee(t *testing.T) {
	f, err := schedule().Charge(2_500_000)
	if err != nil {
		t.Fatalf("charging: %v", err)
	}
	if f.RuleID != "rule-1" {
		t.Fatalf("the fee came back priced by %q", f.RuleID)
	}
}
