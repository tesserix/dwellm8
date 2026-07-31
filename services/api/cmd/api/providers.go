package main

import (
	"log/slog"

	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/platform/config"
)

// The payment provider chain, built once at startup. ADR-0011 §1, ADR-0022 §5.
//
// This is the only place in the process that names an aggregator. Adding
// Razorpay is another case in the switch and a name in PAYMENT_PROVIDERS;
// nothing in money/domain changes, which is the property the seam exists for
// and the reason it is worth the indirection.
// Nothing consumes the registry yet — the collection service is #41 and the
// mandate service is #73. It is built at startup anyway, so a deployment's
// provider configuration is proven when the process starts rather than at the
// moment the first tenant tries to pay.
func paymentProviders(cfg config.Config) (*provider.Registry, error) {
	r := provider.NewRegistry() // offline is already in it, and is not optional

	if cfg.Cashfree.Configured() {
		cf, err := provider.NewCashfree(provider.CashfreeConfig{
			BaseURL:       cfg.Cashfree.BaseURL,
			ClientID:      cfg.Cashfree.ClientID,
			ClientSecret:  cfg.Cashfree.ClientSecret,
			WebhookSecret: cfg.Cashfree.WebhookSecret,
			APIVersion:    cfg.Cashfree.APIVersion,
		})
		if err != nil {
			return nil, err
		}
		r.Register(cf)
	}

	// A name in the chain that is not registered fails here. The typo this
	// catches — razropay, cashfre — is otherwise found by the first tenant to
	// try to pay on the first of the month.
	if err := r.SetChain(cfg.PaymentProviders...); err != nil {
		return nil, err
	}
	return r, nil
}

// logProviders states at startup what money will move through, because the
// alternative is reading a values file to find out.
//
// The sandbox line is a warning rather than an info: a production process
// pointed at test credentials cannot take real rent, and the failure mode is a
// tenant who believes they have paid. config.validate refuses this combination
// unless it has been acknowledged explicitly; this is what the acknowledgement
// looks like in the log afterwards.
func logProviders(logger *slog.Logger, cfg config.Config, r *provider.Registry) {
	logger.Info("payment providers", "chain", cfg.PaymentProviders)

	if !cfg.Cashfree.Configured() {
		return
	}
	fields := []any{
		"provider", provider.CashfreeName,
		"base_url", cfg.Cashfree.BaseURL,
		"api_version", cfg.Cashfree.APIVersion,
		"credentials", credentialKind(cfg),
	}
	if cfg.Cashfree.IsSandbox() && cfg.IsProd() {
		logger.Warn("cashfree is configured with sandbox credentials in a production process — "+
			"no real money can be collected", fields...)
		return
	}
	logger.Info("cashfree configured", fields...)
}

func credentialKind(cfg config.Config) string {
	if cfg.Cashfree.IsSandbox() {
		return "sandbox"
	}
	return "live"
}
