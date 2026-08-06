package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
)

// The capabilities beyond taking a payment (#269). Onboarding a manager's own
// merchant account, moving money onward and charging the platform's fee are the
// same three workflows at Cashfree, Razorpay and Stripe, so they are expressed
// once here and an adapter either implements them or says so.

// full implements every capability; plain implements none beyond Adapter.
type full struct{ provider.Offline }

func (full) Name() string { return "full" }

func (full) RegisterMerchant(_ context.Context, req provider.MerchantRequest) (provider.MerchantStatus, error) {
	return provider.MerchantStatus{Ref: "M-" + req.BusinessName, State: merchant.Submitted}, nil
}

func (full) MerchantState(_ context.Context, ref string) (provider.MerchantStatus, error) {
	return provider.MerchantStatus{Ref: ref, State: merchant.Verified}, nil
}

func (full) Transfer(_ context.Context, req provider.TransferRequest) (provider.Transfer, error) {
	return provider.Transfer{ID: req.IdempotencyKey, Amount: req.Amount, State: provider.TransferPending}, nil
}

func (full) TransferState(_ context.Context, id string) (provider.Transfer, error) {
	return provider.Transfer{ID: id, State: provider.TransferSettled}, nil
}

type plain struct{ provider.Offline }

func (plain) Name() string { return "plain" }

func registry(t *testing.T, adapters ...provider.Adapter) *provider.Registry {
	t.Helper()
	r := provider.NewRegistry()
	for _, a := range adapters {
		r.Register(a)
	}
	return r
}

func TestAnAdapterThatOnboardsMerchantsIsFoundByName(t *testing.T) {
	r := registry(t, full{})

	m, err := provider.MerchantsBy(r, "full")
	if err != nil {
		t.Fatalf("merchant capability of an adapter that has one: %v", err)
	}
	out, err := m.RegisterMerchant(t.Context(), provider.MerchantRequest{BusinessName: "Menon"})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	if out.Ref != "M-Menon" || out.State != merchant.Submitted {
		t.Fatalf("registered = %+v; want a submitted account with the provider's ref", out)
	}
}

// The refusal has to be its own error: a manager on a provider that cannot
// onboard them is a configuration answer, not a fault to retry.
func TestAnAdapterWithoutTheCapabilitySaysSo(t *testing.T) {
	r := registry(t, plain{})

	if _, err := provider.MerchantsBy(r, "plain"); !errors.Is(err, provider.ErrCapability) {
		t.Fatalf("merchant capability of an adapter without one = %v; want ErrCapability", err)
	}
	if _, err := provider.PayoutsBy(r, "plain"); !errors.Is(err, provider.ErrCapability) {
		t.Fatalf("payout capability of an adapter without one = %v; want ErrCapability", err)
	}
	if _, err := provider.MerchantsBy(r, "nobody"); errors.Is(err, provider.ErrCapability) {
		t.Fatalf("an adapter that is not registered at all reported as a missing capability")
	}
}

func TestMoneyMovesOnwardThroughTheSameShapeForEveryProvider(t *testing.T) {
	r := registry(t, full{})

	p, err := provider.PayoutsBy(r, "full")
	if err != nil {
		t.Fatalf("payout capability: %v", err)
	}
	out, err := p.Transfer(t.Context(), provider.TransferRequest{
		IdempotencyKey: "po_1", Amount: 5000000, Currency: "INR",
		BeneficiaryRef: "B1", Purpose: provider.PurposeOwnerPayout,
	})
	if err != nil {
		t.Fatalf("transferring: %v", err)
	}
	if out.State != provider.TransferPending || out.Amount != domain.Minor(5000000) {
		t.Fatalf("transfer = %+v; want the amount asked for, pending", out)
	}
	settled, err := p.TransferState(t.Context(), out.ID)
	if err != nil {
		t.Fatalf("reading the transfer back: %v", err)
	}
	if settled.State != provider.TransferSettled {
		t.Fatalf("state = %s; want settled", settled.State)
	}
}

func TestATransferNobodyCanBePaidIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  provider.TransferRequest
	}{
		{"no beneficiary", provider.TransferRequest{IdempotencyKey: "k", Amount: 100, Currency: "INR", Purpose: provider.PurposeOwnerPayout}},
		{"no amount", provider.TransferRequest{IdempotencyKey: "k", Currency: "INR", BeneficiaryRef: "B1", Purpose: provider.PurposeOwnerPayout}},
		{"no idempotency key", provider.TransferRequest{Amount: 100, Currency: "INR", BeneficiaryRef: "B1", Purpose: provider.PurposeOwnerPayout}},
		{"no purpose", provider.TransferRequest{IdempotencyKey: "k", Amount: 100, Currency: "INR", BeneficiaryRef: "B1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.req.Validate(); err == nil {
				t.Fatalf("%+v was accepted", tc.req)
			}
		})
	}
	ok := provider.TransferRequest{
		IdempotencyKey: "k", Amount: 100, Currency: "INR",
		BeneficiaryRef: "B1", Purpose: provider.PurposeOwnerPayout,
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a complete transfer was refused: %v", err)
	}
}

// Collecting into an account the provider has not verified is the one refusal
// every provider makes, so the seam makes it before the call is attempted.
func TestNothingIsCollectedIntoAnUnverifiedAccount(t *testing.T) {
	for _, tc := range []struct {
		state merchant.State
		ok    bool
	}{
		{merchant.Verified, true},
		{merchant.Submitted, false},
		{merchant.Suspended, false},
		{merchant.Unconnected, false},
	} {
		t.Run(string(tc.state), func(t *testing.T) {
			err := provider.CheckCollectable(provider.MerchantStatus{Ref: "M1", State: tc.state})
			if tc.ok && err != nil {
				t.Fatalf("a %s account was refused: %v", tc.state, err)
			}
			if !tc.ok && !errors.Is(err, merchant.ErrUnverified) {
				t.Fatalf("a %s account gave %v; want ErrUnverified", tc.state, err)
			}
		})
	}
}

// The platform's fee is one shape too: a share of the collection retained at
// capture, whoever the aggregator is.
func TestThePlatformFeeIsExpressedTheSameWayForEveryProvider(t *testing.T) {
	split, err := provider.PlatformFee{Amount: 29900, Currency: "INR"}.Split(1000000, "V1")
	if err != nil {
		t.Fatalf("splitting a collection: %v", err)
	}
	if split.VendorID != "V1" || split.Amount != domain.Minor(1000000-29900) {
		t.Fatalf("split = %+v; want the vendor keeping the collection less the fee", split)
	}
	if _, err := (provider.PlatformFee{Amount: 2000000, Currency: "INR"}).Split(1000000, "V1"); err == nil {
		t.Fatal("a fee larger than the collection was accepted")
	}
}
