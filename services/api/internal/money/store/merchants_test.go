package store_test

import (
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The manager's own merchant account against PostgreSQL (#269): connected once
// per provider, walked through the provider's verdicts, and invisible to
// anybody else's organisation.

func merchantAccount(name string) merchant.Account {
	return merchant.Account{
		Provider: name, Country: "IN", BusinessName: "Menon Properties",
		BusinessType: "proprietorship", State: merchant.Submitted, MerchantRef: "MRC-1",
		Settlement: merchant.Settlement{
			Holder: "Menon Properties", Masked: "XXXX4321",
			IFSC: "HDFC0001234", Currency: "INR",
		},
	}
}

func TestConnectingAnAccountAndReadingItBack(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewMerchants(pool)
	ctx := ownerCtx()

	saved, err := s.Connect(ctx, merchantAccount("cashfree"))
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	if saved.State != merchant.Submitted || saved.Settlement.Masked != "XXXX4321" {
		t.Fatalf("connected = %+v; want a submitted account settling to the masked number", saved)
	}

	got, err := s.ForProvider(ctx, "cashfree")
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if got.BusinessName != "Menon Properties" || got.State != merchant.Submitted {
		t.Fatalf("read back = %+v", got)
	}
}

// Connecting twice is the manager pressing the button again, not a second
// account: one row per provider, updated.
func TestConnectingTheSameProviderTwiceKeepsOneAccount(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewMerchants(pool)
	ctx := ownerCtx()

	if _, err := s.Connect(ctx, merchantAccount("cashfree")); err != nil {
		t.Fatalf("connecting: %v", err)
	}
	again := merchantAccount("cashfree")
	again.BusinessName = "Menon Estates"
	if _, err := s.Connect(ctx, again); err != nil {
		t.Fatalf("connecting again: %v", err)
	}

	all, err := s.List(ctx)
	if err != nil {
		t.Fatalf("listing: %v", err)
	}
	var cashfree int
	for _, a := range all {
		if a.Provider == "cashfree" {
			cashfree++
			if a.BusinessName != "Menon Estates" {
				t.Fatalf("the second connect did not update the account: %+v", a)
			}
		}
	}
	if cashfree != 1 {
		t.Fatalf("%d cashfree accounts; want exactly one", cashfree)
	}
}

func TestTheProvidersVerdictMovesTheAccountAndNothingElseDoes(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewMerchants(pool)
	ctx := ownerCtx()

	if _, err := s.Connect(ctx, merchantAccount("cashfree")); err != nil {
		t.Fatalf("connecting: %v", err)
	}
	verified, err := s.Record(ctx, "cashfree", merchant.Verified, "")
	if err != nil {
		t.Fatalf("recording the verdict: %v", err)
	}
	if !verified.State.MayCollect() || verified.VerifiedAt.IsZero() {
		t.Fatalf("verified = %+v; want a collectable account stamped with when", verified)
	}

	// The provider cannot un-invent the account, and the store refuses the move
	// the domain refuses.
	if _, err := s.Record(ctx, "cashfree", merchant.Unconnected, ""); !errors.Is(err, merchant.ErrTransition) {
		t.Fatalf("verified → unconnected = %v; want ErrTransition", err)
	}

	suspended, err := s.Record(ctx, "cashfree", merchant.Suspended, "KYC documents expired")
	if err != nil {
		t.Fatalf("suspending: %v", err)
	}
	if suspended.State.MayCollect() || suspended.Reason != "KYC documents expired" {
		t.Fatalf("suspended = %+v; want collection refused and the provider's words kept", suspended)
	}
}

func TestAnAccountNobodyConnectedIsAbsentRatherThanEmpty(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewMerchants(pool)

	if _, err := s.ForProvider(ownerCtx(), "stripe"); !errors.Is(err, store.ErrNoMerchant) {
		t.Fatalf("an unconnected provider = %v; want ErrNoMerchant", err)
	}
	if _, err := s.Record(ownerCtx(), "stripe", merchant.Verified, ""); !errors.Is(err, store.ErrNoMerchant) {
		t.Fatalf("a verdict about nothing = %v; want ErrNoMerchant", err)
	}
}

// Where a manager's rent settles is nobody else's business.
func TestAnotherOrganisationCannotSeeTheAccount(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewMerchants(pool)

	if _, err := s.Connect(ownerCtx(), merchantAccount("cashfree")); err != nil {
		t.Fatalf("connecting: %v", err)
	}
	outsider := tenancy.With(t.Context(), isolationtest.OrgOutsider)
	if _, err := s.ForProvider(outsider, "cashfree"); !errors.Is(err, store.ErrNoMerchant) {
		t.Fatalf("an outsider read the account: %v", err)
	}
	all, err := s.List(outsider)
	if err != nil {
		t.Fatalf("listing as an outsider: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("an outsider listed %d accounts", len(all))
	}
}

// A provider that verifies on the spot answers "verified" to the very first
// call, so connect has to stamp the verification itself — the row is otherwise
// a verified account nothing ever verified.
func TestConnectingAnAlreadyVerifiedAccountStampsTheVerification(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewMerchants(pool)

	a := merchantAccount("cashfree")
	a.State = merchant.Verified
	out, err := s.Connect(ownerCtx(), a)
	if err != nil {
		t.Fatalf("connecting a verified account: %v", err)
	}
	if out.State != merchant.Verified || out.VerifiedAt.IsZero() {
		t.Fatalf("connected = %+v; want verified and stamped", out)
	}
}
