package ops_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	moneyprovider "github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
	propertystore "github.com/tesserix/dwellm8/services/api/internal/property/store"
	"github.com/tesserix/dwellm8/services/api/internal/surface/ops"
)

// Rent handed over in cash, by cheque, or as a transfer the manager watched
// land. It goes through the same collection a tenant's own tap goes through —
// one idempotency contract, one ledger posting, one receipt (#297).

func serveCollections(t *testing.T) (*http.ServeMux, tenancy.PlatformPool) {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	platDSN := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" || platDSN == "" {
		t.Skip("TEST_DATABASE_URL and TEST_PLATFORM_DATABASE_URL are not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)
	plat, err := pgxpool.New(ctx, platDSN)
	if err != nil {
		t.Fatalf("connecting as the platform role: %v", err)
	}
	t.Cleanup(plat.Close)

	platPool := tenancy.NewPlatformPool(plat)
	isolationtest.SeedResidentFixtures(t, platPool)
	seedOwnership(t, platPool)

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	paymentStore := moneystore.NewPayments(pool)
	statements := moneyservice.NewStatements(moneystore.NewLedger(pool), paymentStore, nil)
	payments := moneyservice.NewPayments(paymentStore, moneystore.NewInbox(platPool),
		moneyprovider.NewRegistry(), log)
	residents := identityservice.NewResidents(identitystore.New(platPool), log)

	mux := http.NewServeMux()
	h := ops.New(
		propertyservice.New(propertystore.New(pool)),
		leaseservice.NewLeases(leasestore.New(pool), log),
		statements, residents, nil, log, nil,
	).WithPayments(payments)
	registrar := authz.NewRegistrar(mux, &authz.Guard{})
	h.Routes(registrar)
	h.CollectionRoutes(registrar)
	return mux, platPool
}

type collected struct {
	PaymentID    string `json:"payment_id"`
	Status       string `json:"status"`
	DueMinor     int64  `json:"due_amount_minor"`
	AdvanceMinor int64  `json:"advance_amount_minor"`
}

// The suite commits, so these tests get a tenancy of their own rather than
// spending the shared fixture's balance — every neighbouring test asserts that
// fixture owes exactly one unpaid invoice.
type tenancyUnderTest struct {
	lease string
	token string
}

func seedTenancy(t *testing.T, plat tenancy.PlatformPool) tenancyUnderTest {
	return seedTenancyFrom(t, plat, "2026-01-01", "2026-12-31")
}

func seedTenancyFrom(t *testing.T, plat tenancy.PlatformPool, from, to string) tenancyUnderTest {
	t.Helper()
	tok := token(t)
	unit, lease, party := uuidFrom(tok, "1"), uuidFrom(tok, "2"), uuidFrom(tok, "3")
	err := tenancy.Platform(context.Background(), plat, "seeding a tenancy to collect from",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft)
				VALUES ($1, $2, $3, 'flat', $4, 9, 620.00)`,
				unit, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				"COLL-"+tok[:6]); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, $4, 'active', $5::date, $6::date)`,
				lease, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, unit,
				from, to); err != nil {
				return err
			}
			if err := isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgOwner.String(),
				lease, from); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO lease_parties (tenant_id, lease_id, party_id, role, valid_from)
				VALUES ($1, $2, $3, 'tenant', $4::date)`,
				isolationtest.OrgOwner.String(), lease, party, from); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, due_day, valid_from)
				VALUES ($1, $2, 2500000, 5, $3::date)`,
				isolationtest.OrgOwner.String(), lease, from)
			return err
		})
	if err != nil {
		t.Fatalf("seeding a tenancy: %v", err)
	}
	return tenancyUnderTest{lease: lease, token: tok}
}

func token(t *testing.T) string {
	t.Helper()
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generating a token: %v", err)
	}
	return hex.EncodeToString(b[:])
}

func uuidFrom(tok, nth string) string {
	return fmt.Sprintf("%s-%s-4000-8000-%s%s", tok[:8], tok[8:12], nth, tok[:11])
}

func (tt tenancyUnderTest) path() string {
	return "/v1/ops/tenancies/" + tt.lease + "/collections"
}

func TestRecordingRentTakenInCashClearsWhatIsOwed(t *testing.T) {
	mux, plat := serveCollections(t)
	tt := seedTenancy(t, plat)

	w := post(t, mux, isolationtest.OrgOwner, tt.path(), map[string]any{
		"amount_minor":    100000,
		"method":          "offline_cash",
		"reference":       "receipt book 41",
		"idempotency_key": tt.token,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("recording the payment: %d %s", w.Code, w.Body.String())
	}
	var got collected
	decode(t, w, &got)
	if got.Status != "captured" {
		t.Errorf("cash in hand is captured the moment it is recorded, got %q", got.Status)
	}
	if got.PaymentID == "" {
		t.Error("the payment came back without an id")
	}

	// Nothing has been charged on this tenancy yet, so ₹1,000 received is
	// ₹1,000 the tenant is ahead. Either way the receipt reached the ledger.
	if got.AdvanceMinor != 100000 {
		t.Errorf("advance_amount_minor = %d after ₹1,000 against no charge, want 100000", got.AdvanceMinor)
	}
	if got.DueMinor != 0 {
		t.Errorf("due_amount_minor = %d, want 0 — owing less than nothing is owing nothing", got.DueMinor)
	}
}

// The deposit and the first month are handed over at signing, before the term
// starts. There is a tenant on the tenancy — just not one live today (#303).
func TestRecordingTheDepositBeforeTheTermStarts(t *testing.T) {
	mux, plat := serveCollections(t)
	tt := seedTenancyFrom(t, plat, "2027-03-01", "2028-02-29")

	w := post(t, mux, isolationtest.OrgOwner, tt.path(), map[string]any{
		"amount_minor":    15000000,
		"method":          "offline_cash",
		"reference":       "deposit, on signing",
		"idempotency_key": tt.token,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("recording the deposit: %d %s", w.Code, w.Body.String())
	}
	var got collected
	decode(t, w, &got)
	if got.AdvanceMinor != 15000000 {
		t.Errorf("advance_amount_minor = %d, want the whole deposit 15000000", got.AdvanceMinor)
	}
}

func TestTheSameOfflineCollectionTwiceIsOneCollection(t *testing.T) {
	mux, plat := serveCollections(t)
	tt := seedTenancy(t, plat)
	body := map[string]any{
		"amount_minor":    50000,
		"method":          "offline_transfer",
		"reference":       "NEFT ending 8821",
		"idempotency_key": tt.token,
	}

	first := post(t, mux, isolationtest.OrgOwner, tt.path(), body)
	if first.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", first.Code, first.Body.String())
	}
	var one, two collected
	decode(t, first, &one)

	second := post(t, mux, isolationtest.OrgOwner, tt.path(), body)
	if second.Code != http.StatusCreated {
		t.Fatalf("second: %d %s", second.Code, second.Body.String())
	}
	decode(t, second, &two)
	if one.PaymentID != two.PaymentID {
		t.Errorf("a retry made a second payment: %s then %s", one.PaymentID, two.PaymentID)
	}
	if one.DueMinor != two.DueMinor {
		t.Errorf("a retry moved the balance from %d to %d", one.DueMinor, two.DueMinor)
	}
}

// Offline means a person witnessed the money. A manager asserting a UPI
// collection by hand would be recording a receipt nobody saw.
func TestAnOnlineMethodIsNotSomethingAManagerCanClaim(t *testing.T) {
	mux, plat := serveCollections(t)
	tt := seedTenancy(t, plat)
	w := post(t, mux, isolationtest.OrgOwner, tt.path(), map[string]any{
		"amount_minor":    10000,
		"method":          "upi_intent",
		"idempotency_key": tt.token,
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a manager recording a UPI collection by hand: %d %s", w.Code, w.Body.String())
	}
}

func TestARecordedCollectionNeedsAPositiveAmount(t *testing.T) {
	mux, plat := serveCollections(t)
	tt := seedTenancy(t, plat)
	w := post(t, mux, isolationtest.OrgOwner, tt.path(), map[string]any{
		"amount_minor":    0,
		"method":          "offline_cash",
		"idempotency_key": tt.token,
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a receipt for nothing: %d %s", w.Code, w.Body.String())
	}
}

func TestCannotRecordAgainstAnotherOrganisationsTenancy(t *testing.T) {
	mux, plat := serveCollections(t)
	tt := seedTenancy(t, plat)
	w := post(t, mux, isolationtest.OrgOutsider, tt.path(), map[string]any{
		"amount_minor":    10000,
		"method":          "offline_cash",
		"idempotency_key": tt.token,
	})
	if w.Code != http.StatusNotFound {
		t.Fatalf("an outsider recording rent on somebody else's tenancy: %d %s", w.Code, w.Body.String())
	}
}
