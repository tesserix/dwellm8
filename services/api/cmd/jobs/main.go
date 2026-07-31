// Command jobs runs the periodic work: raising invoices, and asking providers
// what happened to payments nobody told us about.
//
// # Why a command rather than a workflow
//
// ADR-0015's durable-workflow standard is for operations with an **irreversible
// step** — a bank transfer that cannot be taken back, where the order of
// compensations is the whole problem. Neither job here has one. Billing writes a
// ledger entry keyed on (lease, period), and polling asks a provider a question;
// both are idempotent, both are safe to interrupt, and both are safe to run
// twice. Wrapping them in a saga would buy compensations for steps that need
// none, and pay for it with a workflow engine in the path of every rent cycle.
//
// So they are a scheduled command. The durability comes from the idempotency
// keys rather than from an engine: a pod killed half way through a billing run
// leaves the invoices it raised, and the next run raises the rest and nothing
// else.
//
// # Why not a goroutine inside the API
//
// The API runs two replicas. A ticker inside it fires twice per interval, and
// while both runs are idempotent, "safe" is not the same as "correct" — two
// replicas racing produce one invoice and a confusing pair of log lines that
// look like a duplicate every month. A CronJob is one runner, visible in
// `kubectl get jobs`, with a history somebody can read after an incident.
//
//	jobs bill   --through-days 5
//	jobs poll   --settle-within 10m
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/e2e"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/config"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("job failed", "error", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("no job named — try `bill` or `poll`")
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	slog.SetDefault(logger)

	// A job is not a server: it gets a deadline, because a run that hangs on a
	// provider holds a CronJob slot until the next one is skipped by
	// concurrencyPolicy, and then billing quietly stops.
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database pool: %w", err)
	}
	defer pool.Close()

	switch args[0] {
	case "bill":
		return bill(ctx, args[1:], cfg, pool, logger)
	case "poll":
		return poll(ctx, args[1:], cfg, pool, logger)
	default:
		return fmt.Errorf("%q is not a job — try `bill` or `poll`", args[0])
	}
}

// bill raises the invoices falling due inside the horizon.
func bill(ctx context.Context, args []string, cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) error {
	fs := flag.NewFlagSet("bill", flag.ContinueOnError)
	// Ahead of the due date, so the reminder ladder has something to point at
	// before the money is late rather than after.
	through := fs.Int("through-days", 5, "how many days ahead to raise invoices for")
	limit := fs.Int("limit", 500, "how many tenancies to consider in one run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The platform pool is used for exactly one thing: listing who to bill for.
	// Everything after that happens inside one organisation's session.
	platformPool, err := pgxpool.New(ctx, cfg.PlatformDatabaseURL)
	if err != nil {
		return fmt.Errorf("platform database pool: %w", err)
	}
	defer platformPool.Close()

	leases := leaseservice.NewLeases(leasestore.New(pool), log)
	run := e2e.NewBillingRun(
		identitystore.NewOrganisations(tenancy.NewPlatformPool(platformPool)),
		leasestore.New(pool),
		leases,
		moneyservice.NewBiller(moneystore.NewLedger(pool), log, nil),
		log,
	)

	horizon := effective.DateOf(time.Now(), time.UTC).AddDays(*through)
	out, err := run.Run(ctx, horizon, *limit)
	if err != nil {
		return err
	}
	log.Info("billing finished",
		"version", version, "tenancies", out.Tenancies, "raised", out.Raised,
		"already invoiced", out.Duplicate, "failed", out.Failed, "total", out.TotalMinor)

	// A run with failures exits non-zero so the CronJob is marked failed and
	// somebody sees it. The invoices it did raise stand — this is a report, not a
	// rollback.
	if out.Failed > 0 {
		return fmt.Errorf("%d tenancies could not be billed", out.Failed)
	}
	return nil
}

// poll asks the provider about payments nobody told us about.
func poll(ctx context.Context, args []string, cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) error {
	fs := flag.NewFlagSet("poll", flag.ContinueOnError)
	// Long enough that a payer has plausibly finished. Asking about a payment
	// created two seconds ago is a request per second for an answer that has not
	// changed.
	within := fs.Duration("settle-within", 10*time.Minute, "how long a payment waits before it is asked about")
	limit := fs.Int("limit", 200, "how many payments to ask about in one run")
	if err := fs.Parse(args); err != nil {
		return err
	}

	providers, err := paymentProviders(cfg)
	if err != nil {
		return fmt.Errorf("payment providers: %w", err)
	}

	// Same shape as billing, for the same reason: payments are tenant-scoped, so
	// a sweep that spans the platform runs once per organisation rather than once
	// as the role that is exempt from row-level security.
	platformPool, err := pgxpool.New(ctx, cfg.PlatformDatabaseURL)
	if err != nil {
		return fmt.Errorf("platform database pool: %w", err)
	}
	defer platformPool.Close()

	orgs, err := identitystore.NewOrganisations(tenancy.NewPlatformPool(platformPool)).Active(ctx)
	if err != nil {
		return err
	}

	payments := moneystore.NewPayments(pool)
	svc := moneyservice.NewPayments(payments, nil, providers, log)

	var total moneyservice.PollResult
	for _, org := range orgs {
		out, err := svc.Sweep(tenancy.With(ctx, org), payments, *within, *limit)
		if err != nil {
			// One organisation's failure is not the rest's. A single unreadable
			// row would otherwise leave every later organisation's payments
			// unasked, which is how one stuck collection becomes all of them.
			total.Failed++
			log.Error("sweeping an organisation", "organisation", org, "error", err)
			continue
		}
		total.Asked += out.Asked
		total.Moved += out.Moved
		total.Unchanged += out.Unchanged
		total.Failed += out.Failed
	}

	log.Info("polling finished",
		"version", version, "organisations", len(orgs), "asked", total.Asked,
		"moved", total.Moved, "unchanged", total.Unchanged, "failed", total.Failed)
	if total.Failed > 0 {
		return fmt.Errorf("%d payments could not be asked about", total.Failed)
	}
	return nil
}

// paymentProviders builds the chain, the same way the API does.
//
// Offline is already in a new registry and is not optional: a deployment with no
// way to record a cash payment cannot take rent when the aggregator is down.
func paymentProviders(cfg config.Config) (*provider.Registry, error) {
	r := provider.NewRegistry()
	if cfg.Cashfree.Configured() {
		cf, err := provider.NewCashfree(provider.CashfreeConfig{
			BaseURL:       cfg.Cashfree.BaseURL,
			ClientID:      cfg.Cashfree.ClientID,
			ClientSecret:  cfg.Cashfree.ClientSecret,
			APIVersion:    cfg.Cashfree.APIVersion,
			WebhookSecret: cfg.Cashfree.WebhookSecret,
		})
		if err != nil {
			return nil, err
		}
		r.Register(cf)
	}
	if err := r.SetChain(cfg.PaymentProviders...); err != nil {
		return nil, err
	}
	return r, nil
}
