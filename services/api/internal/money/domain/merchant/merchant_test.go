package merchant_test

import (
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
)

// The manager's own account with whoever moves the money (#269). The states
// and the rule that follows from them — nothing is collected into an account
// nobody has verified — belong to the domain, not to Cashfree.

func TestOnlyAVerifiedAccountMayBeCollectedInto(t *testing.T) {
	for _, tc := range []struct {
		state   merchant.State
		collect bool
	}{
		{merchant.Unconnected, false},
		{merchant.Submitted, false},
		{merchant.Verified, true},
		{merchant.Suspended, false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			if got := tc.state.MayCollect(); got != tc.collect {
				t.Fatalf("%s may collect = %v, want %v", tc.state, got, tc.collect)
			}
		})
	}
}

func TestTheAccountWalksForwardAndBack(t *testing.T) {
	for _, tc := range []struct {
		name string
		from merchant.State
		to   merchant.State
		ok   bool
	}{
		{"submitting what was never connected", merchant.Unconnected, merchant.Submitted, true},
		{"the provider verifies it", merchant.Submitted, merchant.Verified, true},
		{"the provider suspends it", merchant.Verified, merchant.Suspended, true},
		{"a suspension is lifted by re-verifying", merchant.Suspended, merchant.Verified, true},
		{"verifying what was never submitted", merchant.Unconnected, merchant.Verified, false},
		{"going back to unconnected", merchant.Verified, merchant.Unconnected, false},
		{"standing still", merchant.Verified, merchant.Verified, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.from.CanMoveTo(tc.to)
			if tc.ok && err != nil {
				t.Fatalf("%s → %s: %v", tc.from, tc.to, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("%s → %s was allowed", tc.from, tc.to)
			}
		})
	}
}

// Changing where the money settles un-verifies the account: the verification
// was of that bank account, not of the merchant in the abstract.
func TestChangingTheSettlementAccountNeedsVerifyingAgain(t *testing.T) {
	a := merchant.Account{
		Provider: "cashfree", MerchantRef: "MRC1", State: merchant.Verified,
		Settlement: merchant.Settlement{Holder: "Menon Properties", Masked: "XXXX4321", IFSC: "HDFC0001234", Currency: "INR"},
	}
	moved, err := a.Resettle(merchant.Settlement{
		Holder: "Menon Properties", Masked: "XXXX9876", IFSC: "HDFC0001234", Currency: "INR",
	})
	if err != nil {
		t.Fatalf("moving the settlement account: %v", err)
	}
	if moved.State != merchant.Submitted {
		t.Fatalf("state after a new bank account = %s; want re-submitted for verification", moved.State)
	}
	same, err := a.Resettle(a.Settlement)
	if err != nil {
		t.Fatalf("restating the same account: %v", err)
	}
	if same.State != merchant.Verified {
		t.Fatalf("restating the same account un-verified it: %s", same.State)
	}
}

func TestAnAccountThatSettlesNowhereIsRefused(t *testing.T) {
	a := merchant.Account{Provider: "cashfree", MerchantRef: "MRC1"}
	if err := a.Validate(); !errors.Is(err, merchant.ErrIncomplete) {
		t.Fatalf("an account with no settlement details = %v; want ErrIncomplete", err)
	}
	a.Settlement = merchant.Settlement{Holder: "Menon Properties", Masked: "XXXX4321", IFSC: "HDFC0001234", Currency: "INR"}
	if err := a.Validate(); err != nil {
		t.Fatalf("a complete account was refused: %v", err)
	}
}

// An Indian account needs an IFSC; an international one needs neither, and the
// domain must not demand one of a Stripe merchant in Singapore.
func TestSettlementIsValidatedForItsCountry(t *testing.T) {
	in := merchant.Account{
		Provider: "cashfree", MerchantRef: "M1", Country: "IN",
		Settlement: merchant.Settlement{Holder: "Menon Properties", Masked: "XXXX4321", Currency: "INR"},
	}
	if err := in.Validate(); !errors.Is(err, merchant.ErrIncomplete) {
		t.Fatalf("an Indian account with no IFSC = %v; want ErrIncomplete", err)
	}
	sg := merchant.Account{
		Provider: "stripe", MerchantRef: "acct_1", Country: "SG",
		Settlement: merchant.Settlement{Holder: "Menon Pte Ltd", Masked: "XXXX4321", Currency: "SGD"},
	}
	if err := sg.Validate(); err != nil {
		t.Fatalf("an international account was refused for want of an IFSC: %v", err)
	}
}
