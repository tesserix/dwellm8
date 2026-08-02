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
//	jobs bill        --through-days 5
//	jobs poll        --settle-within 10m
//	jobs automations
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
	discoverystore "github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/e2e"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	maintenanceservice "github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	maintenancestore "github.com/tesserix/dwellm8/services/api/internal/maintenance/store"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/config"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	tdsstore "github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/routine"
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
	case "authz":
		return authzJob(ctx, args[1:], cfg, pool, logger)
	case "review":
		return review(ctx, pool, logger)
	case "automations":
		return automations(ctx, args[1:], cfg, pool, logger)
	default:
		return fmt.Errorf("%q is not a job — try `bill`, `poll`, `authz`, `review` or `automations`", args[0])
	}
}

// review fails when any live statutory rule is past its review date — the
// registry's operational alert (#210, ADR-0023). The failed CronJob is the
// page, the same idiom as the authz reconcile: a rule nobody re-reads after a
// Budget is how a verified number quietly stops being true.
func review(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	rows, err := pool.Query(ctx, `
		SELECT rule_type, jurisdiction, rule_key, owner, review_due
		  FROM statutory_rules
		 WHERE retired_at IS NULL AND review_due < CURRENT_DATE
		 ORDER BY review_due, rule_type, jurisdiction`)
	if err != nil {
		return fmt.Errorf("reading the registry: %w", err)
	}
	defer rows.Close()

	overdue := 0
	for rows.Next() {
		var ruleType, jurisdiction, key, owner string
		var due time.Time
		if err := rows.Scan(&ruleType, &jurisdiction, &key, &owner, &due); err != nil {
			return err
		}
		overdue++
		log.Warn("statutory rule overdue for review",
			"rule", ruleType, "jurisdiction", jurisdiction, "key", key,
			"owner", owner, "review_due", due.Format("2006-01-02"))
	}
	if err := rows.Err(); err != nil {
		return err
	}
	log.Info("registry review finished", "version", version, "overdue", overdue)
	if overdue > 0 {
		return fmt.Errorf("%d statutory rule(s) are past review_due — each is named above with its owner", overdue)
	}
	return nil
}

// authzJob compares the graph with the domain database. dwellm8#151.
//
// reconcile reports divergence and exits non-zero, so the CronJob's failure is
// the alert — it never repairs, because a silent repair is a bug that stops
// being investigated. rebuild is the repair, and the replay path: it writes
// what is missing and removes what should not be there, through the same
// AgreementEdges mapping the event projector uses, so a rebuilt store is the
// projected store by construction.
func authzJob(ctx context.Context, args []string, cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) error {
	fs := flag.NewFlagSet("authz", flag.ContinueOnError)
	mode := fs.String("mode", "reconcile", "reconcile reports divergence; rebuild repairs it")
	limit := fs.Int("limit", 5000, "how many tenancies to compare per organisation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if cfg.Authz.URL == "" {
		return errors.New("AUTHZ_URL is not set — there is no graph to compare")
	}

	fga := authz.NewClient(cfg.Authz.URL)
	if err := fga.Bootstrap(ctx, cfg.Authz.Store); err != nil {
		return fmt.Errorf("authz bootstrap: %w", err)
	}

	platformPool, err := pgxpool.New(ctx, cfg.PlatformDatabaseURL)
	if err != nil {
		return fmt.Errorf("platform database pool: %w", err)
	}
	defer platformPool.Close()

	orgs, err := identitystore.NewOrganisations(tenancy.NewPlatformPool(platformPool)).Active(ctx)
	if err != nil {
		return fmt.Errorf("listing organisations: %w", err)
	}

	leases := leasestore.New(pool)
	var tenancies, diverged, repaired int
	for _, org := range orgs {
		octx := tenancy.With(ctx, org)
		active, err := leases.ActiveTenancies(octx, *limit)
		if err != nil {
			return fmt.Errorf("organisation %s: %w", org, err)
		}
		for _, t := range active {
			tenancies++
			parties := make([]authz.Party, len(t.Parties))
			for i, p := range t.Parties {
				parties[i] = authz.Party{PartyID: p.PartyID, Role: p.Role}
			}
			expected := authz.AgreementEdges(t.ID, t.UnitID, parties)
			actual, err := fga.ReadTuples(ctx, "agreement:"+t.ID)
			if err != nil {
				return err
			}
			missing, extra := diffTuples(expected, actual)
			if len(missing) == 0 && len(extra) == 0 {
				continue
			}
			diverged++
			log.Warn("authz divergence",
				"organisation", org, "lease", t.ID, "missing", missing, "extra", extra)
			if *mode == "rebuild" {
				// Extra edges first: access more permissive than the domain is
				// the state that must not outlive this run.
				if err := fga.WriteTuples(ctx, missing, extra); err != nil {
					return fmt.Errorf("repairing lease %s: %w", t.ID, err)
				}
				repaired++
			}
		}
	}

	log.Info("authz "+*mode+" finished",
		"version", version, "organisations", len(orgs), "tenancies", tenancies,
		"diverged", diverged, "repaired", repaired)
	if *mode == "reconcile" && diverged > 0 {
		return fmt.Errorf("%d tenancies diverge from the graph — run `jobs authz --mode rebuild` after understanding why", diverged)
	}
	return nil
}

// diffTuples is set difference in both directions.
func diffTuples(expected, actual []authz.Tuple) (missing, extra []authz.Tuple) {
	want := map[authz.Tuple]bool{}
	for _, t := range expected {
		want[t] = true
	}
	have := map[authz.Tuple]bool{}
	for _, t := range actual {
		have[t] = true
		if !want[t] {
			extra = append(extra, t)
		}
	}
	for _, t := range expected {
		if !have[t] {
			missing = append(missing, t)
		}
	}
	return missing, extra
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

	// The stay hold sweep rides the same ten-minute heartbeat (#233): a hold
	// whose clock lapsed with no payment attached releases its nights. Lazy
	// expiry already refuses confirming one; this is what stops the calendar
	// showing nights nobody is going to take.
	released, err := discoverystore.NewStay(pool, tenancy.NewPlatformPool(platformPool)).
		SweepExpiredHolds(ctx)
	if err != nil {
		log.Error("sweeping expired stay holds", "error", err)
	} else if released > 0 {
		log.Info("expired stay holds released", "released", released)
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

// automations runs the scheduled prebuilt automations, once per organisation.
// ADR-0033 §6, which is ADR-0028's argument applied to a job that reads every
// tenancy and every balance: as the platform role it would put the whole
// platform's arrears in one result set, where a single wrong join is a reminder
// sent to somebody else's tenant.
//
// It exits zero when automations failed for some organisations and non-zero only
// when the run itself could not proceed. A failing automation is an alert on the
// log line that names it; failing the CronJob would stop every other
// organisation's automations for the sake of one landlord's data.
func automations(ctx context.Context, args []string, cfg config.Config, pool *pgxpool.Pool, log *slog.Logger) error {
	fs := flag.NewFlagSet("automations", flag.ContinueOnError)
	only := fs.String("organisation", "", "run for one organisation rather than all of them")
	if err := fs.Parse(args); err != nil {
		return err
	}

	platformPool, err := pgxpool.New(ctx, cfg.PlatformDatabaseURL)
	if err != nil {
		return fmt.Errorf("platform database pool: %w", err)
	}
	defer platformPool.Close()

	runner, err := automationRunner(pool, platformPool, log)
	if err != nil {
		return err
	}

	var res automation.Result
	if *only != "" {
		res, err = runner.RunFor(tenancy.With(ctx, tenancy.ID(*only)))
	} else {
		res, err = runner.RunAll(ctx)
	}
	if err != nil {
		return err
	}
	log.Info("automations finished", "version", version,
		"organisations", res.Organisations, "automations", res.Automations,
		"acted", res.Acted, "skipped", res.Skipped,
		"awaiting_approval", res.Awaiting, "failed", res.Failed)
	return nil
}

// automationRunner wires the catalogue to the modules. The one place the
// dependency direction of ADR-0033 §2 is visible: the engine below, the modules
// beside it, and the catalogue above both.
func automationRunner(pool, platformPool *pgxpool.Pool, log *slog.Logger) (*automation.Runner, error) {
	leases := leaseservice.NewLeases(leasestore.New(pool), log)
	statements := moneyservice.NewStatements(moneystore.NewLedger(pool), moneystore.NewPayments(pool), nil)
	checklists := maintenanceservice.NewChecklists(maintenancestore.New(pool), log)

	catalogue := routine.Catalogue(routine.Deps{
		Now:        routine.Today,
		Tenancies:  routine.FromLeases{Leases: leases},
		Money:      routine.FromMoney{Statements: statements},
		Checklists: routine.FromChecklists{Checklists: checklists},
		Compliance: routine.FromCertificates{Certificates: tdsstore.New(pool)},
		Events:     routine.Outbox{Pool: pool},
	})
	return automation.NewRunner(catalogue,
		identitystore.NewOrganisations(tenancy.NewPlatformPool(platformPool)),
		automation.NewStore(pool), log)
}
