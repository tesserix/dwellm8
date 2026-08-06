package domain_test

import (
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// Splitting one collection between the three parties that claim on it (#270).
// The arithmetic has one job the ledger depends on: the legs add back to the
// collection exactly, whatever the rates round to.

func terms() domain.SettlementTerms {
	return domain.SettlementTerms{
		ManagementRate:  800, // 8%
		TDSRate:         200, // 2%, §194-I once the gate is acknowledged
		TDSAcknowledged: true,
	}
}

// The acceptance case on #270: ₹32,000 collected, 8% management, 2% TDS.
func TestACollectionSplitsIntoLegsThatAddBackExactly(t *testing.T) {
	fee, err := domain.FeeSchedule{Rate: 299, TaxRate: 1800, RuleID: "r1"}.Charge(3200000)
	if err != nil {
		t.Fatalf("pricing the platform fee: %v", err)
	}

	s, err := terms().Split(fee)
	if err != nil {
		t.Fatalf("splitting: %v", err)
	}

	if s.Management != 256000 {
		t.Fatalf("management leg = %s; want 8%% of ₹32,000", s.Management)
	}
	if s.TDS != 64000 {
		t.Fatalf("TDS leg = %s; want 2%% of ₹32,000", s.TDS)
	}
	if s.Platform != fee.Retained {
		t.Fatalf("platform leg = %s; want the retained fee %s", s.Platform, fee.Retained)
	}
	if got := s.Platform + s.Management + s.TDS + s.Owner; got != 3200000 {
		t.Fatalf("the legs make %s; want the whole ₹32,000", got)
	}
}

// Every rate rounds; the owner's leg is what remains rather than a fourth
// percentage, so a half-paisa cannot go missing between the four.
func TestNoRoundingDriftAtAwkwardAmounts(t *testing.T) {
	for _, gross := range []domain.Minor{1, 333, 100003, 2749933, 999999999} {
		fee, err := domain.FeeSchedule{Rate: 299, TaxRate: 1800}.Charge(gross)
		if err != nil {
			t.Fatalf("%s: pricing: %v", gross, err)
		}
		s, err := terms().Split(fee)
		if err != nil {
			t.Fatalf("%s: splitting: %v", gross, err)
		}
		if got := s.Platform + s.Management + s.TDS + s.Owner; got != gross {
			t.Fatalf("%s split into legs making %s", gross, got)
		}
		if s.Owner < 0 {
			t.Fatalf("%s left the owner %s", gross, s.Owner)
		}
	}
}

// ADR-0024 §6: no deduction until the tenancy's facts are acknowledged. The
// alternative is deducting under a section nobody chose.
func TestNothingIsWithheldUntilTheTaxGateIsAcknowledged(t *testing.T) {
	fee, _ := domain.FeeSchedule{Rate: 299, TaxRate: 1800}.Charge(3200000)

	tm := terms()
	tm.TDSAcknowledged = false
	if _, err := tm.Split(fee); !errors.Is(err, domain.ErrSettlement) {
		t.Fatalf("a deduction without the gate = %v; want ErrSettlement", err)
	}

	tm.TDSRate = 0
	s, err := tm.Split(fee)
	if err != nil {
		t.Fatalf("no deduction due, gate not needed: %v", err)
	}
	if s.TDS != 0 {
		t.Fatalf("withheld %s with no rate", s.TDS)
	}
}

// A letting fee is one-off and belongs to the manager, so it comes out of the
// same collection rather than arriving as a second invoice.
func TestTheLettingFeeIsPartOfTheManagersLeg(t *testing.T) {
	fee, _ := domain.FeeSchedule{Rate: 299, TaxRate: 1800}.Charge(3200000)

	tm := terms()
	tm.LettingFee = 500000
	s, err := tm.Split(fee)
	if err != nil {
		t.Fatalf("splitting: %v", err)
	}
	if s.Management != 256000+500000 {
		t.Fatalf("management leg = %s; want the fee and the letting charge", s.Management)
	}
	if got := s.Platform + s.Management + s.TDS + s.Owner; got != 3200000 {
		t.Fatalf("the legs make %s; want the whole collection", got)
	}
}

// Claims larger than the collection are refused rather than settled from the
// next tenant's rent.
func TestClaimsBeyondTheCollectionAreRefused(t *testing.T) {
	fee, _ := domain.FeeSchedule{Rate: 299, TaxRate: 1800}.Charge(100000)

	tm := terms()
	tm.LettingFee = 200000
	if _, err := tm.Split(fee); !errors.Is(err, domain.ErrSettlement) {
		t.Fatalf("an over-claimed collection = %v; want ErrSettlement", err)
	}
}
