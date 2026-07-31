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
	leasehttp "github.com/tesserix/dwellm8/services/api/internal/lease/http"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	moneyhttp "github.com/tesserix/dwellm8/services/api/internal/money/http"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/config"
	"github.com/tesserix/dwellm8/services/api/internal/platform/httpx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
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

	payments := moneyservice.NewPayments(
		moneystore.NewPayments(pool),
		moneystore.NewInbox(tenancy.NewPlatformPool(platformPool)),
		providers, logger)

	leases := leaseservice.NewLeases(leasestore.New(pool), logger)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Readyz)
	// Module routes mount here as each module lands, one line per module.
	moneyhttp.NewWebhooks(payments, logger).Routes(mux)
	leasehttp.NewLeases(leases, logger).Routes(mux)

	// Rate limiting, issue #228. Two limiters because they fail differently: the
	// per-tenant one stops one organisation taking the service down for the rest,
	// and the webhook route is limited unkeyed because its caller is
	// unauthenticated by definition and has no identity to be limited by.
	//
	// Health is outside both. A readiness probe shed as "too many requests" takes
	// the pod out of rotation at exactly the moment it is busiest, which turns a
	// load spike into an outage.
	tenantLimiter := httpx.NewLimiter(cfg.RateLimits.Tenant, nil)
	webhookLimiter := httpx.NewLimiter(cfg.RateLimits.Webhook, nil)
	handler := httpx.Limited(webhookLimiter, webhookRoutes,
		httpx.Limited(tenantLimiter, httpx.ByTenant("X-Dwellm8-Org"), mux))

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
