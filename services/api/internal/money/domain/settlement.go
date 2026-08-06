package domain

import (
	"errors"
	"fmt"
)

// Splitting one collection between the parties that claim on it. Issue #270,
// ADR-0031 for the platform's leg and ADR-0024 for the deduction.
//
// Three claims and a withholding sit on the same rupee: the platform's fee,
// which the aggregator retained at capture; the manager's management and
// letting fees; the tax withheld against the owner's rent; and the owner, who
// gets what is left. The owner's leg is subtraction rather than a fourth
// percentage, for the reason Fee gives: four rounded rates miss the whole.

// ErrSettlement is a collection that cannot be split as instructed.
var ErrSettlement = errors.New("money: the collection cannot be split that way")

// SettlementTerms is what this tenancy's agreement and tax facts say, priced
// against one collection.
type SettlementTerms struct {
	// ManagementRate is the manager's share of the rent collected.
	ManagementRate Rate
	// LettingFee is a one-off on this collection — the letting charge, a
	// renewal fee — and belongs to the manager alongside the management fee.
	LettingFee Minor
	// TDSRate is the rate the decision matrix selected. The matrix does not
	// multiply (ADR-0024 §3); this is where the rate meets an amount.
	TDSRate Rate
	// TDSAcknowledged is the gate: no deduction until the tenancy's tax facts
	// have been acknowledged, because otherwise it is a deduction under a
	// section nobody chose (ADR-0024 §6).
	TDSAcknowledged bool
}

// Split is one collection, decomposed into the instructions that settle
// it. Every field is positive and the four add back to the collection.
type Split struct {
	Gross Minor
	// Platform is what the aggregator retained: Dwellm8 never holds it.
	Platform Minor
	// Management is the manager's fees, management and letting together.
	Management Minor
	// TDS is withheld against the owner's liability, not lost to them — it
	// reaches the owner statement as tax already paid on their behalf.
	TDS   Minor
	Owner Minor
	// RuleID is the fee rule the platform leg was priced under.
	RuleID string
}

// Validate is the invariant reconciliation runs on: the settlement file has to
// account for every paisa the tenant paid.
func (s Split) Validate() error {
	switch {
	case s.Gross <= 0:
		return fmt.Errorf("%w: a collection of %s", ErrSettlement, s.Gross)
	case s.Platform < 0 || s.Management < 0 || s.TDS < 0 || s.Owner < 0:
		return fmt.Errorf("%w: a negative leg (%s platform, %s manager, %s tax, %s owner)",
			ErrSettlement, s.Platform, s.Management, s.TDS, s.Owner)
	case s.Platform+s.Management+s.TDS+s.Owner != s.Gross:
		return fmt.Errorf("%w: %s, %s, %s and %s do not make %s",
			ErrSettlement, s.Platform, s.Management, s.TDS, s.Owner, s.Gross)
	}
	return nil
}

// Split prices one collection against these terms. The platform's leg is taken
// as already priced, because it was retained when the payment was captured and
// re-deriving it here would be a second answer to a settled question.
func (t SettlementTerms) Split(fee Fee) (Split, error) {
	if err := fee.Validate(); err != nil {
		return Split{}, err
	}
	if t.LettingFee < 0 {
		return Split{}, fmt.Errorf("%w: a letting fee of %s", ErrSettlement, t.LettingFee)
	}

	management, err := t.ManagementRate.Of(fee.Gross)
	if err != nil {
		return Split{}, err
	}
	management += t.LettingFee

	// The deduction is on the rent, not on what is left of it: §194-I is read
	// against the amount credited to the owner for the tenancy, and a manager's
	// fee does not reduce the owner's income.
	tds, err := t.withheld(fee.Gross)
	if err != nil {
		return Split{}, err
	}

	s := Split{
		Gross: fee.Gross, Platform: fee.Retained, Management: management, TDS: tds,
		Owner: fee.Gross - fee.Retained - management - tds, RuleID: fee.RuleID,
	}
	if s.Owner < 0 {
		return Split{}, fmt.Errorf("%w: %s of claims against a collection of %s",
			ErrSettlement, s.Platform+s.Management+s.TDS, s.Gross)
	}
	return s, s.Validate()
}

func (t SettlementTerms) withheld(gross Minor) (Minor, error) {
	if t.TDSRate == 0 {
		return 0, nil
	}
	if !t.TDSAcknowledged {
		return 0, fmt.Errorf("%w: %s is due but the tenancy's tax facts are not acknowledged",
			ErrSettlement, t.TDSRate)
	}
	return t.TDSRate.Of(gross)
}
