// Command api is the one Dwellm8 API.
//
// Per ADR-0001 this is a modular monolith: eight modules — identity, property,
// lease, money, maintenance, community, discovery and notify — in one process,
// one database and one transaction boundary. Modules are wired here and
// nowhere else, so the dependency direction is visible in a single file.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leasehttp "github.com/tesserix/dwellm8/services/api/internal/lease/http"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	moneyhttp "github.com/tesserix/dwellm8/services/api/internal/money/http"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/config"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events/natsx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/httpx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/surface/resident"
)

// version is stamped at build time: -ldflags "-X main.version=$(git rev-parse --short HEAD)"
var version = "dev"

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)
	logger.Info("starting", "version", version, "env", cfg.Env, "port", cfg.Port)

	providers, err := paymentProviders(cfg)
	if err != nil {
		return fmt.Errorf("payment providers: %w", err)
	}
	logProviders(logger, cfg, providers)

	// The request pool connects as dwellm8_api — ADR-0003 §3's request role,
	// never the owner. Row-level security is what isolates one organisation from
	// another, and it only applies to a role FORCE covers.
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database pool: %w", err)
	}
	defer pool.Close()

	// Readiness depends on the database; liveness does not. A process that
	// cannot reach PostgreSQL should stop receiving traffic, not be killed and
	// restarted into the same outage.
	health := httpx.NewHealth(version, func(ctx context.Context) error {
		return pool.Ping(ctx)
	})

	// The platform pool is a second connection as dwellm8_platform, and it is a
	// distinct type so it cannot be passed where the request pool is expected.
	// The webhook inbox is the only thing that gets it: a delivery arrives before
	// anybody knows whose money it is about. ADR-0011 §5.
	platformPool, err := pgxpool.New(context.Background(), cfg.PlatformDatabaseURL)
	if err != nil {
		return fmt.Errorf("platform database pool: %w", err)
	}
	defer platformPool.Close()

	// The outbox relay, ADR-0002 §4. A goroutine in this process rather than its
	// own deployment: the backlog lives in PostgreSQL, so a replica that dies
	// mid-drain loses nothing and the survivors pick the rows up.
	//
	// No broker configured is a supported state — events accumulate and publish
	// when one appears. A broker that is down must never fail a tenant's payment.
	relayCtx, stopRelay := context.WithCancel(context.Background())
	defer stopRelay()
	var eventsConn *natsx.Conn
	if cfg.Events.Configured() && cfg.Events.RelayEnabled {
		conn, err := natsx.Connect(natsx.Config{URL: cfg.Events.NATSURL, Name: "dwellm8-api"}, logger)
		if err != nil {
			return fmt.Errorf("events transport: %w", err)
		}
		defer conn.Close()

		ensureCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err = conn.EnsureStreams(ensureCtx)
		cancel()
		if err != nil {
			return fmt.Errorf("events streams: %w", err)
		}

		relay := events.NewRelay(tenancy.NewPlatformPool(platformPool), conn, logger, events.RelayConfig{
			BatchSize: cfg.Events.RelayBatch,
			Interval:  cfg.Events.RelayEvery,
		})
		go func() {
			if err := relay.Run(relayCtx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("outbox relay stopped", "error", err)
			}
		}()
		logger.Info("outbox relay running", "nats", cfg.Events.NATSURL)
		eventsConn = conn
	} else {
		logger.Warn("outbox relay is off; events accumulate unpublished",
			"nats_configured", cfg.Events.Configured(), "relay_enabled", cfg.Events.RelayEnabled)
	}

	paymentsStore := moneystore.NewPayments(pool)
	payments := moneyservice.NewPayments(
		paymentsStore,
		moneystore.NewInbox(tenancy.NewPlatformPool(platformPool)),
		providers, logger)
	statements := moneyservice.NewStatements(moneystore.NewLedger(pool), paymentsStore, nil)

	// Identity's two resolvers. The principals store is shared: a sign-in and a
	// renter's sign-in are the same table asked different questions.
	principals := identitystore.New(tenancy.NewPlatformPool(platformPool))
	residents := identityservice.NewResidents(principals, logger)

	// The lease module learns to identify a party by mobile number, which is how
	// a renter acquires an identity: their landlord types the number weeks
	// before they ever open the app. ADR-0029 §2.
	leases := leaseservice.NewLeases(leasestore.New(pool), logger).WithResidents(residents)

	// Rate limiting, issue #228. Two limiters because they fail differently: the
	// per-tenant one stops one organisation taking the service down for the rest,
	// and the webhook route is limited unkeyed because its caller is
	// unauthenticated by definition and has no identity to be limited by.
	//
	// Health is outside both. A readiness probe shed as "too many requests" takes
	// the pod out of rotation at exactly the moment it is busiest, which turns a
	// load spike into an outage.
	// ADR-0027. Authentication wraps the module routes and not the health ones:
	// a probe carrying no token must still answer, or the pod is removed from
	// rotation for the crime of not being a signed-in user.
	//
	// The verifier is built even when enforcement is off, so a dev process is
	// the same shape as a production one — the only difference is whether the
	// middleware is in the chain.
	verifier := &auth.Verifier{
		ProjectID:    cfg.Identity.ProjectID,
		TenantPrefix: cfg.Identity.TenantPrefix,
		Keys:         auth.NewGoogleKeys(),
		Leeway:       30 * time.Second,
	}

	// The ADR-0020 guard. Configured-but-dark is the rollout state: the store
	// and model bootstrap, every route's check is declared and compiled, and
	// AUTHZ_ENFORCE=true is the day the answers start to count — after the
	// tuple pipeline (#151) has something to answer with.
	guard := &authz.Guard{
		Enforce: cfg.Authz.Enforce,
		Log:     logger,
		Cache:   authz.NewCache(cfg.Authz.CacheTTL, 10_000),
	}
	if cfg.Authz.URL != "" {
		fga := authz.NewClient(cfg.Authz.URL)
		bctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		err := fga.Bootstrap(bctx, cfg.Authz.Store)
		cancel()
		if err != nil {
			// Enforcing means the process is wrong without its checker; dark
			// means the deploy that brings OpenFGA up will be the one that
			// starts answering.
			if cfg.Authz.Enforce {
				return fmt.Errorf("authz bootstrap: %w", err)
			}
			logger.Warn("authz bootstrap failed; guard stays dark", "error", err)
		}
		guard.Checker = fga
		logger.Info("authz ready", "store", cfg.Authz.Store, "enforce", cfg.Authz.Enforce)

		// The tuple projector, #151: relationship-bearing facts drain from the
		// lease stream into the graph. Durable, so a dead replica resumes where
		// it stopped; idempotent, because delivery is at-least-once.
		if eventsConn != nil {
			ensureCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			cons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
				Name:     "authz-tupler",
				Stream:   "DWELLM8_LEASE",
				Subjects: []string{"dwellm8.lease.tenancy.>"},
			})
			cancel()
			if err != nil {
				return fmt.Errorf("authz projector consumer: %w", err)
			}
			proj := &authz.Projector{FGA: fga, Log: logger}
			go func() {
				if err := natsx.Consume(relayCtx, cons, proj.Handle, logger); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("authz projector stopped", "error", err)
				}
			}()
			logger.Info("authz projector running", "consumer", "authz-tupler")
		}
	} else {
		logger.Warn("authz is not configured; route checks are declared but dark",
			"set", "AUTHZ_URL")
	}

	// Routes that a person authenticates for.
	protected := http.NewServeMux()
	leasehttp.NewLeases(leases, logger).Routes(authz.NewRegistrar(protected, guard))

	// The impersonated-owner control, #227. Changes fail closed without the
	// fingerprint key; the payout run (#80) is the reader.
	payoutAccounts := moneyservice.NewPayoutAccounts(
		moneystore.NewPayoutAccounts(pool), cfg.PayoutFingerprintKey, logger)
	moneyhttp.NewPayoutAccounts(payoutAccounts, logger).Routes(authz.NewRegistrar(protected, guard))

	// The tenant view, on its own tree. Issue #51, ADR-0029.
	//
	// Separate because it resolves a sign-in differently: the ordinary resolver
	// answers 409 for a person in two organisations, and a renter with two
	// landlords is exactly that — the ordinary case for them, not an error. It
	// also carries its own surface check, so a genuine Ops token presented here
	// is refused before any query runs.
	residentMux := http.NewServeMux()
	resident.New(leases, statements, payments, logger, nil).Routes(authz.NewRegistrar(residentMux, guard))

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Readyz)

	// The webhook route is deliberately outside authentication: a provider
	// delivery carries an HMAC signature over its own bytes (ADR-0011) and no
	// user token, because no user is involved. Putting it behind the bearer
	// middleware would reject every payment confirmation Cashfree ever sends.
	//
	// It is not therefore unauthenticated — it is authenticated by something
	// else, and it is the one route rate limited without a key for exactly that
	// reason.
	moneyhttp.NewWebhooks(payments, logger).Routes(authz.NewRegistrar(mux, guard))
	// The organisation a request acts for. Verification says who signed in;
	// this says whose rows they may touch, and it is a database lookup rather
	// than a token claim for the reason ADR-0027 §6 gives.
	if cfg.Identity.Enforce {
		resolver := identityservice.NewResolver(principals, logger)
		mux.Handle("/", auth.Middleware(verifier, resolver.Middleware(protected)))
		mux.Handle("/v1/resident/", auth.Middleware(verifier,
			auth.RequireSurface(auth.SurfaceLive, residents.Middleware(residentMux))))
	} else {
		// The renter every tenant-surface request acts as while authentication is
		// off. Unset is a supported state and answers 503 per request rather than
		// refusing to boot: the rest of the API is usable without it, and serving
		// an arbitrary renter would show one tenant's dues to whoever opened the
		// page.
		if cfg.Identity.ImpersonateResident == "" {
			logger.Warn("no renter to impersonate; the tenant surface will answer 503",
				"set", "DEV_IMPERSONATE_RESIDENT")
		}
		mux.Handle("/v1/resident/",
			residents.Impersonating(cfg.Identity.ImpersonateResident).Middleware(residentMux))

		// Dev only, and config.validate() refuses it anywhere else — an API that
		// forgot to authenticate does not fail, it works for everybody.
		//
		// Impersonation makes the endpoints usable before the GIP tenants exist
		// (issue #229). It needs an organisation named explicitly; without one the
		// process does not start, rather than quietly serving nobody.
		resolver, err := identityservice.NewImpersonatingResolver(identityservice.Impersonation{
			TenantID: tenancy.ID(cfg.Identity.ImpersonateOrg),
			PartyID:  "dev-impersonated",
		}, logger)
		if err != nil {
			return fmt.Errorf("authentication is off and %w — set DEV_IMPERSONATE_ORG", err)
		}
		logger.Warn("authentication is not enforced; every request impersonates one organisation",
			"env", cfg.Env, "organisation", cfg.Identity.ImpersonateOrg)
		mux.Handle("/", resolver.Middleware(protected))
	}

	tenantLimiter := httpx.NewLimiter(cfg.RateLimits.Tenant, nil)
	webhookLimiter := httpx.NewLimiter(cfg.RateLimits.Webhook, nil)
	// A third, because the tenant surface has nobody to be keyed by: a renter
	// sends no organisation header, so ByTenant returns "" for every one of
	// their requests and would limit none of them. Keyed per sign-in instead.
	residentLimiter := httpx.NewLimiter(cfg.RateLimits.Resident, nil)
	handler := httpx.Limited(webhookLimiter, webhookRoutes,
		httpx.Limited(residentLimiter, residentRoutes,
			httpx.Limited(tenantLimiter, httpx.ByTenant("X-Dwellm8-Org"), mux)))

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       90 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	health.Ready()
	logger.Info("ready", "addr", srv.Addr)

	// Shut down in the order a load balancer expects: stop being ready, let the
	// probe notice, then stop accepting. Draining first is what makes a rolling
	// deploy invisible to a tenant halfway through paying rent.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	select {
	case err := <-errc:
		return fmt.Errorf("listen: %w", err)
	case sig := <-stop:
		logger.Info("signal received, draining", "signal", sig.String())
	}

	health.Draining()
	time.Sleep(3 * time.Second) // one readiness probe interval

	ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownGrace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	logger.Info("stopped cleanly")
	return nil
}

func newLogger(cfg config.Config) *slog.Logger {
	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.IsProd() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// webhookRoutes keys the unauthenticated surface, and only that surface. A
// KeyFunc returning "" means "not limited by this one", so everything else falls
// through to the per-tenant limiter rather than being counted twice.
func webhookRoutes(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/v1/webhooks/") {
		return "webhooks"
	}
	return ""
}

// residentRoutes keys the tenant surface, per sign-in.
//
// The bucket is the renter's own, so one person refreshing their dues on a bad
// connection cannot shed another tenant's payment — which is the failure a
// single shared bucket over this prefix would produce, on the busiest day of the
// month for exactly the people trying to pay.
func residentRoutes(r *http.Request) string {
	if !strings.HasPrefix(r.URL.Path, "/v1/resident/") {
		return ""
	}
	return httpx.ByBearer("resident:anonymous")(r)
}
