package service_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The fee against a real database. Issue #234.
//
// The arithmetic is tested in internal/money/domain without one. What needs
// PostgreSQL is that the rate resolves to exactly one rule on a given day, and
// that a collection which cannot be split accrues rather than silently earning
// nothing.

func fees(t *testing.T) (*service.Fees, *store.Fees, context.Context) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	rules := store.NewFees(pool)
	svc := service.NewFees(rules, store.NewPayoutAccounts(pool), store.NewLedger(pool), log)
	return svc, rules, tenancy.With(context.Background(), isolationtest.OrgOwner)
}

// The seeded default. A collection always has a price, because a fee that
// silently becomes zero is revenue lost without a trace.
func TestTheSeededRateIsTwoPointNineNine(t *testing.T) {
	_, rules, ctx := fees(t)

	s, err := rules.Schedule(ctx, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolving the schedule: %v", err)
	}
	if s.Rate != 299 {
		t.Errorf("rate %s, want 2.99%%", s.Rate)
	}
	if s.TaxRate != 1800 || s.TaxInclusive {
		t.Errorf("tax %s inclusive=%v, want 18%% charged on top", s.TaxRate, s.TaxInclusive)
	}
	if s.RuleID == "" {
		t.Error("the schedule came back without the rule that priced it")
	}
}

// Before the rule's window there is no price, and that is an error rather than a
// free collection.
func TestADayWithNoRuleIsRefused(t *testing.T) {
	_, rules, ctx := fees(t)

	_, err := rules.Schedule(ctx, time.Date(2020, time.January, 1, 0, 0, 0, 0, time.UTC))
	if !errors.Is(err, store.ErrNoRule) {
		t.Fatalf("a day before any rule resolved to %v, want ErrNoRule", err)
	}
}

// A rate change is a row, and the rate that applied on the day the money moved
// is the rate that applies — not today's.
func TestARateChangeIsEffectiveDated(t *testing.T) {
	_, rules, ctx := fees(t)
	plat := platformPoolForFees(t)

	// Close the default at the end of 2026 and price 2027 differently.
	err := tenancy.Platform(context.Background(), plat, "testing an effective-dated rate change",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				UPDATE platform_fee_rules SET valid_to = date '2027-01-01'
				 WHERE id = 'f0000000-0000-0000-0000-000000000001'`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO platform_fee_rules (id, rate_bps, tax_rate_bps, valid_from,
				                                owner, review_by, source)
				VALUES ('f0000000-0000-0000-0000-0000000000ff', 350, 1800, date '2027-01-01',
				        'Product', date '2028-01-01', 'test')
				ON CONFLICT (id) DO NOTHING`)
			return err
		})
	if err != nil {
		t.Fatalf("seeding the change: %v", err)
	}
	t.Cleanup(func() {
		_ = tenancy.Platform(context.Background(), plat, "undoing the rate change",
			func(ctx context.Context, tx pgx.Tx) error {
				if _, err := tx.Exec(ctx,
					`DELETE FROM platform_fee_rules WHERE id = 'f0000000-0000-0000-0000-0000000000ff'`); err != nil {
					return err
				}
				_, err := tx.Exec(ctx, `
					UPDATE platform_fee_rules SET valid_to = NULL
					 WHERE id = 'f0000000-0000-0000-0000-000000000001'`)
				return err
			})
	})

	before, err := rules.Schedule(ctx, time.Date(2026, time.December, 31, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolving before the change: %v", err)
	}
	after, err := rules.Schedule(ctx, time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("resolving after the change: %v", err)
	}
	if before.Rate != 299 || after.Rate != 350 {
		t.Fatalf("rates %s then %s, want 2.99%% then 3.5%%", before.Rate, after.Rate)
	}
}

// An offline payment cannot be split — the money never touches this platform —
// so the fee accrues. This is the majority of Indian rent, not an edge case.
func TestAnOfflinePaymentAccruesTheFee(t *testing.T) {
	svc, _, ctx := fees(t)

	q, err := svc.Quote(ctx, 2_500_000, isolationtest.ResidentPriya,
		collect.MethodOfflineCash, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("quoting: %v", err)
	}
	if q.Retained() {
		t.Fatal("an offline payment produced a split — there is no money here to split")
	}
	if q.Accrual != service.AccrualOffline {
		t.Errorf("accrual %q, want offline", q.Accrual)
	}
	if q.Fee.Net != 74_750 || q.Fee.Tax != 13_455 {
		t.Errorf("an accrued fee is still a fee: got %s + %s tax", q.Fee.Net, q.Fee.Tax)
	}
}

// A bearer the aggregator has never been told about. The collection still
// happens — refusing a tenant's rent because we could not arrange our own fee
// would be the wrong trade every time.
func TestABearerWithNoVendorAccruesRatherThanFailing(t *testing.T) {
	svc, _, ctx := fees(t)

	q, err := svc.Quote(ctx, 2_500_000, "d1111111-0000-0000-0000-0000000000ee",
		collect.MethodUPIIntent, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("quoting for an unknown bearer: %v", err)
	}
	if q.Retained() {
		t.Fatal("a bearer with no payout account produced a split")
	}
	if q.Accrual != service.AccrualNoVendor {
		t.Errorf("accrual %q, want no_vendor", q.Accrual)
	}
	if q.Fee.Zero() {
		t.Error("the fee was skipped rather than accrued — that is revenue lost without a trace")
	}
}

// The invariant that matters to reconciliation, asserted on what the adapter
// would actually be sent.
func TestTheVendorLegAndTheRetainedLegAddBackToTheCollection(t *testing.T) {
	svc, _, ctx := fees(t)

	q, err := svc.Quote(ctx, 2_500_000, isolationtest.ResidentPriya,
		collect.MethodOfflineCash, time.Date(2026, time.August, 1, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("quoting: %v", err)
	}
	if q.Fee.Retained+q.Fee.Vendor != domain.Minor(2_500_000) {
		t.Fatalf("%s retained and %s to the vendor do not make the collection",
			q.Fee.Retained, q.Fee.Vendor)
	}
}

func platformPoolForFees(t *testing.T) tenancy.PlatformPool {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting as the platform role: %v", err)
	}
	t.Cleanup(p.Close)
	return tenancy.NewPlatformPool(p)
}

// The whole of ADR-0031 in one pass: a collection is priced, the split is put on
// the order, the capture posts the fee, and a redelivered confirmation posts it
// once. Built on collect_test.go's harness rather than a second fixture.
func TestACapturePostsThePlatformFeeOnce(t *testing.T) {
	h := newHarness(t)
	svc := h.svc.WithFees(service.NewFees(
		store.NewFees(requestPool(t)),
		store.NewPayoutAccounts(requestPool(t)),
		store.NewLedger(requestPool(t)),
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))))

	p, _, err := svc.Collect(h.ctx, service.CollectRequest{
		TenantID: isolationtest.OrgOwner.String(),
		Property: isolationtest.PropertyGranted, Unit: h.unit, Lease: h.lease,
		PayerID: h.payer, Amount: 2_500_000, Method: collect.MethodUPIIntent,
		// The owner bears it. No vendor is registered, so it accrues — which is
		// the case that must still post, not the case that may be skipped.
		Bearer:         h.payer,
		IdempotencyKey: "fee-" + h.token,
	})
	if err != nil {
		t.Fatalf("collecting: %v", err)
	}

	// A payment walks forward: created, attempted, then captured. The fake
	// provider answers whatever it is told to, and the state machine still
	// refuses a jump.
	h.provider.status = collect.StatusAttempted
	p, err = svc.Confirm(h.ctx, p)
	if err != nil {
		t.Fatalf("moving to attempted: %v", err)
	}

	h.provider.status = collect.StatusCaptured
	// Twice, because a provider redelivers, and the fee must post once.
	for i := range 2 {
		if _, err := svc.Confirm(h.ctx, p); err != nil {
			t.Fatalf("confirmation %d: %v", i, err)
		}
	}

	balances, err := h.ledger.Balances(h.ctx, store.BalanceQuery{
		Lease: h.lease, Account: domain.PlatformFeeIncome})
	if err != nil {
		t.Fatalf("reading the fee income: %v", err)
	}
	var earned domain.Minor
	for _, b := range balances {
		earned += b.Amount
	}
	// Income is a credit, so the signed sum is negative: 2.99% of 2,500,000.
	if earned != -74_750 {
		t.Fatalf("platform fee income %s, want -74750 posted exactly once", earned)
	}
}
