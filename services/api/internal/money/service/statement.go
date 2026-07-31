package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
)

// What a tenant is shown, and what they can prove. Issue #51.
//
// The whole of this file is reads, with one exception that is not here: taking
// the money is Payments.Collect, unchanged, because a tenant paying their own
// rent is the same act as a manager taking it and must not acquire a second
// code path with its own idea of idempotency.

// Statement is a tenancy's derived position. See store.Statement.
type Statement = store.Statement

// Charge is one ledger event on a tenancy. See store.Charge.
type Charge = store.Charge

// Statements reads what a tenancy owes and what has happened on it.
type Statements struct {
	ledger   *store.Ledger
	payments *store.Payments
	now      func() time.Time
}

// NewStatements wires the reader.
func NewStatements(l *store.Ledger, p *store.Payments, now func() time.Time) *Statements {
	if now == nil {
		now = time.Now
	}
	return &Statements{ledger: l, payments: p, now: now}
}

// Position returns what the tenancy owes today, broken down.
func (s *Statements) Position(ctx context.Context, leaseID, partyID string) (Statement, error) {
	return s.ledger.Statement(ctx, leaseID, partyID, s.now())
}

// History is everything that has happened on a tenancy, as the tenant sees it.
type History struct {
	Charges  []Charge
	Payments []collect.Payment
}

// History reads the charges and the payments together.
//
// Both, and in one call, because they are one story: an invoice the tenant does
// not recognise is answered by the payment beside it, and a screen that made
// them switch tabs to see the other half is a screen that generates a support
// ticket instead of a payment.
func (s *Statements) History(ctx context.Context, leaseID, partyID string, limit int) (History, error) {
	charges, err := s.ledger.Charges(ctx, leaseID, partyID, limit)
	if err != nil {
		return History{}, err
	}
	payments, err := s.payments.ByLease(ctx, leaseID, limit)
	if err != nil {
		return History{}, err
	}
	return History{Charges: charges, Payments: payments}, nil
}

// ErrNoReceipt is a payment that has not been received, so there is nothing to
// prove yet.
var ErrNoReceipt = errors.New("money: that payment has not been received")

// Receipt is the document a tenant downloads to prove they paid.
//
// It is derived rather than stored, and that is the point: a stored receipt is a
// second record of a payment which can disagree with the ledger, and the
// disagreement surfaces in the one conversation where the tenant is already
// upset. This is the payment, rendered.
type Receipt struct {
	// Number is the human reference — what somebody quotes on a phone call.
	Number      string
	PaymentID   string
	LeaseID     string
	AmountMinor domain.Minor
	Currency    string
	Method      collect.Method
	Provider    string
	// ProviderReference is the gateway's own id for the payment, so a dispute
	// can be traced without this system being the only witness.
	ProviderReference string
	// EntryID is the ledger entry the payment posted. A receipt that cannot
	// point at a posting is a receipt for money that never landed anywhere.
	EntryID    string
	Status     collect.Status
	ReceivedAt time.Time
}

// Receipt returns the receipt for a payment.
//
// Only a captured or settled payment has one. An authorised payment is money the
// bank has agreed to move and has not moved, and issuing a receipt for it is how
// a tenant ends up holding proof of a payment that later failed.
func (s *Statements) Receipt(ctx context.Context, paymentID string) (Receipt, error) {
	p, err := s.payments.ByID(ctx, paymentID)
	if err != nil {
		return Receipt{}, err
	}
	if p.Status != collect.StatusCaptured && p.Status != collect.StatusSettled {
		return Receipt{}, fmt.Errorf("%w: it is %s", ErrNoReceipt, p.Status)
	}

	received := p.CapturedAt
	if received.IsZero() {
		received = p.SettledAt
	}
	return Receipt{
		Number:            ReceiptNumber(p.ID, received),
		PaymentID:         p.ID,
		LeaseID:           p.Lease,
		AmountMinor:       p.Amount,
		Currency:          domain.Currency,
		Method:            p.Method,
		Provider:          p.Provider,
		ProviderReference: p.ProviderPaymentID,
		EntryID:           p.EntryID,
		Status:            p.Status,
		ReceivedAt:        received,
	}, nil
}

// ReceiptNumber derives the reference printed on a receipt.
//
// Derived from the payment's own id rather than drawn from a counter, and that
// is a deliberate trade. A counter gives a tidy sequence and needs a row, a
// lock and a decision about what happens when two organisations share one — and
// gives a number that means nothing outside this database anyway. This one is
// reproducible from the payment id by anybody holding it, which is what support
// actually needs, and it carries the date so a person can find the month.
//
// Twelve hex digits is the payment uuid's first six bytes — 48 bits, so two of
// one landlord's payments collide at around sixteen million of them. That is a
// display reference and is treated as one: every response carries the full
// payment id beside it, and that is what a dispute is traced by.
func ReceiptNumber(paymentID string, at time.Time) string {
	hex := strings.ReplaceAll(paymentID, "-", "")
	if len(hex) > 12 {
		hex = hex[:12]
	}
	return fmt.Sprintf("DW-%s-%s", at.UTC().Format("20060102"), strings.ToUpper(hex))
}
