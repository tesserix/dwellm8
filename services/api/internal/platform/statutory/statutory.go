// Package statutory is the rule registry: every rate, slab and threshold the
// product computes with, effective-dated and scoped to a jurisdiction. ADR-0023.
//
// Nothing here computes tax. It answers one question — what rule was in force in
// this state on this date — and answers it the same way for GST, TDS, a deposit
// cap and a stamp duty scale, because they change for the same reasons and go
// wrong in the same way: silently, months after a Budget, in favour of whoever
// benefits from the stale number.
//
// # A resolution names its date, and never reads the clock
//
// Resolve takes the date as an argument, like every other as-of question in this
// codebase (see platform/effective). A registry that resolved against now() could
// not recompute March's invoice in November, which is the whole point of holding
// rules as rows rather than constants.
//
// # A gap fails loudly
//
// There is no default rate and no fallback threshold. When no rule is in force
// for a type, a jurisdiction and a date, Resolve returns a *Gap naming all four —
// because the alternative is a computation that succeeds with a number nobody
// chose. The deposit cap is the live example: there is deliberately no national
// row, since the Model Tenancy Act is adopted state by state, so an unlisted
// state has to be a gap rather than two months.
package statutory

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// Type is what kind of statutory number a rule holds. The list mirrors the CHECK
// on statutory_rules.rule_type, and the store's contract test fails the build if
// the two ever differ.
type Type string

const (
	GSTRate                       Type = "gst_rate"
	GSTExemptionAmount            Type = "gst_exemption_amount"
	GSTRegistrationThreshold      Type = "gst_registration_threshold"
	TDSRate                       Type = "tds_rate"
	TDSThreshold                  Type = "tds_threshold"
	TDSSurchargeRate              Type = "tds_surcharge_rate"
	TDSCessRate                   Type = "tds_cess_rate"
	DepositCapMonths              Type = "deposit_cap_months"
	AdvanceCapMonths              Type = "advance_cap_months"
	StampDutyRate                 Type = "stamp_duty_rate"
	StampDutyCapAmount            Type = "stamp_duty_cap_amount"
	RegistrationFeeRate           Type = "registration_fee_rate"
	RegistrationTermTriggerMonths Type = "registration_term_trigger_months"
)

// Types is every rule type, for the contract test and for an admin listing.
func Types() []Type {
	return []Type{
		GSTRate, GSTExemptionAmount, GSTRegistrationThreshold,
		TDSRate, TDSThreshold, TDSSurchargeRate, TDSCessRate,
		DepositCapMonths, AdvanceCapMonths,
		StampDutyRate, StampDutyCapAmount,
		RegistrationFeeRate, RegistrationTermTriggerMonths,
	}
}

func (t Type) Valid() bool { return slices.Contains(Types(), t) }

// Jurisdiction is National, or a state.
//
// National is held once rather than copied to twenty-eight states: resolution
// falls back to it and says so, so a central rate cannot be right in Karnataka
// and stale in Maharashtra.
type Jurisdiction string

// National is the central rule.
const National Jurisdiction = "IN"

// State builds a jurisdiction from a two-letter state code.
func State(code string) Jurisdiction { return Jurisdiction(strings.ToUpper(code)) }

var jurisdictionPattern = regexp.MustCompile(`^[A-Z]{2}$`)

func (j Jurisdiction) Valid() bool { return jurisdictionPattern.MatchString(string(j)) }

// Kind is which column carries the value. Four rather than one polymorphic
// number: a rate and an amount are not interchangeable, and code that treats them
// as one is one conversion away from charging 18 paise of GST.
type Kind string

const (
	KindRate   Kind = "rate"
	KindAmount Kind = "amount"
	KindCount  Kind = "count"
	KindSlabs  Kind = "slabs"
)

// Verification is how well the row is known, per india-property-compliance.md §1.1.
type Verification string

const (
	Verified          Verification = "verified"
	NeedsBareActCheck Verification = "needs_bare_act_check"
	Unverified        Verification = "unverified"
	Conflicting       Verification = "conflicting"
)

// Enforcement is what the product does with the rule.
type Enforcement string

const (
	// Block refuses the operation — available only to a verified rule.
	Block Enforcement = "block"
	// Warn tells the user and lets them proceed.
	Warn Enforcement = "warn"
	// RecordOnly stores the fact and says nothing.
	RecordOnly Enforcement = "record_only"
)

// Slab is one band of a progressive scale: [LowerMinor, UpperMinor), with Top
// meaning the band has no upper bound.
//
// Half-open for the same reason intervals of dates are (ADR-0008): the next
// band's lower bound is this one's upper bound exactly, so nobody computes "one
// paisa below".
type Slab struct {
	Seq        int
	LowerMinor int64
	UpperMinor int64
	Top        bool
	RateBps    int
	FlatMinor  int64
}

// Contains reports whether an amount falls in this band.
func (s Slab) Contains(amountMinor int64) bool {
	if amountMinor < s.LowerMinor {
		return false
	}
	return s.Top || amountMinor < s.UpperMinor
}

// Rule is one row of the registry: a value, when it applied, where, and who is
// accountable for it.
type Rule struct {
	ID           string
	Type         Type
	Jurisdiction Jurisdiction
	Key          string

	Kind        Kind
	RateBps     int
	AmountMinor int64
	CountValue  int
	Slabs       []Slab

	Validity effective.Interval
	Corrects string
	Retired  bool

	StatuteRef   string
	SourceURL    string
	Verification Verification
	VerifiedBy   string
	VerifiedOn   effective.Date
	Owner        string
	ReviewDue    effective.Date
	Enforcement  Enforcement
	Note         string
}

var keyPattern = regexp.MustCompile(`^[a-z0-9_]+(\.[a-z0-9_]+)*$`)

// Validate is the Go half of the schema's CHECKs. It exists so a rule assembled
// in code — a test fixture, an admin form, a proposed change — is refused here
// rather than by a constraint violation three layers down.
func (r Rule) Validate() error {
	switch {
	case !r.Type.Valid():
		return fmt.Errorf("statutory: %q is not a rule type", r.Type)
	case !r.Jurisdiction.Valid():
		return fmt.Errorf("statutory: %q is not a jurisdiction — %q or a two-letter state code",
			r.Jurisdiction, National)
	case !keyPattern.MatchString(r.Key):
		return fmt.Errorf("statutory: %q is not a rule key (lower case, dotted)", r.Key)
	case !r.Validity.Valid():
		return fmt.Errorf("statutory: %s/%s/%s does not say when it applied",
			r.Type, r.Jurisdiction, r.Key)
	case strings.TrimSpace(r.StatuteRef) == "":
		return fmt.Errorf("statutory: %s/%s/%s cites no statute — an Act, section, "+
			"notification or circular; a URL is not a citation", r.Type, r.Jurisdiction, r.Key)
	case strings.TrimSpace(r.Owner) == "":
		return fmt.Errorf("statutory: %s/%s/%s has no owner, so nobody reviews it",
			r.Type, r.Jurisdiction, r.Key)
	case r.ReviewDue.Zero():
		return fmt.Errorf("statutory: %s/%s/%s has no review date", r.Type, r.Jurisdiction, r.Key)
	case r.ReviewDue.Before(r.Validity.From()):
		return fmt.Errorf("statutory: %s/%s/%s is due for review on %s, before it takes effect on %s",
			r.Type, r.Jurisdiction, r.Key, r.ReviewDue, r.Validity.From())
	case r.Enforcement == Block && r.Verification != Verified:
		// india-property-compliance.md §1.1, and a CHECK in the schema. A cap
		// enforced from a blog post is worse than no cap: it is wrong with authority.
		return fmt.Errorf("statutory: %s/%s/%s is %s and set to block — only a verified rule may block",
			r.Type, r.Jurisdiction, r.Key, r.Verification)
	case r.Verification == Verified && (strings.TrimSpace(r.VerifiedBy) == "" || r.VerifiedOn.Zero()):
		return fmt.Errorf("statutory: %s/%s/%s claims to be verified and names no human and no date",
			r.Type, r.Jurisdiction, r.Key)
	}
	return r.validateValue()
}

func (r Rule) validateValue() error {
	wrong := func(what string) error {
		return fmt.Errorf("statutory: %s/%s/%s is a %s rule and %s",
			r.Type, r.Jurisdiction, r.Key, r.Kind, what)
	}
	switch r.Kind {
	case KindRate:
		if r.AmountMinor != 0 || r.CountValue != 0 || len(r.Slabs) > 0 {
			return wrong("carries something other than a rate")
		}
		if r.RateBps < 0 {
			return wrong("has a negative rate")
		}
	case KindAmount:
		if r.RateBps != 0 || r.CountValue != 0 || len(r.Slabs) > 0 {
			return wrong("carries something other than an amount")
		}
		if r.AmountMinor < 0 {
			return wrong("has a negative amount")
		}
	case KindCount:
		if r.RateBps != 0 || r.AmountMinor != 0 || len(r.Slabs) > 0 {
			return wrong("carries something other than a count")
		}
		if r.CountValue < 0 {
			return wrong("has a negative count")
		}
	case KindSlabs:
		if r.RateBps != 0 || r.AmountMinor != 0 || r.CountValue != 0 {
			return wrong("carries a scalar as well as its bands")
		}
		return r.validateSlabs()
	default:
		return fmt.Errorf("statutory: %s/%s/%s has no value kind", r.Type, r.Jurisdiction, r.Key)
	}
	return nil
}

// validateSlabs is the Go copy of statutory_rule_slab_shape(): the bands cover
// [0, ) with no gap and a top band. A scale with a hole resolves to nothing for
// an amount that falls in it, which is the failure that would be found by a
// customer.
func (r Rule) validateSlabs() error {
	if len(r.Slabs) == 0 {
		return fmt.Errorf("statutory: %s/%s/%s is a slabs rule with no bands",
			r.Type, r.Jurisdiction, r.Key)
	}
	var next int64
	for i, s := range r.Slabs {
		// A nil bottom band is legitimate and common: a surcharge scale begins with
		// a threshold below which nothing is charged, and that is a zero rate rather
		// than a missing row, so an amount under it resolves to nil instead of to
		// nothing. Anywhere else a zero band is a mistyped bound, because a scale
		// does not stop charging half way up.
		if s.RateBps == 0 && s.FlatMinor == 0 && !(i == 0 && s.LowerMinor == 0) {
			return fmt.Errorf("statutory: band %d of %s/%s/%s charges nothing",
				i, r.Type, r.Jurisdiction, r.Key)
		}
		if s.LowerMinor != next {
			return fmt.Errorf("statutory: %s/%s/%s has a gap or an overlap at %d",
				r.Type, r.Jurisdiction, r.Key, s.LowerMinor)
		}
		if s.Top {
			if i != len(r.Slabs)-1 {
				return fmt.Errorf("statutory: %s/%s/%s has a band above its top band",
					r.Type, r.Jurisdiction, r.Key)
			}
			return nil
		}
		if s.UpperMinor <= s.LowerMinor {
			return fmt.Errorf("statutory: band %d of %s/%s/%s is empty",
				i, r.Type, r.Jurisdiction, r.Key)
		}
		next = s.UpperMinor
	}
	return fmt.Errorf("statutory: %s/%s/%s has no top band — an amount above %d resolves to nothing",
		r.Type, r.Jurisdiction, r.Key, next)
}

// ErrWrongKind is asking a rule for a value it does not hold.
var ErrWrongKind = errors.New("statutory: that rule does not hold that kind of value")

// Rate returns the rate in basis points. 18% is 1800 — never a float, per ADR-0007.
func (r Rule) Rate() (int, error) {
	if r.Kind != KindRate {
		return 0, fmt.Errorf("%w: %s/%s/%s is a %s", ErrWrongKind, r.Type, r.Jurisdiction, r.Key, r.Kind)
	}
	return r.RateBps, nil
}

// Amount returns the amount in minor units.
func (r Rule) Amount() (int64, error) {
	if r.Kind != KindAmount {
		return 0, fmt.Errorf("%w: %s/%s/%s is a %s", ErrWrongKind, r.Type, r.Jurisdiction, r.Key, r.Kind)
	}
	return r.AmountMinor, nil
}

// Count returns the dimensionless count: months of deposit, months of term.
func (r Rule) Count() (int, error) {
	if r.Kind != KindCount {
		return 0, fmt.Errorf("%w: %s/%s/%s is a %s", ErrWrongKind, r.Type, r.Jurisdiction, r.Key, r.Kind)
	}
	return r.CountValue, nil
}

// Band returns the slab an amount falls in. It selects the band and does not
// apply it: what a band means — marginal or flat, on consideration or on rent —
// is the calculation's business, and calculations are out of this package.
func (r Rule) Band(amountMinor int64) (Slab, error) {
	if r.Kind != KindSlabs {
		return Slab{}, fmt.Errorf("%w: %s/%s/%s is a %s", ErrWrongKind, r.Type, r.Jurisdiction, r.Key, r.Kind)
	}
	if amountMinor < 0 {
		return Slab{}, fmt.Errorf("statutory: no band for a negative amount")
	}
	for _, s := range r.Slabs {
		if s.Contains(amountMinor) {
			return s, nil
		}
	}
	// Unreachable for a rule that passed Validate, and worth saying rather than
	// returning a zero band that would charge nothing.
	return Slab{}, fmt.Errorf("statutory: %s/%s/%s has no band covering %d",
		r.Type, r.Jurisdiction, r.Key, amountMinor)
}

// Blocks reports whether this rule may refuse an operation. Verification is
// checked as well as enforcement, so a row that reached the process without
// passing a CHECK still cannot block.
func (r Rule) Blocks() bool { return r.Enforcement == Block && r.Verification == Verified }
