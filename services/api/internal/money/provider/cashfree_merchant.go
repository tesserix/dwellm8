// Cashfree's implementation of the capabilities in capability.go (#269).
//
// Easy Split calls a merchant a vendor and payouts call a beneficiary
// something else again; both are translated here so that no caller ever learns
// either word. Every status is translated from a named list — an unknown one is
// an error, never a guess, for the same reason Confirm parks a payment it does
// not recognise: guessing "verified" lets rent be collected into an account
// Cashfree has blocked.

package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
)

// RegisterMerchant creates the manager's Easy Split vendor.
func (c *Cashfree) RegisterMerchant(ctx context.Context, req MerchantRequest) (MerchantStatus, error) {
	switch {
	case req.BusinessName == "":
		return MerchantStatus{}, errors.New("cashfree: a vendor with no name")
	case req.AccountNumber == "":
		return MerchantStatus{}, errors.New("cashfree: a vendor with no bank account cannot be verified")
	}
	body := map[string]any{
		"vendor_id": req.IdempotencyKey,
		"status":    "ACTIVE",
		"name":      req.BusinessName,
		"email":     req.Email,
		"phone":     req.Phone,
		"bank": map[string]any{
			"account_number": req.AccountNumber,
			"account_holder": req.Settlement.Holder,
			"ifsc":           req.Settlement.IFSC,
		},
		"kyc_details": map[string]any{
			"account_type":  req.BusinessType,
			"business_type": req.BusinessType,
			"pan":           req.TaxID,
			"gst":           req.GSTIN,
		},
	}
	var out cashfreeVendor
	if err := c.call(ctx, http.MethodPost, "/easy-split/vendors", req.IdempotencyKey, body, &out); err != nil {
		return MerchantStatus{}, err
	}
	return out.status(req.Settlement)
}

// MerchantState asks Cashfree what it thinks of the vendor now.
func (c *Cashfree) MerchantState(ctx context.Context, ref string) (MerchantStatus, error) {
	if ref == "" {
		return MerchantStatus{}, errors.New("cashfree: no vendor to ask about")
	}
	var out cashfreeVendor
	if err := c.call(ctx, http.MethodGet, "/easy-split/vendors/"+ref, "", nil, &out); err != nil {
		return MerchantStatus{}, err
	}
	return out.status(merchant.Settlement{})
}

type cashfreeVendor struct {
	VendorID string `json:"vendor_id"`
	Status   string `json:"status"`
	Remarks  string `json:"remarks"`
}

// vendorStates is the whole translation. Anything absent is parked.
var vendorStates = map[string]merchant.State{
	"ACTIVE":               merchant.Verified,
	"PENDING":              merchant.Submitted,
	"IN_BENE_VERIFICATION": merchant.Submitted,
	"BLOCKED":              merchant.Suspended,
	"DELETED":              merchant.Suspended,
}

func (v cashfreeVendor) status(s merchant.Settlement) (MerchantStatus, error) {
	state, known := vendorStates[v.Status]
	if !known {
		return MerchantStatus{}, fmt.Errorf("cashfree: vendor %s is %q, which this adapter does not know how to read",
			v.VendorID, v.Status)
	}
	return MerchantStatus{Ref: v.VendorID, State: state, Reason: v.Remarks, Settlement: s}, nil
}

// Transfer is one Cashfree payout. Our idempotency key is their transfer_id, so
// their uniqueness and ours become the same fact — the same trick CreateOrder
// plays with order_id, and the reason a retry cannot pay twice.
func (c *Cashfree) Transfer(ctx context.Context, req TransferRequest) (Transfer, error) {
	if err := req.Validate(); err != nil {
		return Transfer{}, err
	}
	body := map[string]any{
		"transfer_id":     req.IdempotencyKey,
		"transfer_amount": rupees(req.Amount),
		"transfer_mode":   "banktransfer",
		"remarks":         req.Narration,
		"beneficiary_details": map[string]any{
			"beneficiary_id": req.BeneficiaryRef,
		},
		"fundsource_id": req.FromMerchantRef,
	}
	var out cashfreeTransfer
	if err := c.call(ctx, http.MethodPost, "/payout/transfers", req.IdempotencyKey, body, &out); err != nil {
		return Transfer{}, err
	}
	return out.transfer()
}

// TransferState is the same read a payout poll makes.
func (c *Cashfree) TransferState(ctx context.Context, id string) (Transfer, error) {
	if id == "" {
		return Transfer{}, errors.New("cashfree: no transfer to ask about")
	}
	var out cashfreeTransfer
	if err := c.call(ctx, http.MethodGet, "/payout/transfers?transfer_id="+id, "", nil, &out); err != nil {
		return Transfer{}, err
	}
	return out.transfer()
}

type cashfreeTransfer struct {
	TransferID string      `json:"transfer_id"`
	Status     string      `json:"status"`
	Amount     json.Number `json:"transfer_amount"`
	UTR        string      `json:"transfer_utr"`
	Reason     string      `json:"status_description"`
}

// transferStates: every pending synonym collapses to pending, and a reversal is
// returned rather than failed — the money left and came back, which the ledger
// has to say out loud.
var transferStates = map[string]TransferState{
	"RECEIVED":         TransferPending,
	"APPROVAL_PENDING": TransferPending,
	"PENDING":          TransferPending,
	"PROCESSING":       TransferPending,
	"SUCCESS":          TransferSettled,
	"REVERSED":         TransferReturned,
	"FAILED":           TransferFailed,
	"REJECTED":         TransferFailed,
}

func (t cashfreeTransfer) transfer() (Transfer, error) {
	state, known := transferStates[t.Status]
	if !known {
		return Transfer{}, fmt.Errorf("cashfree: transfer %s is %q, which this adapter does not know how to read",
			t.TransferID, t.Status)
	}
	amount, err := minorFromRupees(t.Amount)
	if err != nil {
		return Transfer{}, err
	}
	return Transfer{
		ID: t.TransferID, State: state, Amount: amount,
		Reference: t.UTR, Reason: t.Reason,
	}, nil
}
