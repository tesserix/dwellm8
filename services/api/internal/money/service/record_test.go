package service_test

import (
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Money that already moved. There is no payer session and no provider to ask,
// so a collection recorded by hand has to make the whole trip in one act —
// created, attempted, captured — or the receipt never posts (#297).
//
// The harness's own registry serves these without a fake: NewRegistry carries
// the offline adapter, and For routes an offline method to it whatever the
// chain says.

func (h harness) record(amount domain.Minor, method collect.Method, key string) (collect.Payment, error) {
	h.t.Helper()
	return h.svc.Record(h.ctx, service.CollectRequest{
		TenantID:       isolationtest.OrgOwner.String(),
		Property:       isolationtest.PropertyGranted,
		Unit:           h.unit,
		Lease:          h.lease,
		PayerID:        h.payer,
		Amount:         amount,
		Method:         method,
		IdempotencyKey: key,
	})
}

func TestRecordingCashCapturesItAndPostsTheReceipt(t *testing.T) {
	h := newHarness(t)
	h.charge(2_000_000, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))

	p, err := h.record(500_000, collect.MethodOfflineCash, h.token+"-cash")
	if err != nil {
		t.Fatalf("recording cash: %v", err)
	}
	if p.Status != collect.StatusCaptured {
		t.Errorf("status = %s, want captured — the money is already in the manager's hand", p.Status)
	}
	if p.EntryID == "" {
		t.Error("the payment captured and posted no ledger entry")
	}
	if got := h.position(time.Now()); got != 1_500_000 {
		t.Errorf("the lease owes %s after ₹5,000 in cash, want 1500000", got)
	}
}

func TestRecordingTheSameCashTwicePostsItOnce(t *testing.T) {
	h := newHarness(t)
	h.charge(2_000_000, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC))

	first, err := h.record(500_000, collect.MethodOfflineCheque, h.token+"-cheque")
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := h.record(500_000, collect.MethodOfflineCheque, h.token+"-cheque")
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("a retry made a second payment: %s then %s", first.ID, second.ID)
	}
	if n := h.entriesFor(first.ID); n != 1 {
		t.Errorf("%d ledger entries for one payment, want 1", n)
	}
	if got := h.position(time.Now()); got != 1_500_000 {
		t.Errorf("the lease owes %s after the same ₹5,000 recorded twice, want 1500000", got)
	}
}

// A method that goes through a provider cannot be asserted by a person: doing
// so would record a receipt nobody witnessed.
func TestAnOnlineMethodCannotBeRecordedByHand(t *testing.T) {
	h := newHarness(t)
	if _, err := h.record(500_000, collect.MethodUPIIntent, h.token+"-upi"); err == nil {
		t.Fatal("recording a UPI collection by hand was allowed")
	}
}
