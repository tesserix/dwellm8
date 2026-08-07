// Command api is the one Dwellm8 API.
//
// Per ADR-0001 this is a modular monolith: eight modules — identity, property,
// lease, money, maintenance, community, discovery and notify — in one process,
// one database and one transaction boundary. Modules are wired here and
// nowhere else, so the dependency direction is visible in a single file.
package main

import (
	"context"
	"encoding/json"
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
	communityservice "github.com/tesserix/dwellm8/services/api/internal/community/service"
	communitystore "github.com/tesserix/dwellm8/services/api/internal/community/store"
	discoveryhttp "github.com/tesserix/dwellm8/services/api/internal/discovery/http"
	twilioverify "github.com/tesserix/dwellm8/services/api/internal/discovery/provider/twilio"
	discoveryservice "github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	discoverystore "github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/identity/docscan"
	identityhttp "github.com/tesserix/dwellm8/services/api/internal/identity/http"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leasehttp "github.com/tesserix/dwellm8/services/api/internal/lease/http"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	maintenancehttp "github.com/tesserix/dwellm8/services/api/internal/maintenance/http"
	maintenanceservice "github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	maintenancestore "github.com/tesserix/dwellm8/services/api/internal/maintenance/store"
	moneyhttp "github.com/tesserix/dwellm8/services/api/internal/money/http"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	propertyhttp "github.com/tesserix/dwellm8/services/api/internal/property/http"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
	propertystore "github.com/tesserix/dwellm8/services/api/internal/property/store"
	"golang.org/x/oauth2/google"

	"github.com/tesserix/dwellm8/services/api/internal/platform/activity"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/blob"
	"github.com/tesserix/dwellm8/services/api/internal/platform/config"
	"github.com/tesserix/dwellm8/services/api/internal/platform/docurl"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events/natsx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/httpx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/mail"
	"github.com/tesserix/dwellm8/services/api/internal/platform/places"
	"github.com/tesserix/dwellm8/services/api/internal/platform/push"
	statutorystore "github.com/tesserix/dwellm8/services/api/internal/platform/statutory/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	tdsstore "github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/routine"
	automationsurface "github.com/tesserix/dwellm8/services/api/internal/surface/automations"
	opssurface "github.com/tesserix/dwellm8/services/api/internal/surface/ops"
	ownersurface "github.com/tesserix/dwellm8/services/api/internal/surface/owner"
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

	// The automation engine and its catalogue, ADR-0033. Built here because the
	// catalogue is the one thing above every module: the engine below it owns the
	// settings and the run log, the modules beside it do the work, and this is the
	// only file that sees all three.
	//
	// A run that is scheduled happens in `jobs automations` (ADR-0028); what the
	// API holds is the settings surface and the consumer for the event-triggered
	// ones, so a tenancy going live starts its move-in without waiting for a
	// CronJob.
	automationStore := automation.NewStore(pool)

	// The checklist module, ADR-0032. Built before the lease service because the
	// lease service takes it: a tenancy does not close over an unfinished move-out,
	// and the gate has to exist before the thing it gates.
	checklists := maintenanceservice.NewChecklists(maintenancestore.New(pool), logger)

	// The lease module learns to identify a party by mobile number, which is how
	// a renter acquires an identity: their landlord types the number weeks
	// before they ever open the app. ADR-0029 §2.
	//
	// It also learns two things it must not read for itself: what blocking work
	// stands in front of closing a tenancy (maintenance) and how far that tenancy
	// has been invoiced (money). Both are service interfaces the lease module
	// declares and the other modules satisfy — ADR-0001 §3, and the whole of the
	// coupling is visible on this line.
	leases := leaseservice.NewLeases(leasestore.New(pool), logger).
		WithResidents(residents).
		WithChecklists(maintenanceservice.LeaseGate{Checklists: checklists}).
		WithBilling(statements)

	automations, err := automation.NewRunner(
		routine.Catalogue(routine.Deps{
			Now:        routine.Today,
			Tenancies:  routine.FromLeases{Leases: leases},
			Money:      routine.FromMoney{Statements: statements},
			Checklists: routine.FromChecklists{Checklists: checklists},
			Compliance: routine.FromCertificates{Certificates: tdsstore.New(pool)},
			Events:     routine.Outbox{Pool: pool},
		}),
		identitystore.NewOrganisations(tenancy.NewPlatformPool(platformPool)),
		automationStore, logger)
	if err != nil {
		// A catalogue that does not validate is a programming error, and the
		// process refusing to start is the only way it is ever noticed: the
		// alternative is an automation that silently never fires.
		return fmt.Errorf("automation catalogue: %w", err)
	}

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
			if err != nil {
				cancel()
				return fmt.Errorf("authz projector consumer: %w", err)
			}
			// A second durable on the identity stream: organisation birth is
			// the graph's first edge. Two consumers because streams are per
			// module (ADR-0002 §2) and a consumer belongs to one stream.
			idCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
				Name:     "authz-tupler-identity",
				Stream:   "DWELLM8_IDENTITY",
				Subjects: []string{"dwellm8.identity.organisation.>"},
			})
			cancel()
			if err != nil {
				return fmt.Errorf("authz projector consumer: %w", err)
			}
			// A third durable on the discovery stream: a published listing and a
			// received enquiry are relationship-bearing facts too (ADR-0019).
			ensureCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
			discCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
				Name:     "authz-tupler-discovery",
				Stream:   "DWELLM8_DISCOVERY",
				Subjects: []string{"dwellm8.discovery.>"},
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
			go func() {
				if err := natsx.Consume(relayCtx, idCons, proj.Handle, logger); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("authz identity projector stopped", "error", err)
				}
			}()
			go func() {
				if err := natsx.Consume(relayCtx, discCons, proj.Handle, logger); err != nil && !errors.Is(err, context.Canceled) {
					logger.Error("authz discovery projector stopped", "error", err)
				}
			}()
			logger.Info("authz projector running",
				"consumers", "authz-tupler, authz-tupler-identity, authz-tupler-discovery")
		}
	} else {
		logger.Warn("authz is not configured; route checks are declared but dark",
			"set", "AUTHZ_URL")
	}

	// The event-triggered automations, ADR-0033 §5. Two durable consumers on the
	// two streams that carry the facts they react to — a tenancy going live, a
	// notice being served, an organisation being created.
	//
	// Independent of the authz projector on purpose, and on their own consumer
	// names: a checklist that fails to start must not stop an access edge being
	// written, and the two have nothing to say to each other.
	//
	// The saved-search stores serve both the alert consumer below and the
	// public routes further down, so they are built once, here.
	searchesStore := discoverystore.NewSearches(tenancy.NewPlatformPool(platformPool))
	pushTokens := discoverystore.NewPushTokens(tenancy.NewPlatformPool(platformPool))
	if eventsConn != nil {
		ensureCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		leaseCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
			Name:     "automations-lease",
			Stream:   "DWELLM8_LEASE",
			Subjects: []string{"dwellm8.lease.>"},
		})
		if err != nil {
			cancel()
			return fmt.Errorf("automation consumer: %w", err)
		}
		idCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
			Name:     "automations-identity",
			Stream:   "DWELLM8_IDENTITY",
			Subjects: []string{"dwellm8.identity.organisation.>"},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("automation consumer: %w", err)
		}
		// Two goroutines rather than a loop over the pair: the consumer type comes
		// from the JetStream SDK, and naming it here would put that import in cmd/
		// — which the arch guard forbids and ADR-0002 §4 explains.
		consumer := routine.Consumer{Runner: automations, Log: logger}
		go func() {
			if err := natsx.Consume(relayCtx, leaseCons, consumer.Handle, logger); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Error("automation consumer stopped", "consumer", "automations-lease", "error", err)
			}
		}()
		go func() {
			if err := natsx.Consume(relayCtx, idCons, consumer.Handle, logger); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Error("automation consumer stopped", "consumer", "automations-identity", "error", err)
			}
		}()
		logger.Info("automations running", "consumers", "automations-lease, automations-identity")

		// The listing closer, ADR-0019 #135: a tenancy starting kills every
		// advert for its unit, with no manual step. Its own durable name, for
		// the automations' reason — a listing that fails to close must not
		// stop a move-in checklist starting, and vice versa.
		ensureCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		letCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
			Name:     "discovery-listings",
			Stream:   "DWELLM8_LEASE",
			Subjects: []string{"dwellm8.lease.tenancy.>"},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("discovery consumer: %w", err)
		}
		// One handler, two streams: lease facts close adverts, money facts
		// confirm stays (#233 — a webhook alone never moves a booking; the
		// received fact, already in the ledger's transaction, does).
		ensureCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		payCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
			Name:     "discovery-stay-payments",
			Stream:   "DWELLM8_MONEY",
			Subjects: []string{"dwellm8.money.payment.>"},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("discovery payments consumer: %w", err)
		}
		// Saved-search alerts (#144/#126): the published fact fans out to
		// every opted-in search it satisfies, over Expo push.
		ensureCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		alertCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
			Name:     "discovery-alerts",
			Stream:   "DWELLM8_DISCOVERY",
			Subjects: []string{"dwellm8.discovery.listing.>"},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("discovery alerts consumer: %w", err)
		}
		alerts := discoveryservice.Alerts{
			Searches: searchesStore,
			Tokens:   pushTokens,
			Sender:   push.New(""),
			Log:      logger,
		}
		go func() {
			if err := natsx.Consume(relayCtx, alertCons, alerts.Handle, logger); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Error("discovery consumer stopped", "consumer", "discovery-alerts", "error", err)
			}
		}()

		// A viewing called off reaching the person holding it (#332): the same
		// push path as the alerts, addressed to a prospect rather than to a
		// saved search.
		ensureCtx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		noticeCons, err := eventsConn.EnsureConsumer(ensureCtx, natsx.ConsumerSpec{
			Name:     "discovery-viewing-notices",
			Stream:   "DWELLM8_DISCOVERY",
			Subjects: []string{"dwellm8.discovery.inspection.>"},
		})
		cancel()
		if err != nil {
			return fmt.Errorf("discovery viewing notices consumer: %w", err)
		}
		notices := discoveryservice.Notices{
			Tokens: pushTokens,
			Sender: push.New(""),
			Log:    logger,
		}
		go func() {
			if err := natsx.Consume(relayCtx, noticeCons, notices.Handle, logger); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Error("discovery consumer stopped", "consumer", "discovery-viewing-notices", "error", err)
			}
		}()

		letMarker := discoveryservice.Consumer{
			Marker: discoverystore.NewLetMarker(tenancy.NewPlatformPool(platformPool)),
			Stay:   discoverystore.NewStay(pool, tenancy.NewPlatformPool(platformPool)),
			Log:    logger,
		}
		go func() {
			if err := natsx.Consume(relayCtx, letCons, letMarker.Handle, logger); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Error("discovery consumer stopped", "consumer", "discovery-listings", "error", err)
			}
		}()
		go func() {
			if err := natsx.Consume(relayCtx, payCons, letMarker.Handle, logger); err != nil &&
				!errors.Is(err, context.Canceled) {
				logger.Error("discovery consumer stopped", "consumer", "discovery-stay-payments", "error", err)
			}
		}()
		logger.Info("discovery consumers running",
			"consumers", "discovery-listings, discovery-stay-payments, discovery-viewing-notices")
	} else {
		logger.Warn("the event-triggered automations are off; a tenancy going live will not " +
			"start its move-in until somebody does. The scheduled ones are unaffected.")
	}

	// GCS-backed media and documents (platform/blob), used by discovery's
	// listing photos and the owner surface alike. Optional the way DocURLKey
	// is: unset serves everything except uploads and photo delivery.
	var blobStore *blob.Store
	if cfg.Blob.Configured() {
		blobStore, err = blob.New(context.Background(), cfg.Blob.BucketPrefix, cfg.Blob.SignerServiceAccount)
		if err != nil {
			return fmt.Errorf("blob store: %w", err)
		}
		defer blobStore.Close()
		logger.Info("blob store ready", "buckets", cfg.Blob.BucketPrefix+"-<surface>-assets",
			"signer", cfg.Blob.SignerServiceAccount)
	} else {
		logger.Warn("no GCS signer; uploads and photo delivery are off",
			"set", "GCS_SIGNER_SA_EMAIL")
	}

	// The discovery funnel, ADR-0019: listings from live inventory, the public
	// site, prospects and their enquiries, and the conversion back into a
	// lease. The occupancy guard and the conversion drafter are both the lease
	// module — the funnel starts and ends at the tenancy, and both couplings
	// are visible on these lines.
	listingsStore := discoverystore.NewListings(pool)
	prospectsStore := discoverystore.NewProspects(tenancy.NewPlatformPool(platformPool))
	enquiriesStore := discoverystore.NewEnquiries(pool, tenancy.NewPlatformPool(platformPool))
	listings := discoveryservice.NewListings(listingsStore, discoverystore.NewPublic(pool),
		routine.Today, logger).WithOccupancy(leases)
	enquiries := discoveryservice.NewEnquiries(enquiriesStore, listingsStore, prospectsStore, logger).
		WithDrafter(discoveryservice.FromLeases{Leases: leases})

	// The OTP verifier: Twilio Verify when configured, the dev fake outside
	// production, absent otherwise — verification answers 503, search and
	// shortlisting still work. A fake that verified nobody must not run where
	// a real prospect can reach it.
	var phoneVerifier discoveryservice.PhoneVerifier
	switch {
	case cfg.Twilio.Configured():
		phoneVerifier = twilioverify.New(cfg.Twilio.AccountSID, cfg.Twilio.AuthToken,
			cfg.Twilio.VerifySID, "")
		logger.Info("prospect verification via Twilio Verify")
	case !cfg.IsProd():
		phoneVerifier = discoveryservice.DevVerifier{Log: logger}
	default:
		logger.Warn("no OTP provider; prospect verification answers 503 until one is wired")
	}
	if !cfg.IsProd() {
		enquiries = enquiries.WithBridges(discoveryservice.DevBridge{Log: logger})
	}
	prospects := discoveryservice.NewProspects(prospectsStore, phoneVerifier, logger)

	// The viewing loop, #139–#141: slots, bookings, changes and outcomes.
	inspections := discoveryservice.NewInspections(
		discoverystore.NewInspections(pool, tenancy.NewPlatformPool(platformPool)),
		prospectsStore, logger)

	// Routes that a person authenticates for.
	protected := http.NewServeMux()
	leasehttp.NewLeases(leases, logger).Routes(authz.NewRegistrar(protected, guard))
	// The Gin layer (platform/ginx): new surfaces standardise on Gin behind
	// the same guard, mounted per-pattern so the two routers never fight over
	// a subtree. Rent revisions, #36, is its first tenant.
	ginEngine := ginx.Engine()
	leasehttp.NewRevisions(leases, logger).Routes(ginx.New(ginEngine, guard))
	protected.Handle("POST /v1/leases/{id}/rent-revisions", ginEngine)

	// HomeStay, #233 — Gin-native on both sides. The host tree mounts under
	// authentication; the guest tree joins the public funnel and its rate
	// bucket, riding the same prospect token and verification gate.
	stayStore := discoverystore.NewStay(pool, tenancy.NewPlatformPool(platformPool))
	stayHost := ginx.Engine()
	discoveryhttp.NewStay(stayStore, prospects, logger).HostRoutes(ginx.New(stayHost, guard))
	protected.Handle("/v1/stay/", stayHost)
	stayPublic := ginx.Engine()
	discoveryhttp.NewStay(stayStore, prospects, logger).WithPayments(payments).
		PublicRoutes(ginx.New(stayPublic, guard))

	// Listing media, #136: uploads on signed PUTs into the find bucket,
	// ordering and moderation here, delivery on the public detail page.
	mediaStore := discoverystore.NewMedia(pool, tenancy.NewPlatformPool(platformPool))
	mediaOwner := ginx.Engine()
	discoveryhttp.NewMedia(mediaStore, blobStore, logger).OwnerRoutes(ginx.New(mediaOwner, guard))
	for _, p := range []string{
		"POST /v1/listings/{id}/media/upload-url",
		"POST /v1/listings/{id}/media",
		"PUT /v1/listings/{id}/media/order",
		"GET /v1/listings/{id}/media",
		"POST /v1/listings/{id}/media/{mid}/takedown",
		"GET /v1/moderation/media",
		"POST /v1/listings/{id}/media/{mid}/clear",
	} {
		protected.Handle(p, mediaOwner)
	}
	mediaPublic := ginx.Engine()
	discoveryhttp.NewMedia(mediaStore, blobStore, logger).PublicRoutes(ginx.New(mediaPublic, guard))

	// Listing moderation, #143: the public reports, the admin judges, the
	// outbox remembers. Publication guardrails fire in the domain on publish.
	moderationStore := discoverystore.NewModeration(pool, tenancy.NewPlatformPool(platformPool))
	moderationOwner := ginx.Engine()
	discoveryhttp.NewModeration(moderationStore, logger).OwnerRoutes(ginx.New(moderationOwner, guard))
	for _, p := range []string{
		"GET /v1/moderation/listings",
		"POST /v1/listings/{id}/suspend",
		"POST /v1/listings/{id}/warn",
		"POST /v1/listings/{id}/reinstate",
		"POST /v1/listings/{id}/reports/dismiss",
	} {
		protected.Handle(p, moderationOwner)
	}
	moderationPublic := ginx.Engine()
	discoveryhttp.NewModeration(moderationStore, logger).PublicRoutes(ginx.New(moderationPublic, guard))

	// Saved searches, #144: the prospect's own criteria, shortlist-style.
	searchesPublic := ginx.Engine()
	discoveryhttp.NewSearches(searchesStore, prospects, logger).
		PublicRoutes(ginx.New(searchesPublic, guard))

	// Push tokens, #126: the prospect's reachability, same session key.
	pushPublic := ginx.Engine()
	discoveryhttp.NewPush(pushTokens, prospects, logger).PublicRoutes(ginx.New(pushPublic, guard))

	// Applications, #142: the formal step between enquiry and lease.
	appsStore := discoverystore.NewApplications(pool, tenancy.NewPlatformPool(platformPool))
	appsOwner := ginx.Engine()
	discoveryhttp.NewApplications(appsStore, listingsStore, prospects,
		discoveryservice.FromLeases{Leases: leases}, logger).
		OwnerRoutes(ginx.New(appsOwner, guard))
	for _, p := range []string{
		"GET /v1/applications",
		"POST /v1/applications/{id}/review",
		"POST /v1/applications/{id}/accept",
		"POST /v1/applications/{id}/decline",
	} {
		protected.Handle(p, appsOwner)
	}
	appsPublic := ginx.Engine()
	discoveryhttp.NewApplications(appsStore, listingsStore, prospects,
		discoveryservice.FromLeases{Leases: leases}, logger).
		PublicRoutes(ginx.New(appsPublic, guard))
	// Registering inventory, #32 — the rows everything else points back to.
	propertyhttp.New(propertyservice.New(propertystore.New(pool)), logger).
		Routes(authz.NewRegistrar(protected, guard))
	discoveryhttp.NewListings(listings, enquiries, logger).Routes(authz.NewRegistrar(protected, guard))
	discoveryInspections := discoveryhttp.NewInspections(inspections, logger)
	discoveryInspections.OwnerRoutes(authz.NewRegistrar(protected, guard))
	discoveryInspections.ScheduleRoutes(authz.NewRegistrar(protected, guard))
	maintenancehttp.NewChecklists(checklists, logger).Routes(authz.NewRegistrar(protected, guard))
	automationsurface.New(automations, automationStore, logger).Routes(authz.NewRegistrar(protected, guard))

	// The impersonated-owner control, #227. Changes fail closed without the
	// fingerprint key; the payout run (#80) is the reader.
	payoutAccounts := moneyservice.NewPayoutAccounts(
		moneystore.NewPayoutAccounts(pool), cfg.PayoutFingerprintKey, logger)
	moneyhttp.NewPayoutAccounts(payoutAccounts, logger).Routes(authz.NewRegistrar(protected, guard))

	// The activity feed, #196: the outbox read back per record, plus notes.
	// One store serves both sides — the org routes here, and the renter's cut
	// on the resident surface below, where the audience narrows what it sees.
	feed := activity.NewStore(pool)
	activity.NewHandler(feed, logger).Routes(authz.NewRegistrar(protected, guard))

	// The period close, #190. Enforcement is the database trigger; these routes
	// are the checklist, the history, and the audit pack.
	moneyhttp.NewPeriods(moneystore.NewPeriods(pool), logger).Routes(authz.NewRegistrar(protected, guard))

	// Address lookup, behind every address field on every surface (#282).
	places.NewHandler(places.New(cfg.Places, logger), logger).
		Routes(authz.NewRegistrar(protected, guard))

	// The owner's view — the Own app's surface (#55). Composition only: the
	// property module's read, the lease's active tenancy, money's owner
	// statement, and the GCS-backed documents.
	ownerMux := http.NewServeMux()
	ownersurface.New(
		propertyservice.New(propertystore.New(pool)),
		leases, statements,
		identityservice.NewOrganisations(identitystore.NewOrganisations(tenancy.NewPlatformPool(platformPool))),
		feed, blobStore, logger, nil).
		Routes(authz.NewRegistrar(ownerMux, guard))

	// The management firm's view — the Ops app's surface. Portfolio, the
	// live roster's arrears, and the org feed: the same composition seam as
	// the owner surface above, reused rather than duplicated (both take the
	// same property.Service and leases/statements/feed instances).
	opsMux := http.NewServeMux()
	tickets := maintenanceservice.NewTickets(maintenancestore.NewTickets(pool), logger)
	community := communityservice.New(communitystore.New(pool), logger)
	owners := identityservice.NewOwners(principals, logger)
	opsHandler := opssurface.New(
		propertyservice.New(propertystore.New(pool)),
		leases, statements, residents, feed, logger, nil).
		WithTickets(tickets).WithCommunity(community).WithOwners(owners).
		WithMerchants(moneyservice.NewMerchants(moneystore.NewMerchants(pool), providers)).
		WithSettlements(moneyservice.NewSettlements(
			moneystore.NewSettlements(pool), moneystore.NewMerchants(pool), providers)).
		WithRegistrations(identityservice.NewRegistrations(principals)).
		WithPayments(payments).WithBlob(blobStore).WithListings(listings).
		WithTemplates(propertyservice.NewTemplates(propertystore.NewTemplates(pool)))
	if scanner, err := documentScanner(cfg.DocScanEngine, logger); err != nil {
		return fmt.Errorf("document scanner: %w", err)
	} else if scanner != nil {
		opsHandler.WithScanner(scanner)
		opsHandler.ScanRoutes(authz.NewRegistrar(opsMux, guard))
	}
	// The decision matrix, over the registry read once at boot (ADR-0023: it is
	// reference data, and a lookup per invoice would put a round trip in a loop).
	// A deployment whose registry chapter has not been applied yet keeps serving
	// everything else; the deduction route is simply absent rather than guessing.
	if table, err := statutorystore.New(pool).Table(context.Background()); err != nil {
		logger.Error("loading the statutory registry", "error", err)
	} else if matrix, err := tds.New(table); err != nil {
		logger.Error("building the TDS matrix", "error", err)
	} else {
		opsHandler.WithTDS(matrix, tdsstore.New(pool))
		// What a payment on this tenancy is short by, and why, #318.
		opsHandler.DeductionRoutes(authz.NewRegistrar(opsMux, guard))
	}
	opsHandler.Routes(authz.NewRegistrar(opsMux, guard))
	opsHandler.WorklistRoutes(authz.NewRegistrar(opsMux, guard))
	opsHandler.OnboardingRoutes(authz.NewRegistrar(opsMux, guard))
	opsHandler.PortfolioRoutes(authz.NewRegistrar(opsMux, guard))
	opsHandler.OwnershipRoutes(authz.NewRegistrar(opsMux, guard))
	// What the landlord has furnished, which decides the rate, #318.
	opsHandler.TaxProfileRoutes(authz.NewRegistrar(opsMux, guard))
	// The copies behind it — a passport, a TRC, an owner abroad's
	// self-attestation, #318.
	opsHandler.PartyDocumentRoutes(authz.NewRegistrar(opsMux, guard))
	// The agreements the firm issues, and its own revisions of them, #341.
	opsHandler.TemplateRoutes(authz.NewRegistrar(opsMux, guard))
	// Where the manager's own rent settles, #269. Provider-agnostic: the
	// registry decides whether that is Cashfree or anyone else.
	opsHandler.MerchantRoutes(authz.NewRegistrar(opsMux, guard))
	// How one collection is divided, and the owner's leg released, #270.
	opsHandler.SettlementRoutes(authz.NewRegistrar(opsMux, guard))
	// What the firm must hold to manage somebody else's property at all — RERA
	// s.9, the constitution's PAN, and the documents that evidence both, #282.
	opsHandler.RegistrationRoutes(authz.NewRegistrar(opsMux, guard))
	// Rent taken in cash, by cheque or by a transfer the manager saw, #297.
	opsHandler.CollectionRoutes(authz.NewRegistrar(opsMux, guard))
	// The applicant pack, #258. Gin-native like the applications it hangs off,
	// mounted here rather than on the owner tree because the firm reaches it
	// under a mandate, and only this mux carries one.
	packs := ginx.Engine()
	discoveryhttp.NewApplicants(
		discoverystore.NewApplicants(pool, tenancy.NewPlatformPool(platformPool)), logger).
		OwnerRoutes(ginx.New(packs, guard))
	for _, p := range []string{
		"GET /v1/ops/applications/{id}/profile",
		"PUT /v1/ops/applications/{id}/profile",
		"POST /v1/ops/applications/{id}/profile/submit",
		"PUT /v1/ops/applications/{id}/profile/people",
		"GET /v1/ops/applications/{id}/addresses",
		"PUT /v1/ops/applications/{id}/addresses",
	} {
		opsMux.Handle(p, packs)
	}

	// The same review queue under a mandate: the firm managing the property is
	// the one working the applications on it.
	appsOps := ginx.Engine()
	discoveryhttp.NewApplications(appsStore, listingsStore, prospects,
		discoveryservice.FromLeases{Leases: leases}, logger).
		OpsRoutes(ginx.New(appsOps, guard))
	for _, p := range []string{
		"GET /v1/ops/applications",
		"POST /v1/ops/applications/{id}/review",
		"POST /v1/ops/applications/{id}/accept",
		"POST /v1/ops/applications/{id}/decline",
	} {
		opsMux.Handle(p, appsOps)
	}

	// Every ops request may declare the mandate it acts under (ADR-0005);
	// the wrapped mux is what both mounts below serve.
	opsSurface := opssurface.ActingUnderGrant(opsMux)

	// The tenant view, on its own tree. Issue #51, ADR-0029.
	//
	// Separate because it resolves a sign-in differently: the ordinary resolver
	// answers 409 for a person in two organisations, and a renter with two
	// landlords is exactly that — the ordinary case for them, not an error. It
	// also carries its own surface check, so a genuine Ops token presented here
	// is refused before any query runs.
	residentMux := http.NewServeMux()
	resident.New(leases, statements, payments, logger, nil).
		WithActivity(feed).WithTickets(tickets).WithCommunity(community).
		WithIdentity(residents).
		Routes(authz.NewRegistrar(residentMux, guard))

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

	// The public listing site, outside authentication for the stranger's
	// reason: a prospect has no account by definition. Reads traverse the
	// schema's one public branch; writes require a browsing token and, past
	// the verification point, a verified phone. ADR-0019.
	discoveryhttp.NewPublic(listings, prospects, enquiries, logger).
		WithMedia(mediaStore, blobStore).
		Routes(authz.NewRegistrar(mux, guard))
	mux.Handle("POST /v1/public/listings/{id}/media/{mid}/report", mediaPublic)
	mux.Handle("POST /v1/public/listings/{id}/report", moderationPublic)
	mux.Handle("POST /v1/public/searches", searchesPublic)
	mux.Handle("GET /v1/public/searches", searchesPublic)
	mux.Handle("POST /v1/public/searches/{id}/seen", searchesPublic)
	mux.Handle("POST /v1/public/searches/{id}/alerts", searchesPublic)
	mux.Handle("DELETE /v1/public/searches/{id}", searchesPublic)
	mux.Handle("POST /v1/public/push/token", pushPublic)
	mux.Handle("POST /v1/public/push/token/drop", pushPublic)
	mux.Handle("POST /v1/public/listings/{id}/applications", appsPublic)
	mux.Handle("GET /v1/public/applications", appsPublic)
	mux.Handle("POST /v1/public/applications/{id}/withdraw", appsPublic)
	discoveryhttp.NewInspections(inspections, logger).PublicRoutes(authz.NewRegistrar(mux, guard))
	// The guest side of HomeStay: the exact search path and its subtree, both
	// into the same Gin engine, inside the public rate bucket.
	mux.Handle("GET /v1/public/stay", stayPublic)
	mux.Handle("/v1/public/stay/", stayPublic)

	// The eSign docUrl fetch, #212 — outside authentication for the webhook's
	// reason: the caller is a signer following an ESP's hyperlink, not a
	// sign-in, and the HMAC grant in the path is the credential. The source is
	// nil until the eSign flow (#62) exists; so does issuance, so a live grant
	// arriving today would be a wiring bug, and the handler refuses it loudly.
	if cfg.DocURLKey != "" {
		docSigner, err := docurl.NewSigner(cfg.DocURLKey)
		if err != nil {
			return fmt.Errorf("docurl signer: %w", err)
		}
		docurl.NewHandler(docSigner, docurl.NewStore(pool), nil, logger).
			Routes(authz.NewRegistrar(mux, guard))
	} else {
		logger.Warn("no docUrl signing key; the eSign document route is not registered",
			"set", "DOCURL_KEY")
	}
	// The organisation a request acts for. Verification says who signed in;
	// this says whose rows they may touch, and it is a database lookup rather
	// than a token claim for the reason ADR-0027 §6 gives.
	if cfg.Identity.Enforce {
		// Onboarding sits inside verification and outside the resolver: its
		// caller has, by definition, no membership to resolve. Issue #31.
		onboardingMux := http.NewServeMux()
		identityhttp.NewOnboarding(principals, logger).Routes(authz.NewRegistrar(onboardingMux, guard))
		mux.Handle("POST /v1/onboarding", auth.Middleware(verifier, onboardingMux))
		// Email verification sits beside it, and before it: onboarding refuses a
		// password sign-in whose address has not been proved. #282.
		if !cfg.Mail.Configured() {
			logger.Warn("no mail provider; email verification cannot send a code",
				"set", "RESEND_API_KEY, RESEND_FROM")
		}
		identityhttp.NewVerification(principals, mail.New(cfg.Mail), logger).
			Routes(authz.NewRegistrar(onboardingMux, guard))
		mux.Handle("/v1/verification/", auth.Middleware(verifier, onboardingMux))
		// The profile sits beside onboarding for the same reason: a verified
		// sign-in editing their own name needs no membership resolved. #240.
		mux.Handle("/v1/me", auth.Middleware(verifier, onboardingMux))

		resolver := identityservice.NewResolver(principals, logger)
		mux.Handle("/", auth.Middleware(verifier, resolver.Middleware(protected)))
		mux.Handle("/v1/resident/", auth.Middleware(verifier,
			auth.RequireSurface(auth.SurfaceLive, residents.Middleware(residentMux))))
		// The owner surface admits only the Own app's sign-ins, the resident
		// surface's reasoning in the other direction: a genuine Ops token
		// presented here is refused before any query runs.
		mux.Handle("/v1/owner/", auth.Middleware(verifier,
			auth.RequireSurface(auth.SurfaceOwn, resolver.Middleware(ownerMux))))
		// Same shape, the Ops app's own sign-ins only.
		mux.Handle("/v1/ops/", auth.Middleware(verifier,
			auth.RequireSurface(auth.SurfaceOps, resolver.Middleware(opsSurface))))
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
		// Dev only, like everything in this branch: the Own app pointed at a
		// local API reads the impersonated organisation's portfolio.
		mux.Handle("/v1/owner/", resolver.Middleware(ownerMux))
		mux.Handle("/v1/ops/", resolver.Middleware(opsSurface))
		// The apps gate on /v1/me. With authentication off there is no principal
		// to look one up by, so it answers the impersonated party — otherwise
		// every app pointed at a local API sits on the "name your firm" screen.
		mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{
				"party_id": "dev-impersonated", "display_name": "Dev impersonation",
			})
		})
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
//
// The docUrl route shares the limiter but not the bucket: an aggregator's
// delivery burst must not be shed because somebody is probing document URLs,
// and vice versa.
func webhookRoutes(r *http.Request) string {
	if strings.HasPrefix(r.URL.Path, "/v1/webhooks/") {
		return "webhooks"
	}
	if strings.HasPrefix(r.URL.Path, "/v1/esign/documents/") {
		return "esign-documents"
	}
	// The public listing site: anonymous by design, so limited as one shared
	// bucket rather than per caller. Its own bucket, not the webhooks' — a
	// scraper must not shed a payment confirmation.
	if strings.HasPrefix(r.URL.Path, "/v1/public/") {
		return "public-discovery"
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

// documentScanner builds the identity-document reader (#318). Nil, not an
// error, when no engine is named: prefill is an aid and the forms are typed by
// hand without it. The credential is workload identity, as the blob store's is
// — nothing about Vision belongs in a secret this service holds.
func documentScanner(engine string, log *slog.Logger) (*docscan.Scanner, error) {
	switch engine {
	case "":
		log.Warn("no document scanner; identity documents are typed in by hand",
			"set", "DOCSCAN_ENGINE=vision")
		return nil, nil
	case "vision":
		src, err := google.DefaultTokenSource(context.Background(),
			"https://www.googleapis.com/auth/cloud-platform")
		if err != nil {
			return nil, fmt.Errorf("vision credentials: %w", err)
		}
		return docscan.New(docscan.NewVision(func(ctx context.Context) (string, error) {
			t, err := src.Token()
			if err != nil {
				return "", err
			}
			return t.AccessToken, nil
		})), nil
	default:
		return nil, fmt.Errorf("%q is not a document engine", engine)
	}
}
