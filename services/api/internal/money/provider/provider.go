// Package provider is the seam between the money module and whoever actually
// moves the money. ADR-0011 §1 and §5.
//
// The rule the package exists to enforce: no domain code names an aggregator.
// An adapter translates one provider's vocabulary into the canonical one in
// money/domain/collect and back, and adding a second aggregator is a new file
// here plus a line of configuration.
//
// Cashfree is the first real aggregator behind it (cashfree.go), and writing it
// found the one thing a seam validated against a single provider always hides:
// the original VerifyWebhook took a payload and a signature, and Cashfree signs
// a timestamp with the body, so that interface could not express it at all.
// ADR-0022 §3.
package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// OrderRequest is what the module asks an adapter to set up.
//
// IdempotencyKey is passed through to the provider where the provider supports
// one, and is the module's own key regardless. The two guarantees are different
// and both are needed: the provider's key stops a duplicate order at their end,
// and the unique index on (tenant_id, idempotency_key) stops a duplicate row at
// ours, including for providers that offer nothing.
type OrderRequest struct {
	IdempotencyKey string
	Amount         domain.Minor
	Currency       string
	Method         collect.Method
	// Reference is what the payer sees. Never a database id.
	Reference string
	PayerName string
	// PayerContact is a phone or an email, whichever the method needs.
	PayerContact string
}

// Order is what came back.
type Order struct {
	ProviderOrderID string
	// PayURL or PayToken is whatever the payer's client needs to proceed. Which
	// one is populated depends on the method, and neither is inspected here.
	PayURL   string
	PayToken string
}

// Confirmation is the provider's answer to "what is actually true about this
// payment" — the only thing permitted to move a payment's status.
type Confirmation struct {
	ProviderPaymentID string
	Status            collect.Status
	FailureCode       string
	// AmountMinor is what the provider says was collected, which must be checked
	// against what was asked for. A confirmation for a different amount is a
	// reconciliation problem, not a payment.
	AmountMinor domain.Minor
}

// Adapter is what every aggregator implements.
type Adapter interface {
	// Name is the value stored in payments.provider. Stable forever: it is data.
	Name() string

	// CreateOrder registers the intent with the provider.
	CreateOrder(ctx context.Context, req OrderRequest) (Order, error)

	// Confirm asks the provider what is true. ADR-0011 §4 — this is the call a
	// webhook triggers rather than replaces.
	Confirm(ctx context.Context, providerPaymentID string) (Confirmation, error)

	// VerifyWebhook reports whether a delivery really came from the provider.
	// Implementations must be constant-time.
	//
	// It takes the whole delivery rather than a payload and a string because the
	// providers do not agree on what is signed. Razorpay signs the body alone and
	// hex-encodes; Cashfree signs the timestamp concatenated with the body and
	// base64-encodes. An interface that passed only the payload could not express
	// the second at all — the timestamp is not recoverable from the body — so a
	// Cashfree adapter would have been unable to verify anything.
	VerifyWebhook(w Webhook) bool

	// Supports reports whether this adapter can handle a method at all, so the
	// registry can fall through to one that can.
	Supports(m collect.Method) bool
}

// Webhook is one delivery as it arrived, before anything has been believed
// about it.
//
// Body is the exact bytes read off the wire. Not a re-marshalled struct: both
// aggregators sign the raw payload, and JSON round-tripping reorders keys and
// normalises whitespace, so a handler that decodes before verifying breaks
// every signature — silently, and only against real traffic, because a test
// that builds the body and signs the same bytes never notices.
type Webhook struct {
	Body      []byte
	Signature string
	// Timestamp is whatever the provider put in its timestamp header, verbatim
	// and unparsed. Cashfree signs it as a string, so parsing it into a time and
	// formatting it back would change the bytes that get hashed.
	Timestamp string
}

// ErrUnavailable is what an adapter returns when the provider is down. It is
// distinguished from every other error on purpose: it is the one failure that
// degrades to offline recording rather than surfacing to the payer. ADR-0011 §6.
var ErrUnavailable = errors.New("provider: unavailable")

// Registry selects an adapter, mirroring the email-provider engine used
// elsewhere in the org: a named set, an ordered preference chain from
// configuration, and a fallback that is always present.
type Registry struct {
	mu       sync.RWMutex
	adapters map[string]Adapter
	chain    []string
}

// NewRegistry returns a registry with the offline adapter already in it. Offline
// is not optional: it is the last link of every chain, and a deployment that
// could lose its aggregator and had no way to record a cash payment would have
// no way to take rent.
func NewRegistry() *Registry {
	r := &Registry{adapters: map[string]Adapter{}}
	r.Register(Offline{})
	return r
}

// Register adds an adapter. Registering the same name twice is a programming
// error and panics at startup rather than silently taking the last one — a
// deployment that quietly used a different provider than its configuration says
// is the worst possible outcome here.
func (r *Registry) Register(a Adapter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, taken := r.adapters[a.Name()]; taken {
		panic(fmt.Sprintf("provider: %q registered twice", a.Name()))
	}
	r.adapters[a.Name()] = a
}

// SetChain fixes the preference order, e.g. from DWELLM8_PAYMENT_PROVIDERS.
// Every name must already be registered: a typo in configuration fails at
// startup, not at the first collection of the day.
func (r *Registry) SetChain(names ...string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, n := range names {
		if _, ok := r.adapters[n]; !ok {
			return fmt.Errorf("provider: chain names %q, which is not registered (have %s)",
				n, strings.Join(sortedNames(r.adapters), ", "))
		}
	}
	r.chain = append([]string(nil), names...)
	return nil
}

// By returns an adapter by name. Used when a payment already exists: it is
// confirmed and reconciled against the provider that created it, forever, no
// matter what the chain says today.
func (r *Registry) By(name string) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.adapters[name]
	if !ok {
		return nil, fmt.Errorf("provider: no adapter named %q (have %s)",
			name, strings.Join(sortedNames(r.adapters), ", "))
	}
	return a, nil
}

// For returns the first adapter in the chain that supports the method.
//
// It deliberately does not fall through to Offline for an online method. Offline
// means a human asserted that money arrived, and choosing it because an API call
// failed would record a receipt nobody witnessed. The degradation in ADR-0011 §6
// is a decision made above this layer, with a person in it.
func (r *Registry) For(m collect.Method) (Adapter, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if m.IsOffline() {
		return r.adapters[collect.OfflineProvider], nil
	}
	for _, name := range r.chain {
		if a := r.adapters[name]; a != nil && !isOffline(a) && a.Supports(m) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("provider: no adapter in the chain [%s] supports %s",
		strings.Join(r.chain, ", "), m)
}

func isOffline(a Adapter) bool { return a.Name() == collect.OfflineProvider }

func sortedNames(m map[string]Adapter) []string {
	out := make([]string, 0, len(m))
	for n := range m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Offline records money that moved without a provider in the path: cash to a
// caretaker, a cheque, a direct bank transfer the owner saw on their statement.
//
// It is a real adapter, not a null object. Its Confirm reports captured because
// that is what an offline payment is by the time anybody records it — the money
// has arrived, and the person recording it is the evidence.
type Offline struct{}

func (Offline) Name() string { return collect.OfflineProvider }

func (Offline) Supports(m collect.Method) bool { return m.IsOffline() }

func (Offline) CreateOrder(_ context.Context, req OrderRequest) (Order, error) {
	if !req.Method.IsOffline() {
		return Order{}, fmt.Errorf("provider: offline cannot create an order for %s", req.Method)
	}
	// There is nothing to register with anybody. The module's own idempotency
	// key is the order id, which keeps the column meaningful rather than null.
	return Order{ProviderOrderID: req.IdempotencyKey}, nil
}

func (Offline) Confirm(_ context.Context, providerPaymentID string) (Confirmation, error) {
	return Confirmation{ProviderPaymentID: providerPaymentID, Status: collect.StatusCaptured}, nil
}

// VerifyWebhook is false for every input: offline payments generate no webhooks,
// so anything claiming to be one is not from a provider that does not exist.
func (Offline) VerifyWebhook(Webhook) bool { return false }

// VerifyHMACSHA256 is the hex-over-body scheme — Razorpay's, and the shape most
// aggregators use.
//
// Constant-time comparison via crypto/subtle, because a byte-by-byte compare on
// a signature leaks the correct value one request at a time to anybody willing
// to measure. hmac.Equal does the same thing and is used for the digest; the
// hex decode is checked first so a malformed signature is rejected as malformed
// rather than as a mismatch.
func VerifyHMACSHA256(payload []byte, secret, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	want, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	return equalHMAC(payload, secret, want)
}

// ErrStaleDelivery is a delivery whose timestamp is outside the replay window.
// It is returned rather than folded into "not verified" because the two mean
// different things to whoever is looking: a bad signature is noise or an
// attacker guessing, and a correctly signed delivery arriving four hours late
// is either our own outage draining a queue or somebody replaying captured
// traffic. Both deserve to be parked; only one deserves a page.
var ErrStaleDelivery = errors.New("provider: a correctly signed delivery outside the replay window")

// VerifyHMACSHA256Base64WithTimestamp is Cashfree's scheme: HMAC-SHA256 over
// the timestamp concatenated with the raw body, base64-encoded, plus the replay
// window the timestamp exists for.
//
// The window is the reason this is not just an encoding difference. A signature
// over the body alone is valid forever, so a delivery captured once can be
// replayed at any point in the future and will verify. Signing the timestamp
// with it lets us refuse anything older than the window — and refusing is only
// possible because the interface carries the timestamp, which is the change
// that made this adapter expressible at all.
func VerifyHMACSHA256Base64WithTimestamp(w Webhook, secret string, now time.Time, window time.Duration) (bool, error) {
	if secret == "" || w.Signature == "" || w.Timestamp == "" {
		return false, nil
	}
	want, err := base64.StdEncoding.DecodeString(w.Signature)
	if err != nil {
		return false, nil
	}
	// Signed exactly as received: timestamp string, then body bytes, with nothing
	// between them and nothing normalised.
	signed := append([]byte(w.Timestamp), w.Body...)
	if !equalHMAC(signed, secret, want) {
		return false, nil
	}
	// Only now, with the delivery proven to be the provider's, is its timestamp
	// worth reading. Parsing an unverified attacker-supplied field first would be
	// working for them.
	secs, err := strconv.ParseInt(strings.TrimSpace(w.Timestamp), 10, 64)
	if err != nil {
		return false, nil
	}
	sent := time.Unix(secs, 0)
	if window > 0 {
		if delta := now.Sub(sent); delta > window || delta < -window {
			return true, fmt.Errorf("%w: signed at %s, now %s", ErrStaleDelivery,
				sent.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339))
		}
	}
	return true, nil
}

func equalHMAC(payload []byte, secret string, want []byte) bool {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return subtle.ConstantTimeCompare(mac.Sum(nil), want) == 1
}
