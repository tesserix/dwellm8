package config

import (
	"strings"
	"testing"
)

// Configuration is where the sandbox-in-production slip lives, and it has no
// symptom until a tenant pays: the checkout works, the provider takes a test
// payment, and the owner is never paid. So the guard is tested the way a money
// rule is tested rather than the way a getter is.

func sandboxEnv() map[string]string {
	return map[string]string{
		"APP_ENV":                "prod",
		"DATABASE_URL":           "postgres://localhost/dwellm8",
		"PAYMENT_PROVIDERS":      "cashfree,offline",
		"CASHFREE_BASE_URL":      "https://sandbox.cashfree.com/pg",
		"CASHFREE_CLIENT_ID":     "TEST1234567890abcdef",
		"CASHFREE_CLIENT_SECRET": "cfsk_ma_test_0123456789abcdef",
		"CASHFREE_API_VERSION":   "2023-08-01",
	}
}

func load(t *testing.T, env map[string]string) (Config, error) {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	return Load()
}

func TestProductionRefusesSandboxCredentialsUnlessItIsSaidOutLoud(t *testing.T) {
	_, err := load(t, sandboxEnv())
	if err == nil {
		t.Fatal("a production process started against Cashfree's sandbox without anybody saying so")
	}
	if !strings.Contains(err.Error(), "CASHFREE_ALLOW_SANDBOX_IN_PROD") {
		t.Errorf("the error does not say how to proceed deliberately: %v", err)
	}

	env := sandboxEnv()
	env["CASHFREE_ALLOW_SANDBOX_IN_PROD"] = "true"
	cfg, err := load(t, env)
	if err != nil {
		t.Fatalf("the acknowledged case was still refused: %v", err)
	}
	if !cfg.Cashfree.IsSandbox() {
		t.Error("TEST credentials were not recognised as sandbox")
	}

	// Outside production the same credentials need no ceremony: that is what a
	// sandbox is for.
	env = sandboxEnv()
	env["APP_ENV"] = "uat"
	if _, err := load(t, env); err != nil {
		t.Errorf("uat against the sandbox was refused: %v", err)
	}
}

// The credential decides, not a flag beside it. A boolean in a values file is a
// claim about the secret, and the two drift the first time one is rotated.
func TestSandboxIsReadFromTheCredential(t *testing.T) {
	for name, tc := range map[string]struct {
		id, secret string
		sandbox    bool
	}{
		"both test":        {"TEST1234", "cfsk_ma_test_abc", true},
		"live":             {"1234567890abcdef", "cfsk_ma_prod_abc", false},
		"test id only":     {"TEST1234", "cfsk_ma_prod_abc", true},
		"test secret only": {"1234567890abcdef", "cfsk_ma_test_abc", true},
	} {
		c := Cashfree{ClientID: tc.id, ClientSecret: tc.secret}
		if got := c.IsSandbox(); got != tc.sandbox {
			t.Errorf("%s: IsSandbox = %v", name, got)
		}
	}
}

// A chain naming an aggregator that is not configured, and credentials nothing
// routes to. Both are silent in production and they fail in opposite directions.
func TestTheChainAndTheCredentialsMustAgree(t *testing.T) {
	env := sandboxEnv()
	delete(env, "CASHFREE_CLIENT_SECRET")
	t.Setenv("CASHFREE_CLIENT_SECRET", "")
	_, err := load(t, env)
	if err == nil {
		t.Fatal("the chain named cashfree with no client secret and started anyway")
	}
	if !strings.Contains(err.Error(), "CASHFREE_CLIENT_SECRET") {
		t.Errorf("the error does not name the missing variable: %v", err)
	}

	env = sandboxEnv()
	env["PAYMENT_PROVIDERS"] = "offline"
	env["CASHFREE_ALLOW_SANDBOX_IN_PROD"] = "true"
	if _, err := load(t, env); err == nil {
		t.Error("cashfree was fully configured and routed to by nothing, and that started quietly")
	}
}

// Offline is the last link of every chain whether or not a deployment remembers
// it. A chain with no way to record a cash payment cannot take rent when the
// aggregator is down. ADR-0011 §6.
func TestOfflineIsAlwaysInTheChain(t *testing.T) {
	env := sandboxEnv()
	env["PAYMENT_PROVIDERS"] = "cashfree"
	env["CASHFREE_ALLOW_SANDBOX_IN_PROD"] = "true"

	cfg, err := load(t, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !contains(cfg.PaymentProviders, "offline") {
		t.Errorf("chain is %v with no offline in it", cfg.PaymentProviders)
	}
	if cfg.PaymentProviders[len(cfg.PaymentProviders)-1] != "offline" {
		t.Errorf("offline is not last in %v — it is the fallback, not a preference",
			cfg.PaymentProviders)
	}
}

// Cashfree signs deliveries with the merchant secret key, so the webhook secret
// defaults to it — and stays a separate field, because that is a fact about
// their current scheme rather than something to hard-code.
func TestTheWebhookSecretDefaultsToTheClientSecret(t *testing.T) {
	env := sandboxEnv()
	env["CASHFREE_ALLOW_SANDBOX_IN_PROD"] = "true"
	cfg, err := load(t, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cashfree.WebhookSecret != cfg.Cashfree.ClientSecret {
		t.Error("the webhook secret did not default to the client secret, so every delivery would be rejected")
	}

	env["CASHFREE_WEBHOOK_SECRET"] = "a-separate-one"
	cfg, err = load(t, env)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Cashfree.WebhookSecret != "a-separate-one" {
		t.Error("an explicit webhook secret was overridden by the default")
	}
}
