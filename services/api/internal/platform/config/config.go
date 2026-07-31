// Package config reads the API's configuration from the environment.
//
// Everything the process needs is read once, at start, and validated before
// the server binds. A missing required value is a startup failure with the
// variable named — never a nil dereference on the first request that needs it.
package config

import (
	"fmt"
	"github.com/tesserix/dwellm8/services/api/internal/platform/httpx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/workflow"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config is the whole of the API's configuration.
type Config struct {
	Env         string // dev, uat, prod
	Port        int    // HTTP listen port
	DatabaseURL string // PostgreSQL DSN as dwellm8_api, from the CNPG app secret
	// PlatformDatabaseURL connects as dwellm8_platform — the role the policies
	// exempt. One path needs it: the webhook inbox, which is written before
	// anybody knows whose money the delivery concerns. Defaults to DatabaseURL
	// in dev, where both are the same database and neither is privileged.
	PlatformDatabaseURL string
	ShutdownGrace       time.Duration // how long in-flight requests get on SIGTERM
	LogLevel            string
	SandboxOrgSlugs     []string // organisations that hold demonstration data (M19)

	// PaymentProviders is the ADR-0011 chain, in preference order. Offline is
	// appended if it is missing rather than being required of every deployment:
	// a chain with no way to record a cash payment is a deployment that cannot
	// take rent when the aggregator is down.
	PaymentProviders []string
	Cashfree         Cashfree

	// Identity is ADR-0027: the GIP project every token must be minted for, and
	// the prefix that names this product's user pools.
	Identity Identity

	// RateLimits are per replica, not per fleet. The effective limit is these
	// numbers times the replica count, which is stated rather than discovered —
	// see internal/platform/httpx on why the limiter is in process.
	RateLimits RateLimits

	// Events is the ADR-0002 backbone.
	Events Events

	// Workflow is ADR-0015's engine. The API needs it to start an operation and
	// the worker needs it to serve one; both refuse to run a money operation
	// without it, rather than falling back to doing it inline.
	Workflow workflow.Config
}

// Events configures the outbox relay and the broker it drains to. ADR-0002.
type Events struct {
	// NATSURL is empty when there is no broker, which is a supported state: the
	// outbox still collects and nothing a user does fails. Publishing resumes
	// when a URL appears, because the backlog is in PostgreSQL rather than in
	// a process's memory.
	NATSURL string
	// RelayEnabled runs the drain loop in this process. Off in the jobs binary,
	// where a batch run has no business holding the broker connection.
	RelayEnabled bool
	RelayBatch   int
	RelayEvery   time.Duration
}

// Configured reports whether a broker is reachable enough to try.
func (e Events) Configured() bool { return e.NATSURL != "" }

// Identity configures token verification. ADR-0027.
//
// No secret here on purpose: verifying a GIP token needs Google's *public*
// certificates and the project id, both of which are public knowledge. A service
// that needed a private key to check a signature would be a service whose ability
// to authenticate could be stolen.
type Identity struct {
	// ProjectID is both the issuer's suffix and the audience. Checking it is what
	// stops a valid token for another Google project being accepted here.
	ProjectID string
	// TenantPrefix names the pools: "dwellm8" gives dwellm8-own and the rest.
	TenantPrefix string
	// Enforce turns the middleware on. False only in dev, and validate() refuses
	// a deployment that leaves it false outside dev — an API that forgot to
	// authenticate is one that works perfectly for everybody.
	Enforce bool

	// ImpersonateOrg is the organisation every request acts as while
	// authentication is off, so the apps and the endpoints can be built against
	// each other before the GIP tenants exist (issue #229).
	//
	// No default, deliberately: a deployment must not acquire an impersonated
	// organisation by forgetting to configure one, and with Enforce true this is
	// ignored entirely.
	ImpersonateOrg string

	// ImpersonateResident is the renter's mobile number every tenant-surface
	// request acts as while authentication is off. ADR-0029 §5.
	//
	// A number rather than a party id, because that is what a developer has: the
	// one they typed into the lease they are testing against. Unset means the
	// resident routes answer 503 rather than serving somebody arbitrary — an
	// impersonation that picked the first renter it found would show one tenant's
	// dues to whoever opened the page.
	ImpersonateResident string
}

// RateLimits configures the three limiters. Issue #228, and issue #51 for the
// third.
//
// Three, because they fail differently: a per-tenant limit stops one
// organisation taking the service down for the rest, a per-route one covers what
// is reachable without a tenant at all — the webhook endpoint, where an attacker
// has no identity to be limited by — and a per-sign-in one covers the tenant
// surface, whose callers are people rather than organisations and send no
// organisation header to be keyed by.
type RateLimits struct {
	// Tenant is the per-organisation allowance on the request path.
	Tenant httpx.Limit
	// Resident is the per-sign-in allowance on the tenant surface. Small: one
	// person on one phone, refreshing their dues and paying once. A generous
	// allowance here buys nothing and widens what a stolen token can scrape.
	Resident httpx.Limit
	// Webhook is the unkeyed allowance on the provider callback route. Higher
	// burst than the tenant limit and a faster refill: a real aggregator
	// legitimately delivers in bursts, and shedding its retries is worse than
	// the load.
	Webhook httpx.Limit
}

// Cashfree is the aggregator's configuration. ADR-0022 §5.
//
// There are no defaults on the first four fields on purpose. A defaulted base
// URL makes production the thing you get by forgetting to configure anything,
// and a defaulted API version makes the deployment's behaviour change on
// Cashfree's release day rather than ours.
type Cashfree struct {
	BaseURL      string
	ClientID     string
	ClientSecret string
	APIVersion   string
	// WebhookSecret verifies deliveries. Cashfree signs with the merchant's
	// secret key, so this is normally the client secret and is defaulted to it —
	// separate anyway, because "they are the same value today" is a fact about
	// Cashfree's current scheme rather than something to hard-code.
	WebhookSecret string
	// AllowSandboxInProd is the explicit acknowledgement that a production
	// deployment is pointed at test credentials. See validate().
	AllowSandboxInProd bool
}

// Configured reports whether enough is present to build the adapter.
func (c Cashfree) Configured() bool {
	return c.BaseURL != "" && c.ClientID != "" && c.ClientSecret != "" && c.APIVersion != ""
}

// IsSandbox reports whether these are test credentials, from the credential
// itself rather than from a flag somebody sets alongside it.
//
// Cashfree prefixes both: an app id starts with TEST and a secret key with
// cfsk_ma_test_. Reading the credential is the only version of this check that
// cannot disagree with reality — a boolean in a values file is a claim about
// the secret, and the two drift the first time somebody rotates one.
func (c Cashfree) IsSandbox() bool {
	return strings.HasPrefix(c.ClientID, "TEST") ||
		strings.Contains(c.ClientSecret, "_test_")
}

// Load reads configuration from the environment.
func Load() (Config, error) {
	c := Config{
		Env:                 get("APP_ENV", "dev"),
		LogLevel:            get("LOG_LEVEL", "info"),
		DatabaseURL:         os.Getenv("DATABASE_URL"),
		PlatformDatabaseURL: get("PLATFORM_DATABASE_URL", os.Getenv("DATABASE_URL")),
		ShutdownGrace:       20 * time.Second,
	}

	port, err := strconv.Atoi(get("PORT", "8080"))
	if err != nil {
		return Config{}, fmt.Errorf("PORT must be a number, got %q", os.Getenv("PORT"))
	}
	c.Port = port

	if slugs := os.Getenv("SANDBOX_ORG_SLUGS"); slugs != "" {
		c.SandboxOrgSlugs = strings.Split(slugs, ",")
	}

	c.Cashfree = Cashfree{
		BaseURL:            os.Getenv("CASHFREE_BASE_URL"),
		ClientID:           os.Getenv("CASHFREE_CLIENT_ID"),
		ClientSecret:       os.Getenv("CASHFREE_CLIENT_SECRET"),
		APIVersion:         os.Getenv("CASHFREE_API_VERSION"),
		WebhookSecret:      get("CASHFREE_WEBHOOK_SECRET", os.Getenv("CASHFREE_CLIENT_SECRET")),
		AllowSandboxInProd: os.Getenv("CASHFREE_ALLOW_SANDBOX_IN_PROD") == "true",
	}

	c.Identity = Identity{
		ProjectID:      os.Getenv("GIP_PROJECT_ID"),
		TenantPrefix:   get("GIP_TENANT_PREFIX", "dwellm8"),
		Enforce:        get("AUTH_ENFORCE", "true") == "true",
		ImpersonateOrg: os.Getenv("DEV_IMPERSONATE_ORG"),

		ImpersonateResident: os.Getenv("DEV_IMPERSONATE_RESIDENT"),
	}

	c.RateLimits = RateLimits{
		Tenant:   httpx.Limit{Burst: envInt("RATE_LIMIT_TENANT_BURST", 120), Every: envDur("RATE_LIMIT_TENANT_EVERY_MS", 500)},
		Resident: httpx.Limit{Burst: envInt("RATE_LIMIT_RESIDENT_BURST", 30), Every: envDur("RATE_LIMIT_RESIDENT_EVERY_MS", 1000)},
		Webhook:  httpx.Limit{Burst: envInt("RATE_LIMIT_WEBHOOK_BURST", 300), Every: envDur("RATE_LIMIT_WEBHOOK_EVERY_MS", 100)},
	}

	c.Workflow = workflow.Config{
		HostPort:  os.Getenv("TEMPORAL_HOSTPORT"),
		Namespace: get("TEMPORAL_NAMESPACE", workflow.Namespace),
		TLS:       os.Getenv("TEMPORAL_TLS") == "true",
	}

	c.Events = Events{
		NATSURL:      os.Getenv("NATS_URL"),
		RelayEnabled: get("EVENTS_RELAY_ENABLED", "true") == "true",
		RelayBatch:   envInt("EVENTS_RELAY_BATCH", 100),
		RelayEvery:   envDur("EVENTS_RELAY_EVERY_MS", 1000),
	}

	c.PaymentProviders = splitList(get("PAYMENT_PROVIDERS", "offline"))
	if !contains(c.PaymentProviders, "offline") {
		c.PaymentProviders = append(c.PaymentProviders, "offline")
	}

	if grace := os.Getenv("SHUTDOWN_GRACE_SECONDS"); grace != "" {
		s, err := strconv.Atoi(grace)
		if err != nil {
			return Config{}, fmt.Errorf("SHUTDOWN_GRACE_SECONDS must be a number, got %q", grace)
		}
		c.ShutdownGrace = time.Duration(s) * time.Second
	}

	return c, c.validate()
}

// validate fails startup rather than the first request that needs the value.
func (c Config) validate() error {
	if c.Env != "dev" && c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required outside dev")
	}
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("PORT out of range: %d", c.Port)
	}
	// An API that forgot to authenticate does not fail — it works, for everybody.
	// So the check is at startup and the failure is the process, not a request.
	if c.Env != "dev" {
		if !c.Identity.Enforce {
			return fmt.Errorf("AUTH_ENFORCE is false outside dev — every request would be anonymous")
		}
		if c.Identity.ProjectID == "" {
			return fmt.Errorf("GIP_PROJECT_ID is required outside dev: without it no token can be verified")
		}
	}
	return c.validateCashfree()
}

func (c Config) validateCashfree() error {
	named := contains(c.PaymentProviders, "cashfree")

	// A chain naming an aggregator that is not configured fails here rather than
	// at the first collection of the month. ADR-0011 §1: the typo is discovered
	// at startup or it is discovered by a tenant.
	if named && !c.Cashfree.Configured() {
		missing := []string{}
		for name, v := range map[string]string{
			"CASHFREE_BASE_URL":      c.Cashfree.BaseURL,
			"CASHFREE_CLIENT_ID":     c.Cashfree.ClientID,
			"CASHFREE_CLIENT_SECRET": c.Cashfree.ClientSecret,
			"CASHFREE_API_VERSION":   c.Cashfree.APIVersion,
		} {
			if v == "" {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		return fmt.Errorf("PAYMENT_PROVIDERS names cashfree and %s %s not set",
			strings.Join(missing, ", "), plural(len(missing)))
	}

	// Configured but not in the chain is the opposite mistake and just as quiet:
	// credentials mounted, nothing routing to them, and every collection falling
	// to whatever is next.
	if c.Cashfree.Configured() && !named {
		return fmt.Errorf("cashfree is configured and PAYMENT_PROVIDERS is [%s] — "+
			"credentials nothing routes to are credentials nobody notices are unused",
			strings.Join(c.PaymentProviders, ", "))
	}

	// The one this deployment actually needs today. Dwellm8 is pointed at
	// Cashfree's sandbox while the merchant account is being set up, and a
	// production process quietly taking test payments is the worst version of
	// that: a tenant who has "paid" and an owner who has not been paid.
	//
	// The check reads the credential rather than trusting a flag, so it cannot be
	// satisfied by editing a values file, and the acknowledgement has to be
	// written down where a reviewer sees it.
	if named && c.IsProd() && c.Cashfree.IsSandbox() && !c.Cashfree.AllowSandboxInProd {
		return fmt.Errorf("APP_ENV=prod with Cashfree sandbox credentials (client id begins TEST): " +
			"set CASHFREE_ALLOW_SANDBOX_IN_PROD=true to say that is deliberate, " +
			"or point CASHFREE_CLIENT_ID and CASHFREE_CLIENT_SECRET at the live merchant account")
	}
	return nil
}

func plural(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// IsProd reports whether this process is serving real money.
func (c Config) IsProd() bool { return c.Env == "prod" }

func get(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envInt reads a positive integer, falling back rather than failing: a limit
// typed wrongly in a values file must not stop the service booting, and a limit
// that silently became zero would shed every request.
func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return fallback
	}
	return n
}

// envDur reads a refill interval in milliseconds.
func envDur(key string, fallbackMillis int) time.Duration {
	return time.Duration(envInt(key, fallbackMillis)) * time.Millisecond
}
