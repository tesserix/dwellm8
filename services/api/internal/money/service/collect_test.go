package service_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// Issue #42's primary scenario, end to end: a success webhook delivered five
// times, twice out of order, must leave exactly one set of ledger postings.
//
// The webhook tests above assert the delivery path in isolation with a spy. This
// asserts the thing the spy cannot: that the money reaches the ledger once.

// fake is the aggregator's answer, held wherever the test puts it. It is not a
// mock of Cashfree: the only thing varying in these tests is how many times that
// answer is asked for.
type fake struct {
	status collect.Status
	order  string
}

func (f *fake) Name() string { return "fake" }
func (f *fake) CreateOrder(context.Context, provider.OrderRequest) (provider.Order, error) {
	return provider.Order{ProviderOrderID: f.order}, nil
}
func (f *fake) Confirm(context.Context, string) (provider.Confirmation, error) {
	return provider.Confirmation{Status: f.status}, nil
}
func (f *fake) VerifyWebhook(provider.Webhook) bool { return true }
func (f *fake) Supports(m collect.Method) bool      { return true }

type harness struct {
	t        *testing.T
	svc      *service.Payments
	provider *fake
	ledger   *store.Ledger
	ctx      context.Context
	lease    string
	unit     string
	payer    string
	token    string
}

func newHarness(t *testing.T) harness {
	t.Helper()
	req, plat := requestPool(t), platformPool(t)
	isolationtest.SeedPropertyTree(t, tenancy.NewPlatformPool(plat))

	tok := token(t)
	unit, lease, payer := id(tok, "1"), id(tok, "2"), id(tok, "3")
	if err := tenancy.Platform(context.Background(), tenancy.NewPlatformPool(plat),
		"seeding the collection contract", func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft)
				VALUES ($1, $2, $3, 'flat', $4, 4, 600.00)`,
				unit, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				"COL-"+tok[:6]); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, $4, 'active', date '2026-01-01', date '2026-12-31')`,
				lease, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unit); err != nil {
				return err
			}
			// ADR-0024: a tenancy does not start without the two facts that decide
			// which TDS section governs every payment made under it.
			return isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgOwner.String(),
				lease, "2026-01-01")
		}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	adapter := &fake{status: collect.StatusAttempted, order: "order-" + tok}
	registry := provider.NewRegistry()
	registry.Register(adapter)
	if err := registry.SetChain("fake"); err != nil {
		t.Fatalf("registering the fake provider: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	return harness{
		t:        t,
		svc:      service.NewPayments(store.NewPayments(req), store.NewInbox(tenancy.NewPlatformPool(plat)), registry, log),
		provider: adapter,
		ledger:   store.NewLedger(req),
		ctx:      tenancy.With(context.Background(), isolationtest.OrgOwner),
		lease:    lease, unit: unit, payer: payer, token: tok,
	}
}

func requestPool(t *testing.T) *pgxpool.Pool { return connect(t, "TEST_DATABASE_URL") }
func platformPool(t *testing.T) *pgxpool.Pool {
	return connect(t, "TEST_PLATFORM_DATABASE_URL")
}

func connect(t *testing.T, env string) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv(env)
	if dsn == "" {
		t.Skip(env + " is not set — skipping the collection contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func token(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("token: %v", err)
	}
	return hex.EncodeToString(b)
}

func id(tok, nth string) string {
	return fmt.Sprintf("%s-%s-4000-8000-%s%s", tok[:8], tok[8:12], nth, tok[:11])
}

// charge raises what the collection is going to pay.
func (h harness) charge(amount domain.Minor, on time.Time) {
	h.t.Helper()
	e, err := domain.Invoice(amount, 0,
		domain.Place{Property: isolationtest.PropertyGranted, Unit: h.unit},
		h.payer, isolationtest.OrgOwner.String(),
		domain.Source{
			Kind: domain.SourceLeaseCharge, ID: h.lease,
			IdempotencyKey: h.token + "-charge", OccurredOn: on, Lease: h.lease,
		})
	if err != nil {
		h.t.Fatalf("building the charge: %v", err)
	}
	if _, err := h.ledger.Post(h.ctx, e); err != nil {
		h.t.Fatalf("raising the charge: %v", err)
	}
}

func (h harness) collect(amount domain.Minor) collect.Payment {
	h.t.Helper()
	p, _, err := h.svc.Collect(h.ctx, service.CollectRequest{
		TenantID:       isolationtest.OrgOwner.String(),
		Property:       isolationtest.PropertyGranted,
		Unit:           h.unit,
		Lease:          h.lease,
		PayerID:        h.payer,
		Amount:         amount,
		Method:         collect.MethodUPIIntent,
		IdempotencyKey: h.token + "-collect",
	})
	if err != nil {
		h.t.Fatalf("collecting: %v", err)
	}
	return p
}

// attempted is where a real collection is by the time a success webhook arrives:
// the payer has opened their app. created -> captured is not a legal move.
func (h harness) attempted(p collect.Payment) collect.Payment {
	h.t.Helper()
	h.provider.status = collect.StatusAttempted
	got, err := h.svc.Confirm(h.ctx, p)
	if err != nil {
		h.t.Fatalf("moving the payment to attempted: %v", err)
	}
	h.provider.status = collect.StatusCaptured
	return got
}

func TestAConfirmedCapturePostsOnceHoweverOftenItIsConfirmed(t *testing.T) {
	h := newHarness(t)
	on := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	h.charge(2_000_000, on)

	p := h.collect(1_200_000)
	if p.EntryID != "" {
		t.Fatal("a created payment already names a ledger entry")
	}
	p = h.attempted(p)
	if p.EntryID != "" {
		t.Fatal("an attempted payment posted to the ledger — only a capture moves money")
	}

	// Five confirmations of the same capture, which is what five deliveries of the
	// same success webhook produce.
	var entryID string
	for i := range 5 {
		got, err := h.svc.Confirm(h.ctx, p)
		if err != nil {
			t.Fatalf("confirmation %d: %v", i, err)
		}
		switch {
		case got.EntryID == "":
			t.Fatalf("confirmation %d captured the payment and posted nothing", i)
		case entryID == "":
			entryID = got.EntryID
		case got.EntryID != entryID:
			t.Fatalf("confirmation %d posted entry %s, and the first posted %s", i, got.EntryID, entryID)
		}
		// The payment as the next delivery would find it.
		p = got
	}

	// The receipt cleared 12 lakh of a 20 lakh charge, exactly once. Asked as of
	// today, because a receipt's accounting date is when the money arrived — as of
	// the charge date the charge stands alone, which the next assertion checks.
	if got, want := h.position(time.Now()), domain.Minor(800_000); got != want {
		t.Errorf("the lease owes %s after one payment of %s against a charge of %s, want %s",
			got, domain.Minor(1_200_000), domain.Minor(2_000_000), want)
	}
	if got, want := h.position(on), domain.Minor(2_000_000); got != want {
		t.Errorf("as of the charge date the lease owes %s, want %s — a receipt has been "+
			"backdated to before it arrived", got, want)
	}
}

// A confirmation arriving after the payment has moved on writes nothing — and,
// crucially, posts nothing. An entry with no payment pointing at it is money
// counted twice.
func TestAStaleConfirmationPostsNothing(t *testing.T) {
	h := newHarness(t)
	on := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	h.charge(1_000_000, on)

	p := h.attempted(h.collect(1_000_000))
	captured, err := h.svc.Confirm(h.ctx, p)
	if err != nil {
		t.Fatalf("confirming: %v", err)
	}

	// The webhook that arrived late, carrying the payment as it was before.
	if _, err := h.svc.Confirm(h.ctx, p); err != nil {
		t.Fatalf("the late confirmation errored rather than being ignored: %v", err)
	}

	entries := h.entriesFor(captured.ID)
	if entries != 1 {
		t.Errorf("%d entries reference payment %s, want 1", entries, captured.ID)
	}
	if got := h.position(time.Now()); got != 0 {
		t.Errorf("the lease owes %s after being paid in full, want 0", got)
	}
}

func (h harness) position(on time.Time) domain.Minor {
	h.t.Helper()
	got, err := h.ledger.Position(h.ctx, h.lease, on)
	if err != nil {
		h.t.Fatalf("position: %v", err)
	}
	return got
}

func (h harness) entriesFor(payment string) int {
	h.t.Helper()
	var n int
	err := tenancy.Scoped(h.ctx, requestPool(h.t), func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM journal_entries WHERE source_kind = 'payment' AND source_id = $1`,
			payment).Scan(&n)
	})
	if err != nil {
		h.t.Fatalf("counting entries: %v", err)
	}
	return n
}
