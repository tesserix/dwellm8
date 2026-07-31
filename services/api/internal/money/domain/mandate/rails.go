package mandate

import (
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// The rail-selection rule from the #13 spike, executable.
//
// Every threshold it needs is an argument. That is the whole point: the caps
// are NPCI and RBI parameters that change without asking us, they live in a
// versioned rule table with an owner and a review date, and a build that
// hard-codes 15,000 is a build that needs shipping the day the regulator moves.
// What is in Go is the *rule*; what is in the table is the *numbers*.
//
// docs/payment-rails.md §3 is the prose. This is the same thing where it can be
// tested.

// Caps is one row of the rail rule table.
type Caps struct {
	// AFAFreeCeiling is the per-debit amount up to which no additional factor of
	// authentication is required. Rent is not in the exempt category set
	// (insurance, mutual-fund SIPs, credit-card bills, the lending MCCs), so for
	// this platform it is the ordinary ceiling and not the raised one.
	AFAFreeCeiling domain.Minor
	// UPIAutopayMax is the ceiling UPI Autopay reaches at all, above the AFA-free
	// one. Between the two, a debit still works and the payer must enter a PIN.
	UPIAutopayMax domain.Minor
	// ENACHMax is the NACH ceiling.
	ENACHMax domain.Minor

	// Source names the rule-table row, so a routing decision recorded against a
	// tenancy can be explained a year later when the numbers have changed.
	Source        string
	EffectiveFrom time.Time
}

// Valid rejects a rule-table row that cannot be true. A misconfigured row must
// fail where it is loaded, not route every tenancy to the fallback and look
// like a product problem.
func (c Caps) Valid() error {
	switch {
	case c.AFAFreeCeiling <= 0:
		return fmt.Errorf("money: a rail rule with an AFA-free ceiling of %s", c.AFAFreeCeiling)
	case c.UPIAutopayMax < c.AFAFreeCeiling:
		return fmt.Errorf("money: a rail rule whose UPI ceiling %s is below its AFA-free ceiling %s",
			c.UPIAutopayMax, c.AFAFreeCeiling)
	case c.ENACHMax < c.UPIAutopayMax:
		return fmt.Errorf("money: a rail rule whose NACH ceiling %s is below its UPI ceiling %s",
			c.ENACHMax, c.UPIAutopayMax)
	case c.Source == "":
		return fmt.Errorf("money: a rail rule with no source cannot be explained to the owner it routed")
	}
	return nil
}

// BankSupport is what the payer's bank can actually do. Coverage is not uniform
// and a rule that assumes it is will route a tenant to a rail their bank does
// not offer, which surfaces as a mandate that never activates.
type BankSupport struct {
	UPIAutopay   bool
	ENACH        bool
	PhysicalNACH bool
}

// Reason is why a rail was chosen, kept as data so it can be stored against the
// tenancy and shown to whoever asks.
type Reason string

const (
	ReasonWithinAFAFree   Reason = "within_afa_free_ceiling"
	ReasonNoAutopayAtBank Reason = "bank_does_not_support_upi_autopay"
	ReasonAboveAFAFree    Reason = "above_afa_free_ceiling_so_upi_would_need_a_pin_every_month"
	ReasonNoENACHAtBank   Reason = "bank_does_not_support_enach"
	ReasonAboveEveryRail  Reason = "above_every_recurring_rail"
	ReasonNoRecurringRail Reason = "no_recurring_rail_available_at_this_bank"
)

// Selection is the routing decision.
type Selection struct {
	// Rail is empty when no standing authority can be registered at all. That is
	// a legitimate outcome and it is not an error: the tenancy collects by
	// request instead, which is what most Indian rent does today.
	Rail Rail
	// Unattended reports whether the debit happens with nobody present. It is the
	// property the product sells, and it is false for a rail that technically
	// carries the amount but asks the tenant for a PIN every month.
	Unattended bool
	// Fallback is the collection method to use when Rail is empty. Never an
	// offline method by default — ADR-0011 §6, degrading to offline is a decision
	// a human makes, not one a routing rule makes for them.
	Fallback collect.Method
	Reason   Reason
	Source   string
}

// Select routes a monthly rent to the rail that can actually carry it.
//
// The rule that is easy to get wrong is the one in the middle. UPI Autopay
// reaches well above the AFA-free ceiling, so the tempting answer for a rent of
// 45,000 is "UPI, it fits". It does fit, and every single debit then requires
// the tenant to enter a UPI PIN within thirty minutes of a collect request.
// That is the onboarding cost of a mandate paired with the monthly failure rate
// of a manual payment — the worst combination available — so above the AFA-free
// ceiling this returns eNACH and not UPI.
func Select(rent domain.Minor, caps Caps, bank BankSupport) (Selection, error) {
	if err := caps.Valid(); err != nil {
		return Selection{}, err
	}
	if rent <= 0 {
		return Selection{}, fmt.Errorf("money: a rent of %s cannot be routed", rent)
	}
	if err := rent.Valid(); err != nil {
		return Selection{}, err
	}

	sel := Selection{Source: caps.Source}

	if rent <= caps.AFAFreeCeiling {
		if bank.UPIAutopay {
			sel.Rail, sel.Unattended, sel.Reason = RailUPIAutopay, true, ReasonWithinAFAFree
			return sel, nil
		}
		// The tenant is not unbankable, they are just not on a rail. A scheduled
		// collect request is honest about what it is; calling it autopay is not.
		sel.Fallback, sel.Reason = collect.MethodUPICollect, ReasonNoAutopayAtBank
		return sel, nil
	}

	if rent <= caps.ENACHMax {
		if bank.ENACH {
			sel.Rail, sel.Unattended, sel.Reason = RailENACH, true, ReasonAboveAFAFree
			return sel, nil
		}
		if bank.PhysicalNACH {
			// Slow — days, and a signed form somebody has to handle — but genuinely
			// unattended once it is live, which is the whole point.
			sel.Rail, sel.Unattended, sel.Reason = RailPhysicalNACH, true, ReasonNoENACHAtBank
			return sel, nil
		}
		sel.Fallback, sel.Reason = collect.MethodUPICollect, ReasonNoRecurringRail
		return sel, nil
	}

	// Above every retail recurring rail. Commercial leases land here, and they
	// are collected by transfer against an invoice like they always were.
	sel.Fallback, sel.Reason = collect.MethodUPIIntent, ReasonAboveEveryRail
	return sel, nil
}

// RequiresAFA reports whether a debit of this amount needs the payer present.
// It exists so the scheduler can refuse to describe such a debit as automatic,
// and so a mandate registered with escalation headroom can be checked against
// the ceiling before the headroom silently costs the tenancy its autopay.
func RequiresAFA(amount domain.Minor, caps Caps) bool {
	return amount > caps.AFAFreeCeiling
}
