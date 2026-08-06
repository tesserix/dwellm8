package service

import (
	"context"
	"fmt"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
)

// Settlements divides a collection and pays the owner's leg out (#270).
//
// The division happens once, at capture, and is written down; everything after
// it — the payout run, the owner statement, the reconciliation — reads that
// row. Which provider carries the money is a name on the instruction, so the
// same workflow runs over Cashfree, Razorpay or Stripe: the adapter is asked
// for a transfer and its verdict, and nothing here decides money has moved.
type Settlements struct {
	store     SettlementStore
	merchants MerchantReader
	registry  *provider.Registry
}

// SettlementStore is the store as this service uses it.
type SettlementStore interface {
	Record(ctx context.Context, in store.Instruction) (store.Settlement, error)
	ByID(ctx context.Context, id string) (store.Settlement, error)
	ForPayment(ctx context.Context, paymentID string) (store.Settlement, error)
	Due(ctx context.Context, on time.Time) ([]store.Settlement, error)
	Instructed(ctx context.Context, id, transferRef string) error
	Settled(ctx context.Context, id, reference string, on time.Time) error
	Failed(ctx context.Context, id, reason string) error
}

// MerchantReader is the merchant store as this service uses it: where rent
// settles, and how quickly the provider promised it would.
type MerchantReader interface {
	ForProvider(ctx context.Context, provider string) (merchant.Account, error)
}

func NewSettlements(s SettlementStore, m MerchantReader, r *provider.Registry) *Settlements {
	return &Settlements{store: s, merchants: m, registry: r}
}

// ErrNoInstruction is re-exported so callers match one error.
var ErrNoInstruction = store.ErrNoInstruction

// Division is one collection split, as anything outside the module sees it.
// The store's row does not travel: a surface reads this vocabulary, not tables.
type Division struct {
	ID          string
	PaymentID   string
	LeaseID     string
	Currency    string
	Split       domain.Split
	State       string
	Provider    string
	TransferRef string
	ExpectedOn  time.Time
	SettledOn   time.Time
	Reason      string
}

func division(s store.Settlement) Division {
	return Division{
		ID: s.ID, PaymentID: s.PaymentID, LeaseID: s.LeaseID, Currency: s.Currency,
		Split: s.Split, State: string(s.State), Provider: s.Provider,
		TransferRef: s.TransferRef, ExpectedOn: s.ExpectedOn, SettledOn: s.SettledOn,
		Reason: s.Reason,
	}
}

// Collection is one captured payment, with the terms that divide it.
type Collection struct {
	PaymentID string
	LeaseID   string
	Provider  string
	// Fee is the platform's leg as it was priced at capture; it is not
	// recomputed here (ADR-0031 §3).
	Fee    domain.Fee
	Terms  domain.SettlementTerms
	PaidOn time.Time
}

// Divide prices one collection and files the division. Capture is retried and
// the division must not be: the store returns the first one unchanged.
func (s *Settlements) Divide(ctx context.Context, c Collection) (Division, error) {
	account, err := s.merchants.ForProvider(ctx, c.Provider)
	if err != nil {
		return Division{}, err
	}
	if !account.State.MayCollect() {
		return Division{}, fmt.Errorf("%w: %s is %s",
			merchant.ErrUnverified, c.Provider, account.State)
	}

	split, err := c.Terms.Split(c.Fee)
	if err != nil {
		return Division{}, err
	}

	paid := c.PaidOn
	if paid.IsZero() {
		paid = time.Now().UTC()
	}
	out, err := s.store.Record(ctx, store.Instruction{
		PaymentID: c.PaymentID, LeaseID: c.LeaseID,
		Currency: account.Settlement.Currency, Split: split,
		Provider: c.Provider, ExpectedOn: account.SettlesOn(paid),
	})
	if err != nil {
		return Division{}, err
	}
	return division(out), nil
}

// Queue is what the manager is holding on somebody else's behalf: everything
// divided and not yet settled by the date the schedule promised.
func (s *Settlements) Queue(ctx context.Context, on time.Time) ([]Division, error) {
	rows, err := s.store.Due(ctx, on)
	if err != nil {
		return nil, err
	}
	out := make([]Division, 0, len(rows))
	for _, row := range rows {
		out = append(out, division(row))
	}
	return out, nil
}

// ByID reads one division.
func (s *Settlements) ByID(ctx context.Context, id string) (Division, error) {
	row, err := s.store.ByID(ctx, id)
	if err != nil {
		return Division{}, err
	}
	return division(row), nil
}

// Release sends the owner's leg to the provider. Only the owner's: the
// manager's fees and the platform's were never in the owner's money.
func (s *Settlements) Release(ctx context.Context, id, beneficiaryRef string) error {
	row, err := s.store.ByID(ctx, id)
	if err != nil {
		return err
	}
	if row.State == store.SettlementSettled {
		return nil
	}

	payouts, err := provider.PayoutsBy(s.registry, row.Provider)
	if err != nil {
		return err
	}
	account, err := s.merchants.ForProvider(ctx, row.Provider)
	if err != nil {
		return err
	}

	transfer, err := payouts.Transfer(ctx, provider.TransferRequest{
		// The instruction is the key, so a second press pays the same payout.
		IdempotencyKey:  "settlement:" + row.ID,
		Amount:          row.Split.Owner,
		Currency:        row.Currency,
		BeneficiaryRef:  beneficiaryRef,
		Purpose:         provider.PurposeOwnerPayout,
		Narration:       "Rent settlement",
		FromMerchantRef: account.MerchantRef,
	})
	if err != nil {
		return fmt.Errorf("release settlement %s: %w", row.ID, err)
	}

	switch transfer.State {
	case provider.TransferFailed, provider.TransferReturned:
		return s.store.Failed(ctx, row.ID, refusal(transfer))
	case provider.TransferSettled:
		return s.store.Settled(ctx, row.ID, reference(transfer), time.Now().UTC())
	default:
		return s.store.Instructed(ctx, row.ID, transfer.ID)
	}
}

// Reconcile applies the provider's verdict to everything already instructed. A
// payout the provider will not make becomes the manager's exception, not a
// silent adjustment.
func (s *Settlements) Reconcile(ctx context.Context, on time.Time) error {
	due, err := s.store.Due(ctx, on)
	if err != nil {
		return err
	}
	for _, row := range due {
		if row.State != store.SettlementInstructed || row.TransferRef == "" {
			continue
		}
		payouts, err := provider.PayoutsBy(s.registry, row.Provider)
		if err != nil {
			return err
		}
		transfer, err := payouts.TransferState(ctx, row.TransferRef)
		if err != nil {
			return fmt.Errorf("reconcile settlement %s: %w", row.ID, err)
		}
		switch transfer.State {
		case provider.TransferSettled:
			err = s.store.Settled(ctx, row.ID, reference(transfer), on)
		case provider.TransferFailed, provider.TransferReturned:
			err = s.store.Failed(ctx, row.ID, refusal(transfer))
		default:
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// reference is what a bank statement will show, which is what reconciliation
// matches on; the provider's own transfer id is the fallback.
func reference(t provider.Transfer) string {
	if t.Reference != "" {
		return t.Reference
	}
	return t.ID
}

func refusal(t provider.Transfer) string {
	if t.Reason != "" {
		return t.Reason
	}
	return "the provider reported the payout as " + t.State.String()
}
