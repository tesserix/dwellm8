// Capabilities beyond taking a payment (#269).
//
// Adapter is what every aggregator does; these are what only some of them do,
// and they are optional interfaces rather than methods on Adapter so that an
// adapter which cannot onboard a merchant says so at the type level instead of
// returning "not implemented" at the worst moment. The shapes are deliberately
// the union nobody's vocabulary survives: Cashfree's merchant, Razorpay's
// linked account and Stripe's connected account are one MerchantRequest, and
// their payouts, transfers and fund-account payments are one TransferRequest.
// Domain code never learns which one is behind it. ADR-0011 §1.

package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
)

// ErrCapability is an adapter being asked for something it does not do. It is
// its own error because it is a configuration answer — this manager is on a
// provider that cannot onboard them — and never something to retry.
var ErrCapability = errors.New("provider: the adapter does not offer that capability")

// MerchantRequest is a manager's own account, in the terms every aggregator
// asks for. Nothing here is a bank account number: the number goes to the
// provider through Settlement, and what comes back is a masked reference.
type MerchantRequest struct {
	// IdempotencyKey stops a second submission creating a second merchant.
	IdempotencyKey string
	BusinessName   string
	// BusinessType is the local legal form — 'individual', 'proprietorship',
	// 'partnership', 'company'. Adapters map it to their own enum.
	BusinessType string
	// Country is ISO-3166 alpha-2 and decides which tax fields matter: PAN and
	// GSTIN in India, a tax id elsewhere.
	Country string
	Email   string
	Phone   string
	// TaxID is the PAN in India. Never logged, and held only as long as the call.
	TaxID string
	// GSTIN is India-only and optional — a small manager below the threshold has
	// none, and demanding one would exclude most of them.
	GSTIN string
	// Settlement is where the money goes, masked — what the manager will be
	// shown, and what is stored once the provider answers.
	Settlement merchant.Settlement
	// AccountNumber is the real bank account, held only for the length of the
	// call: the provider must verify it, and nothing on this side may keep it.
	AccountNumber string
}

// MerchantStatus is what the provider says about that account. It is the
// domain's own state, not the provider's string: an adapter that returns
// anything else has failed to translate.
type MerchantStatus struct {
	Ref   string
	State merchant.State
	// Reason carries the provider's words when it refuses or suspends, so the
	// manager is told what to fix rather than that something went wrong.
	Reason string
	// Settlement is what the provider holds, masked. It may differ from what was
	// sent — that is the point of reading it back.
	Settlement merchant.Settlement
}

// Merchants is the optional capability of onboarding somebody else's account.
type Merchants interface {
	RegisterMerchant(ctx context.Context, req MerchantRequest) (MerchantStatus, error)
	MerchantState(ctx context.Context, ref string) (MerchantStatus, error)
}

// TransferState is where money sent onward has got to. Four states, because
// every provider's dozen collapse to these and the extra ones are all
// synonyms of pending.
type TransferState string

const (
	TransferPending  TransferState = "pending"
	TransferSettled  TransferState = "settled"
	TransferReturned TransferState = "returned"
	TransferFailed   TransferState = "failed"
)

func (s TransferState) String() string { return string(s) }

// Purpose is why money is moving onward. It is not decoration: Indian payout
// rails require a purpose code, and the ledger needs to know which leg it is
// reconciling.
type Purpose string

const (
	// PurposeOwnerPayout is rent reaching the owner, net of everything.
	PurposeOwnerPayout Purpose = "owner_payout"
	// PurposeDepositRefund is a deposit going back to a tenant.
	PurposeDepositRefund Purpose = "deposit_refund"
	// PurposeVendorPayment is a contractor paid for a repair.
	PurposeVendorPayment Purpose = "vendor_payment"
	// PurposeStaffPayout is a firm paying its own people (#278).
	PurposeStaffPayout Purpose = "staff_payout"
)

// TransferRequest moves settled money onward. One shape for a Cashfree payout,
// a Razorpay fund-account payment and a Stripe transfer.
type TransferRequest struct {
	IdempotencyKey string
	Amount         domain.Minor
	Currency       string
	// BeneficiaryRef is the provider's own id for who is being paid, created
	// when the payout account was registered. Never a bank account number.
	BeneficiaryRef string
	Purpose        Purpose
	// Narration is what appears on the beneficiary's statement.
	Narration string
	// FromMerchantRef pays out of one manager's merchant balance rather than the
	// platform's. Empty is the platform's own.
	FromMerchantRef string
}

// Validate refuses a transfer no provider could act on.
func (r TransferRequest) Validate() error {
	switch {
	case r.IdempotencyKey == "":
		return errors.New("provider: a transfer with no idempotency key could pay twice")
	case r.Amount <= 0:
		return fmt.Errorf("provider: a transfer of %s", r.Amount)
	case r.Currency == "":
		return errors.New("provider: a transfer with no currency")
	case r.BeneficiaryRef == "":
		return errors.New("provider: a transfer with no beneficiary reaches nobody")
	case r.Purpose == "":
		return errors.New("provider: a transfer with no purpose cannot be coded to the rails")
	}
	return nil
}

// Transfer is one movement as the provider sees it.
type Transfer struct {
	ID     string
	State  TransferState
	Amount domain.Minor
	// Reference is the provider's own UTR or equivalent, which is what a bank
	// statement will show and therefore what reconciliation matches on.
	Reference string
	Reason    string
}

// Payouts is the optional capability of sending money onward.
type Payouts interface {
	Transfer(ctx context.Context, req TransferRequest) (Transfer, error)
	TransferState(ctx context.Context, id string) (Transfer, error)
}

// MerchantsBy returns the merchant capability of a named adapter.
func MerchantsBy(r *Registry, name string) (Merchants, error) {
	a, err := r.By(name)
	if err != nil {
		return nil, err
	}
	m, ok := a.(Merchants)
	if !ok {
		return nil, fmt.Errorf("%w: %s cannot onboard merchants", ErrCapability, name)
	}
	return m, nil
}

// PayoutsBy returns the payout capability of a named adapter.
func PayoutsBy(r *Registry, name string) (Payouts, error) {
	a, err := r.By(name)
	if err != nil {
		return nil, err
	}
	p, ok := a.(Payouts)
	if !ok {
		return nil, fmt.Errorf("%w: %s cannot send money onward", ErrCapability, name)
	}
	return p, nil
}

// CheckCollectable is the refusal every provider makes, made here first so a
// manager is told to finish their onboarding rather than shown a provider error.
func CheckCollectable(s MerchantStatus) error {
	if s.State.MayCollect() {
		return nil
	}
	if s.Reason != "" {
		return fmt.Errorf("%w: %s is %s — %s", merchant.ErrUnverified, s.Ref, s.State, s.Reason)
	}
	return fmt.Errorf("%w: %s is %s", merchant.ErrUnverified, s.Ref, s.State)
}

// PlatformFee is Dwellm8's own share of a collection, retained at capture so no
// client money rests in a platform account. ADR-0031.
type PlatformFee struct {
	Amount   domain.Minor
	Currency string
}

// Split turns the fee into the vendor leg of a collection: the manager keeps
// the collection less the fee, and the remainder is ours. One amount, because
// two would be two chances to disagree with the order total.
func (f PlatformFee) Split(order domain.Minor, vendorID string) (Split, error) {
	s := Split{VendorID: vendorID, Amount: order - f.Amount}
	if err := s.Validate(order); err != nil {
		return Split{}, err
	}
	return s, nil
}
