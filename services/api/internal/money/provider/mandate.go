package provider

import (
	"context"
	"fmt"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/mandate"
)

// The mandate half of the seam.
//
// It is a second interface rather than five more methods on Adapter, and that
// is the whole design decision. Offline has no mandates — cash does not sign a
// standing authority — and widening Adapter would force it to implement five
// methods whose only correct body is a panic. An interface nobody can wrongly
// satisfy is worth more than one that is convenient to look up.
//
// The practical consequence: `registry.MandateFor(rail)` can fail, and the
// failure is a configuration problem rather than a runtime surprise, because a
// provider that cannot do mandates cannot be asked for one at compile time and
// cannot be resolved for one at run time.

// MandateRequest is what the module asks an adapter to register.
type MandateRequest struct {
	// IdempotencyKey is ours, not the provider's, and means the same thing it
	// means for a payment: retrying must not create a second authority. A
	// duplicate mandate is worse than a duplicate order — it debits a tenant
	// twice a month, forever, and looks legitimate from both ends.
	IdempotencyKey string

	Rail mandate.Rail
	// MaxAmount is the ceiling to authorise. It is fixed for the life of the
	// authority on every rail we use, so the caller decides here whether to leave
	// room for a rent escalation — and pays for that room in AFA if the headroom
	// crosses the AFA-free ceiling.
	MaxAmount domain.Minor
	Currency  string

	// Reference is what the payer sees in their bank or UPI app. Never a
	// database id: this is the string a tenant reads at 9pm deciding whether to
	// approve something.
	Reference    string
	PayerName    string
	PayerContact string
	// PayerVPA is the UPI handle for an Autopay registration. Empty for NACH,
	// where the bank account is collected by the provider's own flow.
	PayerVPA string

	FirstDebitOn string // ISO date; the rail schedules against it
	EndsOn       string // ISO date; empty means until revoked
}

// MandateRegistration is what came back. The payer has not authorised anything
// yet — this is the request they are about to be shown.
type MandateRegistration struct {
	ProviderMandateID string
	// AuthURL or AuthToken is whatever the payer's client needs to authorise.
	// Which one is populated depends on the rail, and neither is inspected here.
	AuthURL   string
	AuthToken string
}

// MandateConfirmation is the provider's answer to "is this authority real, and
// what is it". The only thing permitted to move a mandate's status.
type MandateConfirmation struct {
	ProviderMandateID string
	Status            mandate.Status
	FailureCode       string
	// MaxAmount is the ceiling the provider believes it registered. It is
	// checked against what was asked for: a mandate that came back with a
	// different ceiling is a reconciliation problem, not an authority.
	MaxAmount domain.Minor
}

// DebitRequest is one execution against a live authority.
type DebitRequest struct {
	IdempotencyKey    string
	ProviderMandateID string
	Amount            domain.Minor
	Currency          string
	Reference         string
	// NotifyOn is the pre-debit notification date the rail requires — 24 hours
	// ahead, and the aggregator sends it. It is passed rather than computed here
	// because the obligation is the rail's and the window is a rule-table value.
	NotifyOn string
}

// MandateAdapter is what an aggregator with mandates implements. Adapters
// without mandates do not, and must not.
type MandateAdapter interface {
	Adapter

	// SupportsRail reports whether this adapter can register a rail at all.
	SupportsRail(r mandate.Rail) bool

	// RegisterMandate sets up the authority the payer is about to approve.
	RegisterMandate(ctx context.Context, req MandateRequest) (MandateRegistration, error)

	// ConfirmMandate asks the provider what is true about an authority. This is
	// the call a mandate webhook triggers rather than replaces.
	ConfirmMandate(ctx context.Context, providerMandateID string) (MandateConfirmation, error)

	// Debit executes against a live authority and returns the resulting payment
	// at the provider — an ordinary collection from that point on.
	Debit(ctx context.Context, req DebitRequest) (Order, error)

	// Revoke ends the authority. It is idempotent: revoking a revoked mandate
	// succeeds, because the alternative is a tenant's cancellation failing on a
	// retry and appearing to have been refused.
	Revoke(ctx context.Context, providerMandateID string) error
}

// ErrNoMandates is returned when an adapter that has no mandates is asked for
// one. Distinguished from "unknown adapter" because the two have different
// fixes: one is a typo, the other is a provider that genuinely cannot do this.
var ErrNoMandates = fmt.Errorf("provider: this adapter registers no mandates")

// MandateBy returns the mandate adapter that owns an existing authority.
//
// By name, never by chain — the same rule as payments and for a sharper reason.
// A mandate lives for years; the chain will change under it, and asking the
// current head of the chain about an authority it never registered returns "no
// such mandate", which is indistinguishable from "the tenant revoked it".
func (r *Registry) MandateBy(name string) (MandateAdapter, error) {
	a, err := r.By(name)
	if err != nil {
		return nil, err
	}
	m, ok := a.(MandateAdapter)
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrNoMandates, name)
	}
	return m, nil
}

// MandateFor returns the first adapter in the chain that can register a rail.
//
// Offline is skipped explicitly as well as by interface, which is belt and
// braces on the one mistake that must never happen: an offline "mandate" would
// be a standing authority nobody granted.
func (r *Registry) MandateFor(rail mandate.Rail) (MandateAdapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, name := range r.chain {
		a := r.adapters[name]
		if a == nil || isOffline(a) {
			continue
		}
		if m, ok := a.(MandateAdapter); ok && m.SupportsRail(rail) {
			return m, nil
		}
	}
	return nil, fmt.Errorf("provider: no adapter in the chain [%s] registers %s mandates",
		joinNames(r.chain), rail)
}

func joinNames(names []string) string {
	out := ""
	for i, n := range names {
		if i > 0 {
			out += ", "
		}
		out += n
	}
	return out
}
