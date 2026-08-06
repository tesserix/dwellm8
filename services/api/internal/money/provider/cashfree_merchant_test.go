package provider

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
)

// Cashfree as one implementation of the capabilities every provider offers
// (#269). What is worth catching here is translation: their vocabulary for a
// vendor's status and a payout's status is not ours, and a guess in either
// direction is a manager told they can collect when they cannot.

func TestRegisteringAMerchantSendsCashfreeAVendor(t *testing.T) {
	var got map[string]any
	var path string
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		_, _ = w.Write([]byte(`{"vendor_id":"VEND1","status":"IN_BENE_VERIFICATION"}`))
	})

	out, err := c.RegisterMerchant(t.Context(), MerchantRequest{
		IdempotencyKey: "k1", BusinessName: "Menon Properties", BusinessType: "proprietorship",
		Country: "IN", Email: "meera@example.test", Phone: "+919847012345",
		TaxID: "ABCDE1234F", AccountNumber: "50100123454321",
		Settlement: merchant.Settlement{Holder: "Menon Properties", Masked: "XXXXXXXXXX4321", IFSC: "HDFC0001234", Currency: "INR"},
	})
	if err != nil {
		t.Fatalf("registering: %v", err)
	}
	if path != "/easy-split/vendors" {
		t.Fatalf("posted to %s", path)
	}
	bank, _ := got["bank"].(map[string]any)
	if bank["account_number"] != "50100123454321" || bank["ifsc"] != "HDFC0001234" {
		t.Fatalf("bank sent = %v; want the real account for them to verify", bank)
	}
	if out.Ref != "VEND1" || out.State != merchant.Submitted {
		t.Fatalf("registered = %+v; want their vendor id, submitted", out)
	}
}

func TestCashfreeVendorStatusTranslation(t *testing.T) {
	for _, tc := range []struct {
		theirs string
		ours   merchant.State
	}{
		{"ACTIVE", merchant.Verified},
		{"IN_BENE_VERIFICATION", merchant.Submitted},
		{"PENDING", merchant.Submitted},
		{"BLOCKED", merchant.Suspended},
		{"DELETED", merchant.Suspended},
	} {
		t.Run(tc.theirs, func(t *testing.T) {
			c, _ := testCashfree(t, func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"vendor_id":"VEND1","status":"` + tc.theirs + `"}`))
			})
			out, err := c.MerchantState(t.Context(), "VEND1")
			if err != nil {
				t.Fatalf("reading the vendor: %v", err)
			}
			if out.State != tc.ours {
				t.Fatalf("%s translated to %s; want %s", tc.theirs, out.State, tc.ours)
			}
		})
	}
}

// A status nobody has taught the adapter is parked, never guessed: guessing
// verified would let rent be collected into an account Cashfree has blocked.
func TestAnUnknownVendorStatusIsRefusedRatherThanGuessed(t *testing.T) {
	c, _ := testCashfree(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"vendor_id":"VEND1","status":"SOMETHING_NEW"}`))
	})
	if _, err := c.MerchantState(t.Context(), "VEND1"); err == nil {
		t.Fatal("an unknown vendor status was translated anyway")
	}
}

func TestATransferIsOneCashfreePayoutAndItsStateIsTranslated(t *testing.T) {
	var body map[string]any
	c, _ := testCashfree(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_, _ = w.Write([]byte(`{"transfer_id":"po_1","status":"RECEIVED","transfer_amount":500,"transfer_utr":""}`))
	})

	out, err := c.Transfer(t.Context(), TransferRequest{
		IdempotencyKey: "po_1", Amount: 50000, Currency: "INR",
		BeneficiaryRef: "BENE1", Purpose: PurposeOwnerPayout, Narration: "March rent",
	})
	if err != nil {
		t.Fatalf("transferring: %v", err)
	}
	if body["transfer_id"] != "po_1" || body["beneficiary_details"] == nil {
		t.Fatalf("payout body = %v; want our key as their id and a beneficiary", body)
	}
	if out.State != TransferPending {
		t.Fatalf("RECEIVED translated to %s; want pending", out.State)
	}

	done, _ := testCashfree(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"transfer_id":"po_1","status":"SUCCESS","transfer_utr":"UTR123","transfer_amount":500}`))
	})
	settled, err := done.TransferState(t.Context(), "po_1")
	if err != nil {
		t.Fatalf("reading the payout: %v", err)
	}
	if settled.State != TransferSettled || settled.Reference != "UTR123" {
		t.Fatalf("settled = %+v; want the UTR reconciliation matches on", settled)
	}
}

func TestAnIncompleteTransferNeverLeavesTheAdapter(t *testing.T) {
	c, _ := testCashfree(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("an invalid transfer reached Cashfree")
	})
	if _, err := c.Transfer(t.Context(), TransferRequest{Amount: 100, Currency: "INR"}); err == nil {
		t.Fatal("a transfer with no beneficiary was sent")
	}
}
