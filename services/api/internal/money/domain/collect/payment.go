// Package collect holds the canonical payment model. ADR-0011.
//
// A payment is what a provider was asked to do and what it said back. It is not
// the ledger: the ledger records what is true about the money, and this records
// an attempt to collect it. The two meet at EntryID, and only once the payment
// reaches a state that justifies a posting.
//
// It sits beside money/domain rather than inside it because the parent package
// already has a Payment — the ADR-0006 constructor that turns a receipt into a
// balanced entry. The two are genuinely different things, and the collision is
// the package boundary pointing at itself: domain.Payment is an event becoming
// postings, collect.Payment is a collection with a lifecycle.
//
// Nothing here names Razorpay. The vocabulary below is the one the domain speaks,
// every adapter maps its own into it, and a second aggregator is a new file in
// internal/money/provider rather than a change to anything in this package.
package collect

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
)

// Method is how the money moves. Offline is a first-class method rather than the
// absence of one: when the provider is down the tenant still pays, and the
// alternative to recording that is a caretaker's notebook.
type Method string

const (
	// MethodUPICollect is a request pushed to the payer's UPI app.
	MethodUPICollect Method = "upi_collect"
	// MethodUPIIntent is the payer opening their own app from a link or QR.
	MethodUPIIntent Method = "upi_intent"
	// MethodUPIAutopay is a debit under a standing UPI mandate — no payer
	// present. The authority itself lives in money/domain/mandate; this is what
	// one of its debits looks like once it is an ordinary collection.
	MethodUPIAutopay Method = "upi_autopay"
	// MethodNACHDebit is a debit under a NACH authority, electronic or paper.
	// It carries the rents UPI Autopay cannot: above the AFA-free ceiling every
	// UPI debit needs the payer's PIN, which is not autopay in any useful sense.
	MethodNACHDebit  Method = "nach_debit"
	MethodCard       Method = "card"
	MethodNetbanking Method = "netbanking"

	MethodOfflineCash     Method = "offline_cash"
	MethodOfflineCheque   Method = "offline_cheque"
	MethodOfflineTransfer Method = "offline_transfer"
)

var methods = map[Method]bool{
	MethodUPICollect: true, MethodUPIIntent: true, MethodUPIAutopay: true,
	MethodNACHDebit: true, MethodCard: true, MethodNetbanking: true,
	MethodOfflineCash: true, MethodOfflineCheque: true, MethodOfflineTransfer: true,
}

// IsOffline reports whether the money moved without a provider in the path. An
// offline payment has no provider ids and never receives a webhook, so the
// reconciliation sweep must not treat its silence as a stuck collection.
func (m Method) IsOffline() bool {
	switch m {
	case MethodOfflineCash, MethodOfflineCheque, MethodOfflineTransfer:
		return true
	}
	return false
}

// Methods returns every method, ordered, for the contract test.
func Methods() []Method {
	out := make([]Method, 0, len(methods))
	for m := range methods {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// Status is where a payment has got to.
type Status string

const (
	// StatusCreated is an order that exists at the provider and has not been
	// attempted. A payment is never absent — it is created before the payer is
	// ever shown anything, so a payer who pays against a screen this system has
	// forgotten is impossible.
	StatusCreated Status = "created"
	// StatusAttempted is the payer having done something. It means nothing about
	// whether money moved.
	StatusAttempted Status = "attempted"
	// StatusAuthorised is money held and not taken. Cards do this; UPI largely
	// does not. An authorisation that is never captured is released.
	StatusAuthorised Status = "authorised"
	// StatusCaptured is money taken. This is the state that posts to the ledger,
	// into gateway clearing — the provider has it, the bank does not.
	StatusCaptured Status = "captured"
	// StatusSettled is the provider having paid it onward to a real account.
	// ADR-0006's settlement entry, and the point the clearing balance clears.
	StatusSettled Status = "settled"

	StatusFailed    Status = "failed"
	StatusExpired   Status = "expired"
	StatusCancelled Status = "cancelled"
)

// The state machine, ADR-0011 §3. The same set exists as
// payment_transition_allowed() in the schema, and the contract test compares
// them pair by pair over the whole cross product.
var transitions = map[Status][]Status{
	StatusCreated:   {StatusAttempted, StatusFailed, StatusExpired, StatusCancelled},
	StatusAttempted: {StatusAuthorised, StatusCaptured, StatusFailed, StatusExpired},
	// An authorisation can be captured, can fail at capture, or can be released
	// deliberately. Both endings exist because the provider reports them
	// differently, and an owner asking why a collection did not happen needs the
	// difference between "the bank declined" and "we let it go".
	StatusAuthorised: {StatusCaptured, StatusFailed, StatusCancelled},
	StatusCaptured:   {StatusSettled},
	StatusSettled:    nil,
	StatusFailed:     nil,
	StatusExpired:    nil,
	StatusCancelled:  nil,
}

// IsTerminal reports whether nothing further can happen. Terminal states absorb:
// a late webhook for a failed payment cannot revive it.
func (s Status) IsTerminal() bool {
	next, known := transitions[s]
	return known && len(next) == 0
}

func (s Status) String() string { return string(s) }

// AwaitingPayer reports whether the payment is still waiting on the person
// paying it, and is therefore worth asking the provider about.
//
// Not the same as "not terminal", and the difference is the polling sweep's
// whole cost model: a captured payment is non-terminal — it can still settle or
// be refunded — and asking Cashfree about it changes nothing, because settlement
// is a reconciliation question (ADR-0012) rather than a payment-status one. So a
// sweep keyed on IsTerminal would re-ask about every captured payment forever.
func (s Status) AwaitingPayer() bool {
	return s == StatusCreated || s == StatusAttempted
}

// AwaitingPayerStatuses is the same set, for the query that has to express it in
// SQL. Two copies, one test — collect_test.go asserts they agree.
func AwaitingPayerStatuses() []Status {
	return []Status{StatusCreated, StatusAttempted}
}

// Statuses returns every status, ordered.
func Statuses() []Status {
	out := make([]Status, 0, len(transitions))
	for s := range transitions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// CanTransition reports whether a payment may move from one status to another.
//
// from == to is allowed, and it is the reason this design needs no delivery
// counter anywhere: the fifth copy of a webhook asks for the state the payment
// is already in, which is permitted and changes nothing.
func CanTransition(from, to Status) bool {
	if from == to {
		_, known := transitions[from]
		return known
	}
	for _, s := range transitions[from] {
		if s == to {
			return true
		}
	}
	return false
}

// Payment is one attempt to collect. It mirrors the payments table.
type Payment struct {
	ID       string
	TenantID string

	// Where the money is. A collection is always against something a tenant
	// occupies, so Property is required — unlike a ledger posting, which may
	// belong to the organisation itself.
	Property string
	Unit     string

	PayerKind domain.PartyKind
	PayerID   string

	Amount domain.Minor
	Method Method

	// Provider is the adapter's name — "razorpay", or "offline" for something a
	// human recorded. Never a provider-specific field.
	Provider          string
	ProviderOrderID   string
	ProviderPaymentID string

	Status      Status
	FailureCode string

	// MandateID is the standing authority this debit ran under, when it ran
	// under one. Empty for every collection a payer was present for, which is
	// most of them. The authority itself lives in money/domain/mandate.
	MandateID string

	// Lease is the tenancy this collection pays. Empty for a deposit or an ad-hoc
	// collection; where it is set, the entry the capture posts inherits it, which
	// is what puts a receipt into the lease position. ADR-0006 §5.
	Lease string

	// IdempotencyKey is the caller's key, unique within the organisation.
	IdempotencyKey string

	// EntryID is the ledger entry this payment caused, once it caused one.
	EntryID string

	CreatedAt    time.Time
	AuthorisedAt time.Time
	CapturedAt   time.Time
	SettledAt    time.Time
}

// ErrStaleTransition is what a caller checks for to tell "this webhook arrived
// late" — which is normal and is not an error condition for the system — from
// "this payment is wrong".
var ErrStaleTransition = errors.New("money: a delivery that arrived late, out of order, " +
	"or skipping a state does not move a payment")

// Validate asserts everything the schema will assert, before the schema is
// asked, and names the field rather than the transaction.
func (p Payment) Validate() error {
	if p.TenantID == "" {
		return errors.New("money: a payment with no organisation")
	}
	if p.Property == "" {
		return errors.New("money: a payment must name the property it collects against")
	}
	if p.Amount <= 0 {
		return fmt.Errorf("money: a payment of %s is not a collection", p.Amount)
	}
	if err := p.Amount.Valid(); err != nil {
		return err
	}
	if !methods[p.Method] {
		return fmt.Errorf("money: unknown payment method %q", p.Method)
	}
	if _, known := transitions[p.Status]; !known {
		return fmt.Errorf("money: unknown payment status %q", p.Status)
	}
	if p.PayerKind != domain.Tenant && p.PayerKind != domain.Owner {
		return fmt.Errorf("money: %q is not a payer", p.PayerKind)
	}
	if p.PayerID == "" {
		return errors.New("money: a payment with no payer is a receipt nobody can be given")
	}
	if p.IdempotencyKey == "" {
		return errors.New("money: a collection request without an idempotency key cannot be retried safely")
	}
	if p.Provider == "" {
		return errors.New("money: a payment must name the adapter that owns it")
	}
	// The schema's payments_captured_has_entry, checked here so the error names
	// the payment rather than the constraint.
	if (p.Status == StatusCaptured || p.Status == StatusSettled) && p.EntryID == "" {
		return fmt.Errorf("money: payment %s is %s and posted nothing — money the ledger does not know about",
			p.ID, p.Status)
	}
	if p.Method.IsOffline() && p.Provider != OfflineProvider {
		return fmt.Errorf("money: %s is an offline method and names provider %q", p.Method, p.Provider)
	}
	return nil
}

// OfflineProvider is the adapter name for money that moved without a provider in
// the path — cash to a caretaker, a cheque, a direct bank transfer.
const OfflineProvider = "offline"

// ApplyConfirmed moves the payment to a status that has been confirmed against
// the provider's API or its settlement file.
//
// This is the only writer of Status, and the argument it takes is deliberately
// not a webhook's opinion. ADR-0011 §4: a webhook is a hint that something
// changed, never the evidence that it did. Decide() is what a webhook produces;
// this is what a confirmation produces.
func (p *Payment) ApplyConfirmed(to Status, at time.Time) error {
	if _, known := transitions[to]; !known {
		return fmt.Errorf("money: unknown payment status %q", to)
	}
	if !CanTransition(p.Status, to) {
		return fmt.Errorf("%w: payment %s is %s and was told %s",
			ErrStaleTransition, p.ID, p.Status, to)
	}
	if p.Status == to {
		return nil // already applied; the common case for a redelivered webhook
	}
	p.Status = to
	switch to {
	case StatusAuthorised:
		p.AuthorisedAt = at
	case StatusCaptured:
		p.CapturedAt = at
	case StatusSettled:
		p.SettledAt = at
	}
	return nil
}

// Disposition is what to do with a webhook delivery.
type Disposition string

const (
	// Confirm means the delivery is plausible and the provider must now be
	// asked what is actually true. It is not "apply this".
	Confirm Disposition = "confirm"
	// Ignore means the delivery says what is already recorded.
	Ignore Disposition = "ignore"
	// Park means the delivery is kept and not acted on, for a human or the
	// reconciliation sweep. Never "drop".
	Park Disposition = "park"
)

// ParkReason mirrors the schema's CHECK.
type ParkReason string

const (
	ParkUnknownPayment   ParkReason = "unknown_payment"
	ParkSignatureInvalid ParkReason = "signature_invalid"
	ParkStaleTransition  ParkReason = "stale_transition"
	ParkUnsupportedEvent ParkReason = "unsupported_event"
)

// Delivery is one webhook, already normalised by the adapter.
type Delivery struct {
	Provider          string
	EventID           string
	EventType         string
	SignatureVerified bool
	// ProviderPaymentID is what the delivery claims to be about.
	ProviderPaymentID string
	// Claimed is the status the provider's message asserts. It is a claim, and
	// the name says so.
	Claimed Status
}

// Decision is what the handler does.
type Decision struct {
	Disposition Disposition
	Reason      ParkReason
	// Confirm is the status worth confirming — what to expect when the provider
	// is asked. It is not written anywhere on its own.
	Confirm Status
}

// Decide is the whole of the webhook rule. ADR-0011 §4.
//
// current is nil when no payment matches the delivery. Note what this function
// cannot do: it has no path that writes a status. Every outcome is ignore, park,
// or go and ask — which is what makes "a webhook alone never moves money" a
// property of the design rather than a discipline someone has to remember.
func Decide(d Delivery, current *Payment) Decision {
	// Order matters. An unverified delivery is not inspected further: its
	// contents are an attacker's if the signature failed, so the payment it
	// names is not looked up and nothing about it is trusted.
	if !d.SignatureVerified {
		return Decision{Disposition: Park, Reason: ParkSignatureInvalid}
	}
	if _, known := transitions[d.Claimed]; !known || d.Claimed == "" {
		return Decision{Disposition: Park, Reason: ParkUnsupportedEvent}
	}
	if current == nil {
		// The provider believes it collected money for a payment this system has
		// never heard of. Dropping it loses that; guessing an organisation for it
		// is a cross-tenant write.
		return Decision{Disposition: Park, Reason: ParkUnknownPayment}
	}
	if current.Status == d.Claimed {
		return Decision{Disposition: Ignore}
	}
	if !CanTransition(current.Status, d.Claimed) {
		// The out-of-order case: `captured` arriving after the `settled` that
		// followed it. Parked rather than ignored, because it is also what a
		// replay attack looks like and the two deserve the same scrutiny.
		return Decision{Disposition: Park, Reason: ParkStaleTransition}
	}
	return Decision{Disposition: Confirm, Confirm: d.Claimed}
}
