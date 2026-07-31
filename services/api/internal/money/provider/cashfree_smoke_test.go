package provider

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
)

// The one test in this package that talks to Cashfree.
//
// Every other test runs against a recorded shape, which proves the adapter's
// logic and proves nothing about whether Cashfree agrees with it. The gap that
// leaves is the one integrations actually fail in: a field named slightly
// differently, an API version that wants a shape we are not sending, an account
// not enabled for something. This closes it, and skips without credentials so
// it costs a laptop nothing.
//
//	CASHFREE_BASE_URL=https://sandbox.cashfree.com/pg \
//	CASHFREE_CLIENT_ID=... CASHFREE_CLIENT_SECRET=... \
//	CASHFREE_API_VERSION=2023-08-01 \
//	go test ./internal/money/provider/ -run Smoke -v
//
// It refuses to run against live credentials. A smoke test that creates orders
// on a production merchant account is a smoke test that eventually creates one
// somebody has to explain.
func TestCashfreeSandboxSmoke(t *testing.T) {
	id, secret := os.Getenv("CASHFREE_CLIENT_ID"), os.Getenv("CASHFREE_CLIENT_SECRET")
	base, version := os.Getenv("CASHFREE_BASE_URL"), os.Getenv("CASHFREE_API_VERSION")
	if id == "" || secret == "" || base == "" || version == "" {
		t.Skip("CASHFREE_* not set — skipping the sandbox smoke test")
	}
	if !isSandboxCredential(id, secret) {
		t.Fatal("these are live credentials — this test creates real orders and will not run against them")
	}

	c, err := NewCashfree(CashfreeConfig{
		BaseURL: base, ClientID: id, ClientSecret: secret,
		WebhookSecret: secret, APIVersion: version,
		Timeout: 30 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewCashfree: %v", err)
	}

	ctx := context.Background()
	// Unique per run: the order id is our idempotency key and Cashfree refuses a
	// duplicate, which is the property we want in production and a nuisance in a
	// test that runs twice.
	key := "dwellm8-smoke-" + time.Now().UTC().Format("20060102T150405")

	order, err := c.CreateOrder(ctx, OrderRequest{
		IdempotencyKey: key,
		Amount:         1_200_000, // ₹12,000 — a rent that routes to UPI Autopay
		Currency:       domain.Currency,
		Method:         collect.MethodUPIIntent,
		Reference:      "Dwellm8 integration check",
		PayerName:      "Integration Check",
		PayerContact:   "9000000000",
		PayerRef:       "smoke-payer-0000-4000-8000-000000000001",
	})
	if err != nil {
		t.Fatalf("CreateOrder against the sandbox: %v", err)
	}
	if order.ProviderOrderID != key {
		t.Errorf("order id came back as %q, want our idempotency key %q", order.ProviderOrderID, key)
	}
	if order.PayToken == "" {
		t.Error("no payment session came back, so a payer's client would have nothing to open")
	}
	t.Logf("created order %s at %s", order.ProviderOrderID, base)

	// Confirming an order nobody has paid must be `created`, not a failure —
	// the distinction the reconciliation sweep depends on.
	conf, err := c.Confirm(ctx, order.ProviderOrderID)
	if err != nil {
		t.Fatalf("Confirm against the sandbox: %v", err)
	}
	if conf.Status != collect.StatusCreated {
		t.Errorf("an unattempted order confirmed as %s", conf.Status)
	}

	// The duplicate, which must be refused at their end as well as ours.
	if _, err := c.CreateOrder(ctx, OrderRequest{
		IdempotencyKey: key, Amount: 1_200_000, Currency: domain.Currency,
		Method: collect.MethodUPIIntent, PayerContact: "9000000000",
		PayerRef: "smoke-payer-0000-4000-8000-000000000001",
	}); err == nil {
		t.Error("Cashfree accepted a duplicate order id, so their uniqueness is not ours after all")
	}
}

func isSandboxCredential(id, secret string) bool {
	return len(id) >= 4 && id[:4] == "TEST" ||
		len(secret) >= 12 && secret[:12] == "cfsk_ma_test"
}
