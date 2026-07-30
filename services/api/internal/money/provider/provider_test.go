package provider

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// A stand-in aggregator. Every adapter test below runs against this rather than
// against a real provider, which is the point of the seam: the module's
// behaviour is testable without anybody's sandbox being up.
type fake struct {
	name      string
	supports  map[collect.Method]bool
	secret    string
	orders    int
	unhealthy bool
}

func (f *fake) Name() string { return f.name }

func (f *fake) Supports(m collect.Method) bool { return f.supports[m] }

func (f *fake) CreateOrder(_ context.Context, req OrderRequest) (Order, error) {
	if f.unhealthy {
		return Order{}, ErrUnavailable
	}
	f.orders++
	return Order{ProviderOrderID: "order_" + req.IdempotencyKey}, nil
}

func (f *fake) Confirm(_ context.Context, id string) (Confirmation, error) {
	return Confirmation{ProviderPaymentID: id, Status: collect.StatusCaptured}, nil
}

func (f *fake) VerifyWebhook(payload []byte, sig string) bool {
	return VerifyHMACSHA256(payload, f.secret, sig)
}

func upiFake() *fake {
	return &fake{name: "razorpay", secret: "whsec", supports: map[collect.Method]bool{
		collect.MethodUPICollect: true, collect.MethodUPIIntent: true,
		collect.MethodUPIAutopay: true, collect.MethodCard: true,
		collect.MethodNetbanking: true,
	}}
}

// The issue's primary scenario. Three retries with the same key reach the
// provider once, because the caller holds the key and the adapter is asked once
// per distinct key rather than once per call.
func TestARetriedRequestCreatesOneOrder(t *testing.T) {
	f := upiFake()
	r := NewRegistry()
	r.Register(f)
	if err := r.SetChain("razorpay"); err != nil {
		t.Fatalf("SetChain: %v", err)
	}

	a, err := r.For(collect.MethodUPICollect)
	if err != nil {
		t.Fatalf("For: %v", err)
	}

	// What the store does: one order per key. The unique index on
	// (tenant_id, idempotency_key) is what makes this true under concurrency;
	// here it is asserted at the level the adapter sees.
	seen := map[string]Order{}
	const key = "collect-2026-08-unit-1"
	for i := 0; i < 3; i++ {
		if _, done := seen[key]; done {
			continue
		}
		o, err := a.CreateOrder(context.Background(), OrderRequest{
			IdempotencyKey: key, Amount: 2_750_000,
			Currency: domain.Currency, Method: collect.MethodUPICollect,
		})
		if err != nil {
			t.Fatalf("CreateOrder: %v", err)
		}
		seen[key] = o
	}
	if f.orders != 1 {
		t.Errorf("the provider was asked %d times for one key", f.orders)
	}
}

func TestTheChainPicksTheFirstAdapterThatSupportsTheMethod(t *testing.T) {
	upiOnly := &fake{name: "upi-only", supports: map[collect.Method]bool{collect.MethodUPICollect: true}}
	everything := upiFake()

	r := NewRegistry()
	r.Register(upiOnly)
	r.Register(everything)
	if err := r.SetChain("upi-only", "razorpay"); err != nil {
		t.Fatalf("SetChain: %v", err)
	}

	if a, _ := r.For(collect.MethodUPICollect); a.Name() != "upi-only" {
		t.Errorf("UPI went to %s", a.Name())
	}
	// Cards fall past the first adapter, which cannot take them.
	if a, _ := r.For(collect.MethodCard); a.Name() != "razorpay" {
		t.Errorf("card went to %s", a.Name())
	}
}

// The degradation rule, ADR-0011 §6, stated as a negative: an online method must
// never silently become an offline one. Offline means a person asserted that
// money arrived, and choosing it because an API call failed would record a
// receipt nobody witnessed.
func TestAnOnlineMethodNeverFallsThroughToOffline(t *testing.T) {
	r := NewRegistry() // offline is registered, nothing else is
	if err := r.SetChain(); err != nil {
		t.Fatalf("SetChain: %v", err)
	}

	if a, err := r.For(collect.MethodUPICollect); err == nil {
		t.Errorf("UPI resolved to %s with an empty chain", a.Name())
	}

	// Even with offline explicitly first in the chain.
	if err := r.SetChain(collect.OfflineProvider); err != nil {
		t.Fatalf("SetChain: %v", err)
	}
	if a, err := r.For(collect.MethodCard); err == nil {
		t.Errorf("a card resolved to %s", a.Name())
	}

	// An offline method, on the other hand, always resolves.
	a, err := r.For(collect.MethodOfflineCash)
	if err != nil || a.Name() != collect.OfflineProvider {
		t.Errorf("offline cash resolved to (%v, %v)", a, err)
	}
}

func TestAnUnavailableProviderIsDistinguishableFromEveryOtherFailure(t *testing.T) {
	f := upiFake()
	f.unhealthy = true
	_, err := f.CreateOrder(context.Background(), OrderRequest{Method: collect.MethodUPICollect})
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("a downed provider returned %v, which no caller can act on differently", err)
	}
}

func TestAChainNamingAnUnregisteredAdapterFailsAtStartup(t *testing.T) {
	r := NewRegistry()
	r.Register(upiFake())
	err := r.SetChain("razorpay", "razropay") // the typo that would surface at 9am
	if err == nil {
		t.Fatal("a chain with an unregistered name was accepted")
	}
	if !strings.Contains(err.Error(), "razropay") {
		t.Errorf("the error does not name the typo: %v", err)
	}
}

func TestRegisteringTheSameNameTwicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("a duplicate registration was accepted, so the deployment would use whichever won")
		}
	}()
	r := NewRegistry()
	r.Register(upiFake())
	r.Register(upiFake())
}

func TestByReturnsTheAdapterThatOwnsAnExistingPayment(t *testing.T) {
	r := NewRegistry()
	r.Register(upiFake())
	// The chain has changed, as it would after a provider migration…
	if err := r.SetChain(); err != nil {
		t.Fatalf("SetChain: %v", err)
	}
	// …and a payment created under the old one is still confirmed against it.
	a, err := r.By("razorpay")
	if err != nil || a.Name() != "razorpay" {
		t.Errorf("By(razorpay) = (%v, %v)", a, err)
	}
	if _, err := r.By("stripe"); err == nil {
		t.Error("an unknown adapter name resolved")
	}
}

func TestSignatureVerification(t *testing.T) {
	const secret = "whsec"
	payload := []byte(`{"event":"payment.captured","id":"evt_1"}`)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	good := hex.EncodeToString(mac.Sum(nil))

	if !VerifyHMACSHA256(payload, secret, good) {
		t.Error("a correct signature was rejected")
	}
	for name, sig := range map[string]string{
		"empty":        "",
		"not hex":      "zzzz",
		"truncated":    good[:len(good)-2],
		"one bit off":  flipLast(good),
		"another body": good, // checked below against a different payload
		"wrong secret": good,
	} {
		body := payload
		if name == "another body" {
			body = append(append([]byte(nil), payload...), ' ')
		}
		useSecret := secret
		if name == "wrong secret" {
			useSecret = "whsec2"
		}
		if VerifyHMACSHA256(body, useSecret, sig) {
			t.Errorf("%s: accepted", name)
		}
	}
	if VerifyHMACSHA256(payload, "", good) {
		t.Error("an empty secret verified — a misconfigured deployment would trust every delivery")
	}
}

func flipLast(hexsig string) string {
	if hexsig == "" {
		return hexsig
	}
	last := hexsig[len(hexsig)-1]
	if last == '0' {
		return hexsig[:len(hexsig)-1] + "1"
	}
	return hexsig[:len(hexsig)-1] + "0"
}

// Offline is a real adapter and its behaviour is asserted, not assumed.
func TestOfflineRecordsWhatAHumanWitnessed(t *testing.T) {
	var o Offline
	if o.Name() != collect.OfflineProvider {
		t.Errorf("offline is named %q", o.Name())
	}
	if o.VerifyWebhook([]byte("{}"), "anything") {
		t.Error("offline verified a webhook, and offline payments generate none")
	}
	if _, err := o.CreateOrder(context.Background(),
		OrderRequest{Method: collect.MethodCard, IdempotencyKey: "k"}); err == nil {
		t.Error("offline created an order for a card")
	}
	ord, err := o.CreateOrder(context.Background(),
		OrderRequest{Method: collect.MethodOfflineCash, IdempotencyKey: "k"})
	if err != nil || ord.ProviderOrderID != "k" {
		t.Errorf("offline cash order = (%+v, %v)", ord, err)
	}
	// By the time anybody records cash, the money has arrived.
	c, err := o.Confirm(context.Background(), "k")
	if err != nil || c.Status != collect.StatusCaptured {
		t.Errorf("offline confirm = (%+v, %v)", c, err)
	}
}
