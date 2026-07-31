package main

// A live probe against Cashfree's sandbox, through the adapter this service
// actually uses. Deliberately not a test: it needs the network and real
// credentials, and a test that needs those is one that fails in CI for reasons
// nobody caused.
//
//	export CASHFREE_CLIENT_ID="$(gcloud secrets versions access latest \
//	  --secret=prod-homechef-cashfree-test-app-id --project=tesseracthub-480811)"
//	export CASHFREE_CLIENT_SECRET="$(gcloud secrets versions access latest \
//	  --secret=prod-homechef-cashfree-test-secret-key --project=tesseracthub-480811)"
//	go run ./tools/cfprobe
//
// The credentials are HomeChef's Cashfree *test* pair, borrowed while Dwellm8's
// own merchant account is opened. They are sandbox credentials and they are
// still secrets: read them from Secret Manager at the moment of use, never into
// a file.

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
)

func main() {
	cf, err := provider.NewCashfree(provider.CashfreeConfig{
		BaseURL:      "https://sandbox.cashfree.com/pg",
		ClientID:     os.Getenv("CASHFREE_CLIENT_ID"),
		ClientSecret: os.Getenv("CASHFREE_CLIENT_SECRET"),
		APIVersion:   "2023-08-01",
	})
	if err != nil {
		fmt.Println("adapter:", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	ref := fmt.Sprintf("dwellm8probe%d", time.Now().Unix())
	order, err := cf.CreateOrder(ctx, provider.OrderRequest{
		IdempotencyKey: ref,
		Amount:         2750000, // ₹27,500 — a plausible rent
		Currency:       "INR",
		Method:         collect.MethodUPIIntent,
		Reference:      "Rent, August",
		PayerName:      "Probe Tenant",
		PayerContact:   "+919876543210",
		// Opaque, not the phone number: Cashfree rejects a leading +, and a
		// customer id keyed on a contact detail breaks when the tenant changes it.
		PayerRef: "probe_" + ref,
	})
	if err != nil {
		fmt.Println("create order:", err)
		os.Exit(1)
	}
	fmt.Printf("order created:  %s\n", order.ProviderOrderID)
	if order.PayToken != "" {
		fmt.Println("pay token:      (received)")
	}

	// The API-based confirmation. No webhook involved — this is the same call
	// the polling sweep makes.
	conf, err := cf.Confirm(ctx, order.ProviderOrderID)
	if err != nil {
		fmt.Println("confirm:", err)
		os.Exit(1)
	}
	fmt.Printf("polled status:  %s\n", conf.Status)
	fmt.Println("nobody has paid it, so `created` is the correct answer")
}
