package service_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
)

// Dividing a collection and paying the owner's leg out (#270). The provider is
// a fake because what matters here is the sequence: nothing is paid out of an
// unverified account, a division happens once, and the provider's verdict on
// the transfer is what moves the instruction — never our own optimism.

type payoutProvider struct {
	provider.Offline
	sent     []provider.TransferRequest
	state    provider.TransferState
	fail     error
	merchant merchant.State
}

func (p *payoutProvider) Name() string { return "fake" }

func (p *payoutProvider) MerchantState(_ context.Context, ref string) (provider.MerchantStatus, error) {
	return provider.MerchantStatus{Ref: ref, State: p.merchant}, nil
}

func (p *payoutProvider) Transfer(_ context.Context, req provider.TransferRequest) (provider.Transfer, error) {
	if p.fail != nil {
		return provider.Transfer{}, p.fail
	}
	p.sent = append(p.sent, req)
	return provider.Transfer{ID: req.IdempotencyKey, State: p.state, Amount: req.Amount}, nil
}

func (p *payoutProvider) TransferState(_ context.Context, id string) (provider.Transfer, error) {
	return provider.Transfer{ID: id, State: p.state, Reference: "UTR9"}, nil
}

type settlementRecorder struct {
	rows map[string]store.Settlement
	next int
}

func newRecorder() *settlementRecorder {
	return &settlementRecorder{rows: map[string]store.Settlement{}}
}

func (r *settlementRecorder) Record(_ context.Context, in store.Instruction) (store.Settlement, error) {
	for _, row := range r.rows {
		if row.PaymentID == in.PaymentID {
			return row, nil
		}
	}
	r.next++
	out := store.Settlement{
		ID: string(rune('a' + r.next)), PaymentID: in.PaymentID, LeaseID: in.LeaseID,
		Currency: in.Currency, Split: in.Split, State: store.SettlementPending,
		Provider: in.Provider, ExpectedOn: in.ExpectedOn,
	}
	r.rows[out.ID] = out
	return out, nil
}

func (r *settlementRecorder) ByID(_ context.Context, id string) (store.Settlement, error) {
	row, ok := r.rows[id]
	if !ok {
		return store.Settlement{}, store.ErrNoInstruction
	}
	return row, nil
}

func (r *settlementRecorder) ForPayment(_ context.Context, paymentID string) (store.Settlement, error) {
	for _, row := range r.rows {
		if row.PaymentID == paymentID {
			return row, nil
		}
	}
	return store.Settlement{}, store.ErrNoInstruction
}

func (r *settlementRecorder) Due(context.Context, time.Time) ([]store.Settlement, error) {
	out := make([]store.Settlement, 0, len(r.rows))
	for _, row := range r.rows {
		out = append(out, row)
	}
	return out, nil
}

func (r *settlementRecorder) Instructed(_ context.Context, id, ref string) error {
	row := r.rows[id]
	row.State, row.TransferRef = store.SettlementInstructed, ref
	r.rows[id] = row
	return nil
}

func (r *settlementRecorder) Settled(_ context.Context, id, ref string, on time.Time) error {
	row := r.rows[id]
	row.State, row.TransferRef, row.SettledOn = store.SettlementSettled, ref, on
	r.rows[id] = row
	return nil
}

func (r *settlementRecorder) Failed(_ context.Context, id, reason string) error {
	row := r.rows[id]
	row.State, row.Reason = store.SettlementFailed, reason
	r.rows[id] = row
	return nil
}

func settlements(t *testing.T, p *payoutProvider, acc merchant.Account) (*service.Settlements, *settlementRecorder) {
	t.Helper()
	reg := provider.NewRegistry()
	reg.Register(p)
	rec := newRecorder()
	return service.NewSettlements(rec, stubMerchants{account: acc}, reg), rec
}

type stubMerchants struct{ account merchant.Account }

func (s stubMerchants) ForProvider(context.Context, string) (merchant.Account, error) {
	if s.account.Provider == "" {
		return merchant.Account{}, store.ErrNoMerchant
	}
	return s.account, nil
}

func verified() merchant.Account {
	return merchant.Account{
		Provider: "fake", MerchantRef: "MRC-1", State: merchant.Verified,
		Settlement: merchant.Settlement{Currency: "INR"},
	}
}

func fee(t *testing.T) domain.Fee {
	t.Helper()
	f, err := domain.FeeSchedule{Rate: 299, TaxRate: 1800, RuleID: "r1"}.Charge(3200000)
	if err != nil {
		t.Fatalf("pricing: %v", err)
	}
	return f
}

func TestDividingACollectionRecordsEveryLegOnce(t *testing.T) {
	p := &payoutProvider{merchant: merchant.Verified, state: provider.TransferPending}
	s, rec := settlements(t, p, verified())

	terms := domain.SettlementTerms{ManagementRate: 800, TDSRate: 200, TDSAcknowledged: true}
	out, err := s.Divide(context.Background(), service.Collection{
		PaymentID: "pay-1", LeaseID: "lease-1", Provider: "fake", Fee: fee(t), Terms: terms,
	})
	if err != nil {
		t.Fatalf("dividing: %v", err)
	}
	if out.Split.Management != 256000 || out.Split.TDS != 64000 {
		t.Fatalf("divided = %+v", out.Split)
	}

	again, err := s.Divide(context.Background(), service.Collection{
		PaymentID: "pay-1", LeaseID: "lease-1", Provider: "fake", Fee: fee(t), Terms: terms,
	})
	if err != nil || again.ID != out.ID {
		t.Fatalf("a retried capture divided again: %+v, %v", again, err)
	}
	if len(rec.rows) != 1 {
		t.Fatalf("%d divisions for one collection", len(rec.rows))
	}
}

// The schedule is the provider's promise, so the date the owner is told to
// expect comes from the account, not from a constant in here.
func TestTheExpectedDateComesFromTheAccountsSchedule(t *testing.T) {
	acc := verified()
	acc.SettlementDays = 2
	p := &payoutProvider{merchant: merchant.Verified}
	s, _ := settlements(t, p, acc)

	out, err := s.Divide(context.Background(), service.Collection{
		PaymentID: "pay-2", Provider: "fake", Fee: fee(t),
		Terms:  domain.SettlementTerms{ManagementRate: 800},
		PaidOn: time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("dividing: %v", err)
	}
	if want := time.Date(2026, 3, 12, 0, 0, 0, 0, time.UTC); !out.ExpectedOn.Equal(want) {
		t.Fatalf("expected on %s; want T+2 = %s", out.ExpectedOn, want)
	}
}

func TestNothingIsCollectedIntoAnUnverifiedAccount(t *testing.T) {
	acc := verified()
	acc.State = merchant.Submitted
	p := &payoutProvider{}
	s, _ := settlements(t, p, acc)

	_, err := s.Divide(context.Background(), service.Collection{
		PaymentID: "pay-3", Provider: "fake", Fee: fee(t),
		Terms: domain.SettlementTerms{ManagementRate: 800},
	})
	if !errors.Is(err, merchant.ErrUnverified) {
		t.Fatalf("dividing into an unverified account = %v; want ErrUnverified", err)
	}
}

// Releasing sends the owner's leg and nothing else: the manager's fee and the
// platform's were never in the owner's money to begin with.
func TestReleasingSendsTheOwnersLegOnly(t *testing.T) {
	p := &payoutProvider{merchant: merchant.Verified, state: provider.TransferPending}
	s, rec := settlements(t, p, verified())

	out, err := s.Divide(context.Background(), service.Collection{
		PaymentID: "pay-4", Provider: "fake", Fee: fee(t),
		Terms: domain.SettlementTerms{ManagementRate: 800, TDSRate: 200, TDSAcknowledged: true},
	})
	if err != nil {
		t.Fatalf("dividing: %v", err)
	}

	if err := s.Release(context.Background(), out.ID, "BENE-1"); err != nil {
		t.Fatalf("releasing: %v", err)
	}
	if len(p.sent) != 1 || p.sent[0].Amount != out.Split.Owner {
		t.Fatalf("sent %+v; want one transfer of the owner's leg %s", p.sent, out.Split.Owner)
	}
	if p.sent[0].Purpose != provider.PurposeOwnerPayout {
		t.Fatalf("sent under purpose %q", p.sent[0].Purpose)
	}
	if rec.rows[out.ID].State != store.SettlementInstructed {
		t.Fatalf("state after release = %s", rec.rows[out.ID].State)
	}

	// Pressing release twice must not pay twice: the same idempotency key.
	if err := s.Release(context.Background(), out.ID, "BENE-1"); err != nil {
		t.Fatalf("releasing again: %v", err)
	}
	if len(p.sent) == 2 && p.sent[0].IdempotencyKey != p.sent[1].IdempotencyKey {
		t.Fatalf("a retry carried a different key: %q then %q", p.sent[0].IdempotencyKey, p.sent[1].IdempotencyKey)
	}
}

// Reconciliation is the provider's verdict applied to what we instructed. A
// mismatch becomes an exception the manager can see, never a silent adjustment.
func TestReconcileAppliesTheProvidersVerdict(t *testing.T) {
	p := &payoutProvider{merchant: merchant.Verified, state: provider.TransferPending}
	s, rec := settlements(t, p, verified())

	out, _ := s.Divide(context.Background(), service.Collection{
		PaymentID: "pay-5", Provider: "fake", Fee: fee(t),
		Terms: domain.SettlementTerms{ManagementRate: 800},
	})
	if err := s.Release(context.Background(), out.ID, "BENE-1"); err != nil {
		t.Fatalf("releasing: %v", err)
	}

	p.state = provider.TransferSettled
	if err := s.Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatalf("reconciling: %v", err)
	}
	if row := rec.rows[out.ID]; row.State != store.SettlementSettled || row.TransferRef != "UTR9" {
		t.Fatalf("after reconciliation = %+v; want settled against the UTR", row)
	}

	p.state = provider.TransferReturned
	if err := s.Reconcile(context.Background(), time.Now()); err != nil {
		t.Fatalf("reconciling a settled row: %v", err)
	}
	if rec.rows[out.ID].State != store.SettlementSettled {
		t.Fatalf("a settled division was moved again: %+v", rec.rows[out.ID])
	}
}
