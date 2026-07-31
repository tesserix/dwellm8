// Package mandate holds the standing authority a tenant gives to be debited.
//
// A mandate is not a payment, and the reason it needs its own package is that
// the two disagree about time. A payment is one attempt that resolves in
// minutes and never moves backwards; a mandate is an authority that lives for
// the length of a tenancy, is paused and resumed on purpose, and produces many
// payments. ADR-0011 modelled `upi_autopay` as an ordinary method because the
// mandate behind it did not exist yet, and that was honest at the time —
// nothing named a mandate anywhere in the module.
//
// The vocabulary here names no aggregator. Razorpay calls the authority a token
// and Cashfree calls it a subscription; both are a ProviderMandateID.
package mandate

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// Rail is the payment rail the authority runs on. It is not a collect.Method:
// a method is how one payment moved, a rail is what the standing authority was
// registered against, and the debits it produces are ordinary payments.
type Rail string

const (
	// RailUPIAutopay is an NPCI UPI Autopay mandate. Cheap, instant to set up,
	// and capped — see rails.go, which is where the caps live.
	RailUPIAutopay Rail = "upi_autopay"
	// RailENACH is an electronic NACH mandate authorised by netbanking, debit
	// card or Aadhaar. Days to activate, and carries rent that UPI cannot.
	RailENACH Rail = "enach"
	// RailPhysicalNACH is a signed paper mandate. It exists because a tenant
	// whose bank does neither of the above is otherwise unbankable by this
	// platform, and "unbankable" means their landlord keeps using a notebook.
	RailPhysicalNACH Rail = "nach_physical"
)

var rails = map[Rail]bool{RailUPIAutopay: true, RailENACH: true, RailPhysicalNACH: true}

// Rails returns every rail, ordered, for the contract test.
func Rails() []Rail {
	out := make([]Rail, 0, len(rails))
	for r := range rails {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (r Rail) String() string { return string(r) }

// IsUnattended reports whether a debit on this rail happens with nobody
// present. It is the property the product actually cares about, and it is a
// function of the rail plus the amount — see rails.go. A rail alone cannot
// answer it, which is why this is deliberately not a method on Rail.

// Status is where the authority has got to.
type Status string

const (
	// StatusCreated is registered with the provider and not yet presented to the
	// payer. The mandate exists before the tenant is shown anything, for the same
	// reason a payment does: a tenant who authorises against a screen this system
	// has forgotten is impossible.
	StatusCreated Status = "created"
	// StatusPending is the payer having been asked and not yet answered. On UPI
	// this lasts thirty minutes. On physical NACH it lasts five to ten working
	// days, which is the whole reason this state is not a transient.
	StatusPending Status = "pending"
	// StatusActive is an authority that may be debited.
	StatusActive Status = "active"
	// StatusPaused is a live authority that must not be debited right now — a
	// tenant on a payment holiday, a dispute, an owner asking us to stop. The
	// authority is intact and the tenant did not have to revoke anything.
	StatusPaused Status = "paused"

	// StatusRejected is the bank or the payer declining the registration. It is
	// distinct from expired because an owner asking why autopay never started
	// needs the difference between "your tenant's bank said no" and "your tenant
	// never answered".
	StatusRejected Status = "rejected"
	// StatusRevoked is the authority deliberately ended, by either side.
	StatusRevoked Status = "revoked"
	// StatusExpired is the authority reaching its end date, or the payer never
	// answering the registration request.
	StatusExpired Status = "expired"
)

// The state machine.
//
// It is deliberately **not** forward-only, which is the one place this package
// contradicts ADR-0011 §3 rather than copying it. A payment is forward-only
// because money moves once and a backwards transition is always either a bug or
// an attack. An authority is a different animal: pausing and resuming is a
// product feature, and a tenant who resumes after a payment holiday must not be
// made to re-authorise a mandate that was never revoked.
//
// What survives from ADR-0011 is everything that matters for correctness:
// terminal states absorb, self-transition is a permitted no-op so a redelivered
// webhook needs no counter, and nothing outside ApplyConfirmed writes Status.
var transitions = map[Status][]Status{
	StatusCreated: {StatusPending, StatusRejected, StatusExpired, StatusRevoked},
	StatusPending: {StatusActive, StatusRejected, StatusExpired, StatusRevoked},
	StatusActive:  {StatusPaused, StatusRevoked, StatusExpired},
	StatusPaused:  {StatusActive, StatusRevoked, StatusExpired},

	StatusRejected: nil,
	StatusRevoked:  nil,
	StatusExpired:  nil,
}

// Statuses returns every status, ordered, for the drift check against the
// schema's mandate_transition_allowed().
func Statuses() []Status {
	out := make([]Status, 0, len(transitions))
	for s := range transitions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (s Status) String() string { return string(s) }

// IsTerminal reports whether nothing further can happen. A revoked mandate
// cannot be revived by a late delivery claiming it is active.
func (s Status) IsTerminal() bool {
	next, known := transitions[s]
	return known && len(next) == 0
}

// IsDebitable reports whether a debit may be attempted. Only one state says
// yes, and the scheduler asks this rather than comparing strings.
func (s Status) IsDebitable() bool { return s == StatusActive }

// CanTransition reports whether an authority may move from one status to
// another. from == to is allowed, and is what the fifth copy of a webhook asks
// for.
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

// Mandate is the standing authority. It mirrors the mandates table.
type Mandate struct {
	ID       string
	TenantID string

	// Where the authority applies. A mandate is always against a unit somebody
	// occupies — unlike a ledger posting, and like a payment, except that Unit is
	// required here. A mandate for a whole property would authorise debits for
	// tenancies it was never shown to.
	Property string
	Unit     string

	PayerKind domain.PartyKind
	PayerID   string

	// LeaseID is the tenancy this authority was taken for. A mandate that
	// outlives its lease is a live authority to debit somebody who moved out, so
	// the link exists to make expiry checkable rather than remembered.
	LeaseID string

	Rail Rail

	// MaxAmount is the ceiling the payer authorised. It is fixed at registration
	// on every rail we use: a rent escalation past it needs a new authority, or
	// an amendment where the provider supports one. Debits above it fail at the
	// rail, so the scheduler checks it before asking.
	MaxAmount domain.Minor

	// Provider is the adapter's name. Stable forever: a mandate is registered,
	// confirmed, debited and revoked against the provider that created it, no
	// matter what the chain says after a migration.
	Provider          string
	ProviderMandateID string

	Status       Status
	FailureCode  string
	FirstDebitOn time.Time
	// EndsOn is the authority's own expiry, which is not the lease's. A mandate
	// that outlives its tenancy is a live authority to debit somebody who has
	// moved out.
	EndsOn time.Time

	CreatedAt   time.Time
	ActivatedAt time.Time
	EndedAt     time.Time
}

// ErrStaleTransition distinguishes "this delivery arrived late" — normal, and
// not a fault in the system — from "this mandate is wrong".
var ErrStaleTransition = errors.New("money: a delivery that arrived late or out of order does not move a mandate")

// Validate asserts what the schema will assert, and names the field rather than
// the constraint.
func (m Mandate) Validate() error {
	if m.TenantID == "" {
		return errors.New("money: a mandate with no organisation")
	}
	if m.Property == "" {
		return errors.New("money: a mandate must name the property it authorises debits against")
	}
	if m.Unit == "" {
		// Stricter than payments on purpose. ADR-0009's assertion 6 requires a
		// unit-identifying table's policy to use is_delegated_unit(); a nullable
		// unit here would be a standing authority whose scope is a whole building.
		return errors.New("money: a mandate must name the unit — an authority over a whole property is not one a tenant gave")
	}
	if !rails[m.Rail] {
		return fmt.Errorf("money: unknown mandate rail %q", m.Rail)
	}
	if _, known := transitions[m.Status]; !known {
		return fmt.Errorf("money: unknown mandate status %q", m.Status)
	}
	if m.MaxAmount <= 0 {
		return fmt.Errorf("money: a mandate with a ceiling of %s authorises nothing", m.MaxAmount)
	}
	if err := m.MaxAmount.Valid(); err != nil {
		return err
	}
	if m.PayerKind != domain.Tenant {
		// An owner does not give a standing authority to be debited rent. If that
		// changes, it changes here and in the policy, together.
		return fmt.Errorf("money: %q does not give a rent mandate", m.PayerKind)
	}
	if m.PayerID == "" {
		return errors.New("money: a mandate with no payer authorises nobody")
	}
	if m.Provider == "" {
		return errors.New("money: a mandate must name the adapter that owns it")
	}
	if m.Status == StatusActive && m.ProviderMandateID == "" {
		return fmt.Errorf("money: mandate %s is active with no provider id — an authority nobody can be asked about", m.ID)
	}
	if !m.EndsOn.IsZero() && !m.FirstDebitOn.IsZero() && m.EndsOn.Before(m.FirstDebitOn) {
		return fmt.Errorf("money: mandate %s ends before its first debit", m.ID)
	}
	return nil
}

// CanDebit reports whether this authority covers an amount, and says why not
// when it does not. The scheduler calls it before asking the provider, because
// a debit above the ceiling fails at the rail and a failed debit is a message
// to a tenant.
func (m Mandate) CanDebit(amount domain.Minor) error {
	if !m.Status.IsDebitable() {
		return fmt.Errorf("money: mandate %s is %s", m.ID, m.Status)
	}
	if amount <= 0 {
		return fmt.Errorf("money: a debit of %s is not a collection", amount)
	}
	if amount > m.MaxAmount {
		return fmt.Errorf("money: mandate %s authorises %s and was asked for %s — the ceiling is fixed at registration",
			m.ID, m.MaxAmount, amount)
	}
	return nil
}

// ApplyConfirmed moves the mandate to a status confirmed against the provider's
// API. As with a payment, this is the only writer of Status and it takes a
// confirmed status rather than a claimed one: a webhook is a hint that the
// authority may have changed, never the evidence that it did.
func (m *Mandate) ApplyConfirmed(to Status, at time.Time) error {
	if _, known := transitions[to]; !known {
		return fmt.Errorf("money: unknown mandate status %q", to)
	}
	if !CanTransition(m.Status, to) {
		return fmt.Errorf("%w: mandate %s is %s and was told %s",
			ErrStaleTransition, m.ID, m.Status, to)
	}
	if m.Status == to {
		return nil // already applied; the redelivered webhook
	}
	m.Status = to
	switch to {
	case StatusActive:
		if m.ActivatedAt.IsZero() {
			// Set once. A pause and resume does not re-date the authority — the
			// tenant authorised it when they authorised it.
			m.ActivatedAt = at
		}
	case StatusRevoked, StatusExpired, StatusRejected:
		m.EndedAt = at
	}
	return nil
}

// Delivery is one mandate webhook, already normalised by the adapter. It is the
// mandate-shaped twin of collect.Delivery and shares its park vocabulary, so
// the inbox stores one kind of row and the sweep reads one kind of reason.
type Delivery struct {
	Provider          string
	EventID           string
	EventType         string
	SignatureVerified bool
	ProviderMandateID string
	// Claimed is the status the provider's message asserts. A claim, and the
	// name says so.
	Claimed Status
}

// Decision is what the handler does. It reuses collect's disposition and park
// vocabularies deliberately: the rule is the same rule, and a second copy of it
// would drift.
type Decision struct {
	Disposition collect.Disposition
	Reason      collect.ParkReason
	Confirm     Status
}

// Decide is ADR-0011 §4 applied to a mandate delivery, in the same order and
// for the same reasons. Note what it cannot do: no path here writes a status.
//
// current is nil when no mandate matches the delivery.
func Decide(d Delivery, current *Mandate) Decision {
	// Signature first. An unverified delivery is not inspected further and the
	// mandate it names is not looked up, so a prober learns nothing about which
	// authorities exist.
	if !d.SignatureVerified {
		return Decision{Disposition: collect.Park, Reason: collect.ParkSignatureInvalid}
	}
	if _, known := transitions[d.Claimed]; !known || d.Claimed == "" {
		return Decision{Disposition: collect.Park, Reason: collect.ParkUnsupportedEvent}
	}
	if current == nil {
		// The provider believes a tenant authorised something this system has
		// never seen. Dropping it loses that; guessing an organisation is a
		// cross-tenant write.
		return Decision{Disposition: collect.Park, Reason: collect.ParkUnknownPayment}
	}
	if current.Status == d.Claimed {
		return Decision{Disposition: collect.Ignore}
	}
	if !CanTransition(current.Status, d.Claimed) {
		return Decision{Disposition: collect.Park, Reason: collect.ParkStaleTransition}
	}
	return Decision{Disposition: collect.Confirm, Confirm: d.Claimed}
}
