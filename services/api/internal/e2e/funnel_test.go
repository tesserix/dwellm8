package e2e_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	discoveryhttp "github.com/tesserix/dwellm8/services/api/internal/discovery/http"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The whole funnel through HTTP, against a real database: an owner lists a
// vacant flat, a stranger finds it, verifies a phone, enquires; the owner
// responds over a masked bridge and converts the applicant — and the lease
// draft carries every field forward. Listing to rental, one test.

func harness(t *testing.T) (*http.ServeMux, string) {
	t.Helper()
	dsn, plat := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" || plat == "" {
		t.Skip("TEST_DATABASE_URL and TEST_PLATFORM_DATABASE_URL are not set")
	}
	req, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(req.Close)
	platPool, err := pgxpool.New(context.Background(), plat)
	if err != nil {
		t.Fatalf("connecting as platform: %v", err)
	}
	t.Cleanup(platPool.Close)
	p := tenancy.NewPlatformPool(platPool)
	isolationtest.SeedPropertyTree(t, p)

	var unit string
	code := "D-" + time.Now().Format("150405.000")
	if err := tenancy.Platform(context.Background(), p, "seeding a unit for the funnel test",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 780) RETURNING id`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, code).Scan(&unit)
		}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	today := func() effective.Date { n := time.Now(); return effective.Day(n.Year(), n.Month(), n.Day()) }

	residents := identityservice.NewResidents(identitystore.New(p), log)
	leases := leaseservice.NewLeases(leasestore.New(req), log).WithResidents(residents)

	listingsStore := store.NewListings(req)
	prospectsStore := store.NewProspects(p)
	enquiriesStore := store.NewEnquiries(req, p)
	listings := service.NewListings(listingsStore, store.NewPublic(req), today, log).
		WithOccupancy(leases)
	enquiries := service.NewEnquiries(enquiriesStore, listingsStore, prospectsStore, log).
		WithDrafter(service.FromLeases{Leases: leases}).
		WithBridges(service.DevBridge{Log: log})
	prospects := service.NewProspects(prospectsStore, service.DevVerifier{Log: log}, log)

	mux := http.NewServeMux()
	discoveryhttp.NewListings(listings, enquiries, log).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	discoveryhttp.NewPublic(listings, prospects, enquiries, log).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux, unit
}

func scoped(r *http.Request) *http.Request {
	return r.WithContext(tenancy.With(r.Context(), isolationtest.OrgOwner))
}

func do(t *testing.T, mux *http.ServeMux, r *http.Request, want int) map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != want {
		t.Fatalf("%s %s = %d, want %d — %s", r.Method, r.URL.Path, rec.Code, want, rec.Body)
	}
	var out map[string]any
	if rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("undecodable response %q: %v", rec.Body, err)
		}
	}
	return out
}

func TestListingToRental(t *testing.T) {
	mux, unit := harness(t)
	city := fmt.Sprintf("Funnelpur-%d", time.Now().UnixNano())

	// The owner drafts from inventory. Costs incomplete first: publication must
	// refuse until the disclosure is confirmed (#134).
	draftBody := func(confirmed bool) string {
		return fmt.Sprintf(`{
			"property_id": %q, "unit_id": %q,
			"headline": "Sunny 2BHK, covered parking", "locality": "Indiranagar",
			"city": %q, "state_code": "KA", "bedrooms": 2,
			"rent_minor": 3200000, "deposit_minor": 9600000,
			"maintenance_minor": 350000, "parking_minor": 100000,
			"costs_confirmed": %v}`, isolationtest.PropertyGranted, unit, city, confirmed)
	}

	out := do(t, mux, scoped(httptest.NewRequest("POST", "/v1/listings",
		strings.NewReader(draftBody(false)))), http.StatusCreated)
	unconfirmed := out["id"].(string)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, scoped(httptest.NewRequest("POST", "/v1/listings/"+unconfirmed+"/publish", nil)))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("publishing undisclosed costs = %d, want 422 — %s", rec.Code, rec.Body)
	}

	out = do(t, mux, scoped(httptest.NewRequest("POST", "/v1/listings",
		strings.NewReader(draftBody(true)))), http.StatusCreated)
	listing := out["id"].(string)
	do(t, mux, scoped(httptest.NewRequest("POST", "/v1/listings/"+listing+"/publish", nil)),
		http.StatusOK)

	// The stranger finds it, with the true cost totalled.
	out = do(t, mux, httptest.NewRequest("GET", "/v1/public/listings?city="+city, nil), http.StatusOK)
	cards := out["listings"].([]any)
	if len(cards) != 1 {
		t.Fatalf("search found %d listings, want 1", len(cards))
	}
	card := cards[0].(map[string]any)
	if card["total_monthly_minor"].(float64) != 3650000 {
		t.Fatalf("total_monthly_minor = %v, want 3650000", card["total_monthly_minor"])
	}

	// A browsing token, the verification point, and the enquiry.
	out = do(t, mux, httptest.NewRequest("POST", "/v1/public/prospects", strings.NewReader("{}")),
		http.StatusCreated)
	token := out["token"].(string)
	withToken := func(r *http.Request) *http.Request {
		r.Header.Set("X-Dwellm8-Prospect", token)
		return r
	}

	// Contact before verification is refused.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, withToken(httptest.NewRequest("POST", "/v1/public/enquiries",
		strings.NewReader(`{"listing_id": "`+listing+`", "message": "Is it available?"}`))))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unverified enquiry = %d, want 403 — %s", rec.Code, rec.Body)
	}

	do(t, mux, withToken(httptest.NewRequest("POST", "/v1/public/prospects/verify",
		strings.NewReader(`{"phone": "+919876501234"}`))), http.StatusOK)
	do(t, mux, withToken(httptest.NewRequest("POST", "/v1/public/prospects/confirm",
		strings.NewReader(`{"phone": "+919876501234", "code": "000000"}`))), http.StatusOK)

	do(t, mux, withToken(httptest.NewRequest("POST", "/v1/public/shortlist",
		strings.NewReader(`{"listing_id": "`+listing+`"}`))), http.StatusOK)
	out = do(t, mux, withToken(httptest.NewRequest("POST", "/v1/public/enquiries",
		strings.NewReader(`{"listing_id": "`+listing+`", "message": "Is it available?"}`))),
		http.StatusCreated)
	enquiry := out["id"].(string)

	// The owner's pipeline shows the enquiry with the masked contact — the only
	// form of the number that exists anywhere in this system.
	out = do(t, mux, scoped(httptest.NewRequest("GET", "/v1/enquiries?state=new", nil)), http.StatusOK)
	var pipelineRow map[string]any
	for _, e := range out["enquiries"].([]any) {
		if m := e.(map[string]any); m["id"] == enquiry {
			pipelineRow = m
		}
	}
	if pipelineRow == nil {
		t.Fatalf("the enquiry is not in the pipeline")
	}
	if pipelineRow["contact_masked"] != "XXXXXX1234" {
		t.Fatalf("contact_masked = %v, want XXXXXX1234", pipelineRow["contact_masked"])
	}

	// Responding opens the masked bridge.
	out = do(t, mux, scoped(httptest.NewRequest("POST", "/v1/enquiries/"+enquiry+"/respond", nil)),
		http.StatusOK)
	if out["proxy_number"] == nil {
		t.Fatalf("no proxy number came back with the response: %v", out)
	}

	// Conversion: the lease draft carries the unit, the rent and the applicant.
	start := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	out = do(t, mux, scoped(httptest.NewRequest("POST", "/v1/enquiries/"+enquiry+"/convert",
		strings.NewReader(`{"start_on": "`+start+`", "tenant_name": "Asha Nair",
			"tenant_phone": "+919876501234"}`))), http.StatusCreated)
	if out["lease_id"] == nil || out["lease_state"] != "draft" {
		t.Fatalf("conversion = %v", out)
	}

	// Off the market while the paperwork runs.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/public/listings/"+listing, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a converted listing still advertises = %d", rec.Code)
	}

	// And honestly gone from the prospect's shortlist view, not silently.
	out = do(t, mux, withToken(httptest.NewRequest("GET", "/v1/public/shortlist", nil)), http.StatusOK)
	items := out["shortlist"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["state"] != "paused" {
		t.Fatalf("shortlist after conversion = %v, want the listing shown as paused", items)
	}
}
