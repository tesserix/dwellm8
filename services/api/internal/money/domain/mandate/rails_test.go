package mandate_test

import (
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/mandate"
)

// The caps as they stand on the review date in docs/payment-rails.md. They are
// written here as *test data*, not as defaults in the package, because the
// moment a threshold has a default in code somebody will rely on it and the
// rule table stops being the source of truth.
func caps() mandate.Caps {
	return mandate.Caps{
		AFAFreeCeiling: 1_500_000,       // ₹15,000
		UPIAutopayMax:  10_000_000,      // ₹1,00,000
		ENACHMax:       1_000_000_000_0, // ₹1,00,00,000
		Source:         "rail_rules/2026-07-31",
		EffectiveFrom:  time.Date(2026, 7, 31, 0, 0, 0, 0, time.UTC),
	}
}

func everyBank() mandate.BankSupport {
	return mandate.BankSupport{UPIAutopay: true, ENACH: true, PhysicalNACH: true}
}

// The issue's primary scenario: a rent of 45,000 and a rent of 12,000, each
// routed to a rail that can actually carry it.
func TestTheTwoRentsFromTheSpike(t *testing.T) {
	twelve, err := mandate.Select(1_200_000, caps(), everyBank())
	if err != nil {
		t.Fatalf("routing ₹12,000: %v", err)
	}
	if twelve.Rail != mandate.RailUPIAutopay || !twelve.Unattended {
		t.Errorf("₹12,000 routed to %+v", twelve)
	}

	fortyFive, err := mandate.Select(4_500_000, caps(), everyBank())
	if err != nil {
		t.Fatalf("routing ₹45,000: %v", err)
	}
	if fortyFive.Rail != mandate.RailENACH {
		t.Errorf("₹45,000 routed to %s — UPI Autopay reaches ₹1,00,000 and would ask the tenant "+
			"for a PIN every month, which is a mandate's cost with a manual payment's failure rate",
			fortyFive.Rail)
	}
	if !fortyFive.Unattended {
		t.Error("eNACH was reported as attended")
	}

	// Both decisions carry the row that made them, so a routing argued about in
	// a year can be explained with the numbers that applied at the time.
	for _, s := range []mandate.Selection{twelve, fortyFive} {
		if s.Source != caps().Source {
			t.Errorf("a routing decision with no rule-table source: %+v", s)
		}
	}
}

func TestTheBoundary(t *testing.T) {
	c := caps()
	at, err := mandate.Select(c.AFAFreeCeiling, c, everyBank())
	if err != nil || at.Rail != mandate.RailUPIAutopay {
		t.Errorf("exactly at the AFA-free ceiling routed to (%+v, %v)", at, err)
	}
	just, err := mandate.Select(c.AFAFreeCeiling+1, c, everyBank())
	if err != nil || just.Rail != mandate.RailENACH {
		t.Errorf("one paisa above the ceiling routed to (%+v, %v)", just, err)
	}
}

// The issue's edge case: a tenant whose bank does not support Autopay falls
// back cleanly to collect requests, and the fallback is never offline.
func TestABankWithoutAutopayFallsBackToCollectAndNeverToOffline(t *testing.T) {
	sel, err := mandate.Select(1_200_000, caps(), mandate.BankSupport{ENACH: true, PhysicalNACH: true})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if sel.Rail != "" {
		t.Errorf("a mandate was registered on a bank that cannot hold one: %s", sel.Rail)
	}
	if sel.Unattended {
		t.Error("a collect request was described as unattended")
	}
	if sel.Fallback != collect.MethodUPICollect {
		t.Errorf("fallback = %s", sel.Fallback)
	}
	if sel.Reason != mandate.ReasonNoAutopayAtBank {
		t.Errorf("reason = %s", sel.Reason)
	}

	// Every fallback this function can produce, and none of them offline: ADR-0011
	// §6, degrading to offline is a decision with a human in it.
	for _, tc := range []struct {
		rent domain.Minor
		bank mandate.BankSupport
	}{
		{1_200_000, mandate.BankSupport{}},
		{4_500_000, mandate.BankSupport{}},
		{200_000_000_00, everyBank()},
	} {
		s, err := mandate.Select(tc.rent, caps(), tc.bank)
		if err != nil {
			t.Fatalf("Select(%s): %v", tc.rent, err)
		}
		if s.Fallback.IsOffline() {
			t.Errorf("rent %s fell back to %s", tc.rent, s.Fallback)
		}
	}
}

// Physical NACH is slow and involves paper, and it is the only rail that
// reaches a tenant whose bank does no eNACH. Dropping it would make that tenant
// uncollectable by any standing authority.
func TestPhysicalNACHCatchesTheBankThatDoesNothingElse(t *testing.T) {
	sel, err := mandate.Select(4_500_000, caps(), mandate.BankSupport{PhysicalNACH: true})
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if sel.Rail != mandate.RailPhysicalNACH || !sel.Unattended {
		t.Errorf("routed to %+v", sel)
	}
	if sel.Reason != mandate.ReasonNoENACHAtBank {
		t.Errorf("reason = %s", sel.Reason)
	}
}

func TestAboveEveryRail(t *testing.T) {
	c := caps()
	sel, err := mandate.Select(c.ENACHMax+1, c, everyBank())
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if sel.Rail != "" || sel.Reason != mandate.ReasonAboveEveryRail {
		t.Errorf("above the NACH ceiling routed to %+v", sel)
	}
}

// The second acceptance criterion: the regulator moves the ceiling, the table
// gets a row, and no code changes. Asserted by routing the same rent through
// two rule rows and getting two different answers.
func TestARegulatoryChangeIsARowAndNotARelease(t *testing.T) {
	rent := domain.Minor(2_500_000) // ₹25,000

	today, err := mandate.Select(rent, caps(), everyBank())
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if today.Rail != mandate.RailENACH {
		t.Fatalf("under today's ceiling ₹25,000 should be eNACH, got %s", today.Rail)
	}

	raised := caps()
	raised.AFAFreeCeiling = 5_000_000 // the regulator raises it to ₹50,000
	raised.Source = "rail_rules/2027-04-01"

	after, err := mandate.Select(rent, raised, everyBank())
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if after.Rail != mandate.RailUPIAutopay {
		t.Errorf("with the ceiling raised, ₹25,000 should be UPI Autopay, got %s", after.Rail)
	}
	if after.Source == today.Source {
		t.Error("two different rules produced the same source, so neither decision can be explained")
	}
}

// A rule-table row that cannot be true must fail where it is loaded. The
// alternative is every tenancy quietly routing to the fallback, which looks
// like a product problem and is a configuration one.
func TestAnImpossibleRuleRowIsRefused(t *testing.T) {
	for name, mangle := range map[string]func(*mandate.Caps){
		"no AFA-free ceiling": func(c *mandate.Caps) { c.AFAFreeCeiling = 0 },
		"UPI below AFA-free":  func(c *mandate.Caps) { c.UPIAutopayMax = c.AFAFreeCeiling - 1 },
		"NACH below UPI":      func(c *mandate.Caps) { c.ENACHMax = c.UPIAutopayMax - 1 },
		"no source":           func(c *mandate.Caps) { c.Source = "" },
	} {
		c := caps()
		mangle(&c)
		if _, err := mandate.Select(1_200_000, c, everyBank()); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := mandate.Select(0, caps(), everyBank()); err == nil {
		t.Error("a rent of zero was routed")
	}
}

// The open question from the spike, encoded so the answer has somewhere to
// land: headroom for a rent escalation is only free below the AFA-free ceiling.
func TestEscalationHeadroomIsFreeOnlyBelowTheCeiling(t *testing.T) {
	c := caps()
	rent := domain.Minor(1_200_000) // ₹12,000

	if mandate.RequiresAFA(rent, c) {
		t.Error("₹12,000 was reported as needing AFA")
	}
	// Ten per cent of headroom keeps it AFA-free…
	if mandate.RequiresAFA(rent+rent/10, c) {
		t.Error("₹13,200 was reported as needing AFA")
	}
	// …and a ceiling set for a doubling does not.
	if !mandate.RequiresAFA(rent*2, c) {
		t.Error("₹24,000 was reported as AFA-free")
	}
}
