// Package merchant holds the account the money actually settles into.
//
// Dwellm8 never pools client money: a manager collects into their own merchant
// account with their own aggregator, under their own agreement. Which
// aggregator that is belongs to money/provider — Cashfree calls it a merchant,
// Razorpay an account, Stripe a connected account, and all three verify a bank
// account before they will settle to it. What is here is the part that is the
// same everywhere: the states, the rule that nothing is collected into an
// unverified account, and the fact that verification is of a bank account
// rather than of a business in the abstract. #269.
package merchant

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// State is where the account stands with its provider.
type State string

const (
	// Unconnected: the manager has no account with any provider yet.
	Unconnected State = "unconnected"
	// Submitted: details are with the provider, who has not yet said yes.
	Submitted State = "submitted"
	// Verified: the provider will settle to it, so rent may be collected.
	Verified State = "verified"
	// Suspended: the provider has stopped settling — KYC lapsed, a dispute,
	// a frozen bank account. Recoverable by verifying again.
	Suspended State = "suspended"
)

// ErrIncomplete is an account that cannot be submitted to any provider.
var ErrIncomplete = errors.New("merchant: the account is missing details the provider needs")

// ErrTransition is a move the state machine does not make.
var ErrTransition = errors.New("merchant: that is not a move this account makes")

// ErrUnverified is the refusal every collection path checks.
var ErrUnverified = errors.New("merchant: no verified account to collect into")

// MayCollect reports whether rent may be collected into an account in this
// state. The one question the rest of the module asks.
func (s State) MayCollect() bool { return s == Verified }

func (s State) String() string { return string(s) }

var moves = map[State][]State{
	Unconnected: {Submitted},
	Submitted:   {Verified, Suspended},
	Verified:    {Suspended, Submitted},
	Suspended:   {Verified, Submitted},
}

// CanMoveTo reports whether the account may walk from one state to another.
// Standing still is refused: a no-op transition hides a provider that answered
// nothing.
func (s State) CanMoveTo(to State) error {
	if slices.Contains(moves[s], to) {
		return nil
	}
	return fmt.Errorf("%w: %s → %s", ErrTransition, s, to)
}

// Settlement is where the provider sends the money. The number itself is never
// held here — masked and fingerprinted, the way payout accounts already are.
type Settlement struct {
	Holder   string
	Masked   string
	IFSC     string
	Currency string
	// SwiftBIC carries an international account where there is no IFSC.
	SwiftBIC string
}

// Account is one manager's merchant account with one provider.
type Account struct {
	Provider    string
	MerchantRef string
	// Country is the ISO-3166 alpha-2 the account is regulated in; empty reads
	// as India, which is what all of this is for first.
	Country      string
	BusinessName string
	BusinessType string
	State        State
	Settlement   Settlement
	// Reason carries the provider's own words when it suspends or refuses.
	Reason string
	// VerifiedAt is when the provider last said it would settle to this account.
	VerifiedAt time.Time
}

var ifsc = regexp.MustCompile(`^[A-Z]{4}0[A-Z0-9]{6}$`)

// Validate refuses an account no provider could act on.
func (a Account) Validate() error {
	switch {
	case a.Provider == "":
		return fmt.Errorf("%w: no provider", ErrIncomplete)
	case a.Settlement.Holder == "":
		return fmt.Errorf("%w: no account holder", ErrIncomplete)
	case a.Settlement.Masked == "":
		return fmt.Errorf("%w: no account number", ErrIncomplete)
	case a.Settlement.Currency == "":
		return fmt.Errorf("%w: no currency", ErrIncomplete)
	}
	if a.indian() && !ifsc.MatchString(strings.ToUpper(a.Settlement.IFSC)) {
		return fmt.Errorf("%w: %q is not an IFSC", ErrIncomplete, a.Settlement.IFSC)
	}
	return nil
}

func (a Account) indian() bool { return a.Country == "" || strings.EqualFold(a.Country, "IN") }

// Resettle points the account at a different bank account. A genuinely new one
// puts the account back to submitted: what the provider verified was that bank
// account, and re-stating the one on file changes nothing.
func (a Account) Resettle(to Settlement) (Account, error) {
	out := a
	out.Settlement = to
	if err := out.Validate(); err != nil {
		return Account{}, err
	}
	if to == a.Settlement {
		return out, nil
	}
	out.State = Submitted
	out.Reason = ""
	return out, nil
}
