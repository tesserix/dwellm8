package resident_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/surface/resident"
)

// The tenant view, end to end against a real database. Issue #51.
//
// The handler is exercised through an http.ServeMux rather than called directly,
// because half of what this story is about lives in the routing: a receipt is
// reached through the tenancy it belongs to, and the path is what makes that
// true.
//
// The session is constructed rather than resolved from a token. Verifying a GIP
// token is tested in internal/platform/auth against keys the test generates;
// what is under test here is what a *resolved* renter can see, and standing up
// Identity Platform to assert it would test Google.

func serve(t *testing.T) (*http.ServeMux, *pgxpool.Pool) {
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

	isolationtest.SeedResidentFixtures(t, tenancy.NewPlatformPool(plat))

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	payments := moneystore.NewPayments(pool)
	mux := http.NewServeMux()
	resident.New(
		leaseservice.NewLeases(leasestore.New(pool), log),
		moneyservice.NewStatements(moneystore.NewLedger(pool), payments, nil),
		moneyservice.NewPayments(payments, moneystore.NewInbox(tenancy.NewPlatformPool(plat)),
			provider.NewRegistry(), log),
		log, nil,
	).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux, pool
}

// priya is the renter of the isolation fixtures: one flat from the owner, one
// from a second landlord.
func priya() identityservice.Session {
	return identityservice.Session{
		PartyID: isolationtest.ResidentPriya,
		Phone:   "+919876500001",
		Residencies: []identityservice.Residency{
			{LeaseID: isolationtest.LeasePriyaOwner, TenantID: isolationtest.OrgOwner,
				Organisation: "Harness Owner", State: "active"},
			{LeaseID: isolationtest.LeasePriyaOther, TenantID: isolationtest.OrgOutsider,
				Organisation: "Second Landlord", State: "active"},
		},
	}
}

func call(t *testing.T, mux *http.ServeMux, s identityservice.Session, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(identityservice.WithSession(r.Context(), s)))
	return w
}

// The story's primary scenario, minus the OTP: a renter opens the link and sees
// where they live, what they owe and why.
func TestARenterSeesTheirTenanciesAcrossLandlords(t *testing.T) {
	mux, _ := serve(t)

	w := call(t, mux, priya(), http.MethodGet, "/v1/resident/tenancies", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET tenancies: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Tenancies []struct {
			LeaseID      string `json:"lease_id"`
			Organisation string `json:"organisation"`
			Unit         string `json:"unit"`
			RentMinor    int64  `json:"rent_amount_minor"`
			DueDay       int    `json:"due_day"`
			Dues         struct {
				DueMinor  int64 `json:"due_amount_minor"`
				RentMinor int64 `json:"rent_amount_minor"`
			} `json:"dues"`
		} `json:"tenancies"`
	}
	decode(t, w, &out)

	if len(out.Tenancies) != 2 {
		t.Fatalf("a renter with two landlords sees %d tenancies, want 2 — the surface reads one "+
			"organisation at a time and both must appear", len(out.Tenancies))
	}
	for _, tn := range out.Tenancies {
		if tn.RentMinor != 2500000 || tn.DueDay != 5 {
			t.Errorf("%s: rent %d due on the %d, want 2500000 on the 5th — the schedule in force "+
				"is what the tenant is shown", tn.Unit, tn.RentMinor, tn.DueDay)
		}
		// One invoice was raised and nothing paid, so what is owed is that
		// invoice — derived from postings, never from a column.
		if tn.Dues.RentMinor != 2500000 || tn.Dues.DueMinor != 2500000 {
			t.Errorf("%s: dues %+v, want 2500000 owed against 2500000 of rent", tn.Unit, tn.Dues)
		}
	}
	if out.Tenancies[0].Organisation == out.Tenancies[1].Organisation {
		t.Errorf("both tenancies name the same landlord — the two are meant to be distinct organisations")
	}
}

// The story's failure scenario. Changing the identifier in the URL is answered
// with "no such tenancy", and it is answered the same way whether the lease
// exists or not.
func TestChangingTheLeaseInTheURLDisclosesNothing(t *testing.T) {
	mux, _ := serve(t)

	// Rohit's lease: a real tenancy, of the same landlord, that is not hers.
	for _, path := range []string{
		"/v1/resident/tenancies/" + isolationtest.LeaseRohitOwner,
		"/v1/resident/tenancies/" + isolationtest.LeaseRohitOwner + "/history",
		"/v1/resident/tenancies/" + isolationtest.LeaseRohitOwner + "/payments/" +
			isolationtest.LeasePriyaOwner + "/receipt",
	} {
		w := call(t, mux, priya(), http.MethodGet, path, "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("GET %s answered %d, want 404 — a 403 would confirm the tenancy exists, which "+
				"is the whole of what somebody changing the id is trying to learn\n%s",
				path, w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), isolationtest.LeaseRohitOwner) {
			t.Fatalf("GET %s echoed the lease id back — the refusal must disclose nothing", path)
		}
	}

	// And a lease that does not exist at all reads identically.
	absent := call(t, mux, priya(), http.MethodGet,
		"/v1/resident/tenancies/e1111111-0000-0000-0000-0000000000ff", "")
	if absent.Code != http.StatusNotFound {
		t.Fatalf("an absent tenancy answered %d, want 404", absent.Code)
	}
}

// A payment is posted for the tenant, by the tenant, against their own tenancy —
// and the request cannot name anybody else, because it names nobody at all.
func TestARenterPaysTheirOwnRent(t *testing.T) {
	mux, _ := serve(t)
	key := "test-" + time.Now().UTC().Format("20060102150405.000000000")

	w := call(t, mux, priya(), http.MethodPost,
		"/v1/resident/tenancies/"+isolationtest.LeasePriyaOwner+"/payments",
		`{"amount_minor":2500000,"method":"offline_transfer","idempotency_key":"`+key+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("POST payments: %d %s", w.Code, w.Body.String())
	}
	var first struct {
		PaymentID string `json:"payment_id"`
		Status    string `json:"status"`
	}
	decode(t, w, &first)
	if first.PaymentID == "" || first.Status != "created" {
		t.Fatalf("payment %+v, want a created payment", first)
	}

	// The same key again is the same payment. A tenant on a bad connection
	// pressing Pay twice must not owe twice.
	again := call(t, mux, priya(), http.MethodPost,
		"/v1/resident/tenancies/"+isolationtest.LeasePriyaOwner+"/payments",
		`{"amount_minor":2500000,"method":"offline_transfer","idempotency_key":"`+key+`"}`)
	var second struct {
		PaymentID string `json:"payment_id"`
	}
	decode(t, again, &second)
	if second.PaymentID != first.PaymentID {
		t.Fatalf("a repeated idempotency key created a second payment (%s then %s)",
			first.PaymentID, second.PaymentID)
	}
}

func TestAPaymentForZeroIsRefused(t *testing.T) {
	mux, _ := serve(t)
	w := call(t, mux, priya(), http.MethodPost,
		"/v1/resident/tenancies/"+isolationtest.LeasePriyaOwner+"/payments",
		`{"amount_minor":0,"idempotency_key":"zero"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a payment of nothing answered %d, want 422", w.Code)
	}
}

// An unknown field is refused rather than ignored: a client sending "amount"
// would otherwise pay zero and be told it worked.
func TestAMisspeltAmountIsRefused(t *testing.T) {
	mux, _ := serve(t)
	w := call(t, mux, priya(), http.MethodPost,
		"/v1/resident/tenancies/"+isolationtest.LeasePriyaOwner+"/payments",
		`{"amount":2500000,"idempotency_key":"typo"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("a misspelt amount answered %d, want 400", w.Code)
	}
}

// History shows the charge that was raised. A payment that has not been received
// carries no receipt link — a link that 404s on the screen whose job is proving
// something happened is worse than no link.
func TestHistoryShowsChargesAndWithholdsUnearnedReceipts(t *testing.T) {
	mux, _ := serve(t)

	w := call(t, mux, priya(), http.MethodGet,
		"/v1/resident/tenancies/"+isolationtest.LeasePriyaOwner+"/history", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET history: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Charges []struct {
			Kind        string `json:"kind"`
			AmountMinor int64  `json:"amount_minor"`
		} `json:"charges"`
		Payments []struct {
			Status  string `json:"status"`
			Receipt string `json:"receipt"`
		} `json:"payments"`
	}
	decode(t, w, &out)

	if len(out.Charges) == 0 {
		t.Fatalf("no charges on a tenancy that has been invoiced")
	}
	for _, c := range out.Charges {
		if c.Kind == "invoice" && c.AmountMinor != 2500000 {
			t.Errorf("the invoice reads %d, want 2500000 — the tenant's line only, not the "+
				"owner's income against it", c.AmountMinor)
		}
	}
	for _, p := range out.Payments {
		if p.Status == "created" && p.Receipt != "" {
			t.Errorf("a payment that has not been received carries a receipt link — a tenant " +
				"would be holding proof of a payment that may still fail")
		}
	}
}

// The renter's own payment on their *other* tenancy is refused through this one.
// The policy would allow it — it is genuinely their payment — and the receipt
// would carry the wrong address, which is the document's whole purpose.
func TestAReceiptIsReachedThroughItsOwnTenancy(t *testing.T) {
	mux, _ := serve(t)
	key := "cross-" + time.Now().UTC().Format("20060102150405.000000000")

	made := call(t, mux, priya(), http.MethodPost,
		"/v1/resident/tenancies/"+isolationtest.LeasePriyaOther+"/payments",
		`{"amount_minor":100000,"method":"offline_transfer","idempotency_key":"`+key+`"}`)
	if made.Code != http.StatusCreated {
		t.Fatalf("POST payments on the second tenancy: %d %s", made.Code, made.Body.String())
	}
	var p struct {
		PaymentID string `json:"payment_id"`
	}
	decode(t, made, &p)

	w := call(t, mux, priya(), http.MethodGet,
		"/v1/resident/tenancies/"+isolationtest.LeasePriyaOwner+"/payments/"+p.PaymentID+"/receipt", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("a receipt from another tenancy answered %d, want 404", w.Code)
	}
}

// Nothing on this surface may be held by a shared cache. A CDN or a corporate
// proxy serving one renter's dues to the next is the cheapest possible way to
// leak this.
func TestEveryAnswerIsUncacheable(t *testing.T) {
	mux, _ := serve(t)
	for _, path := range []string{
		"/v1/resident/tenancies",
		"/v1/resident/tenancies/" + isolationtest.LeasePriyaOwner,
		"/v1/resident/tenancies/" + isolationtest.LeasePriyaOwner + "/history",
	} {
		w := call(t, mux, priya(), http.MethodGet, path, "")
		if cc := w.Header().Get("Cache-Control"); !strings.Contains(cc, "no-store") {
			t.Errorf("GET %s answered with Cache-Control %q, want no-store", path, cc)
		}
	}
}

// A request that reached the handler without a session is refused rather than
// served empty. A handler that has to remember to check is one that will forget.
func TestNoSessionIsRefused(t *testing.T) {
	mux, _ := serve(t)
	r := httptest.NewRequest(http.MethodGet, "/v1/resident/tenancies", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("a request with no session answered %d, want 401", w.Code)
	}
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), v); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
}
