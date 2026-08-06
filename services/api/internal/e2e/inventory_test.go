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

	"github.com/jackc/pgx/v5/pgxpool"
	discoveryhttp "github.com/tesserix/dwellm8/services/api/internal/discovery/http"
	discoveryservice "github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	discoverystore "github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	propertyhttp "github.com/tesserix/dwellm8/services/api/internal/property/http"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
	propertystore "github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// The very start of the funnel, through HTTP: an owner registers a building
// that did not exist, adds a flat to it, advertises it, and a stranger finds
// it in search — issue #32 joined to ADR-0019, no seeded inventory anywhere.

func inventoryHarness(t *testing.T) *http.ServeMux {
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
	// The organisations must exist for the tenant FK; nothing else is seeded.
	isolationtest.SeedPropertyTree(t, tenancy.NewPlatformPool(platPool))

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	today := func() effective.Date { n := time.Now(); return effective.Day(n.Year(), n.Month(), n.Day()) }
	leases := leaseservice.NewLeases(leasestore.New(req), log)
	listingsStore := discoverystore.NewListings(req)

	mux := http.NewServeMux()
	propertyhttp.New(propertyservice.New(propertystore.New(req)), log).
		Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	discoveryhttp.NewListings(
		discoveryservice.NewListings(listingsStore, discoverystore.NewPublic(req), today, log).
			WithOccupancy(leases),
		discoveryservice.NewEnquiries(discoverystore.NewEnquiries(req, tenancy.NewPlatformPool(platPool)),
			listingsStore, discoverystore.NewProspects(tenancy.NewPlatformPool(platPool)), log),
		log).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	discoveryhttp.NewPublic(
		discoveryservice.NewListings(listingsStore, discoverystore.NewPublic(req), today, log),
		discoveryservice.NewProspects(discoverystore.NewProspects(tenancy.NewPlatformPool(platPool)), nil, log),
		discoveryservice.NewEnquiries(discoverystore.NewEnquiries(req, tenancy.NewPlatformPool(platPool)),
			listingsStore, discoverystore.NewProspects(tenancy.NewPlatformPool(platPool)), log),
		log).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux
}

func post(t *testing.T, mux *http.ServeMux, path, body string, want int) map[string]any {
	t.Helper()
	r := httptest.NewRequest("POST", path, strings.NewReader(body)).
		WithContext(tenancy.With(context.Background(), isolationtest.OrgOwner))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, r)
	if rec.Code != want {
		t.Fatalf("POST %s = %d, want %d — %s", path, rec.Code, want, rec.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestRegisterAdvertiseFind(t *testing.T) {
	mux := inventoryHarness(t)
	stamp := time.Now().UnixNano()
	city := fmt.Sprintf("Newbuild-%d", stamp)

	// Register the building. The refusals first: a bad PIN and a bad kind are
	// named back, never defaulted (#32's guardrails).
	post(t, mux, "/v1/properties", `{"code": "NB-`+fmt.Sprint(stamp)+`",
		"name": "Newbuild Heights", "kind": "tower",
		"address_line1": "1 New Road", "locality": "Kadubeesanahalli",
		"city": "`+city+`", "state_code": "KA", "pin": "560103"}`, http.StatusUnprocessableEntity)
	prop := post(t, mux, "/v1/properties", `{"code": "NB-`+fmt.Sprint(stamp)+`",
		"name": "Newbuild Heights", "kind": "building",
		"address_line1": "1 New Road", "locality": "Kadubeesanahalli",
		"city": "`+city+`", "state_code": "KA", "pin": "560103"}`, http.StatusCreated)
	propertyID := prop["id"].(string)

	// The same code twice is a 409 naming the collision.
	post(t, mux, "/v1/properties", `{"code": "NB-`+fmt.Sprint(stamp)+`",
		"name": "Duplicate", "kind": "building", "address_line1": "2 New Road",
		"locality": "X", "city": "`+city+`", "state_code": "KA", "pin": "560103"}`,
		http.StatusConflict)

	// A flat, and its parking attached to it.
	unit := post(t, mux, "/v1/properties/"+propertyID+"/units",
		`{"code": "1204", "unit_kind": "flat", "floor": 12, "carpet_area_sqft": 1180}`,
		http.StatusCreated)
	unitID := unit["id"].(string)
	post(t, mux, "/v1/properties/"+propertyID+"/units",
		`{"code": "P-31", "unit_kind": "parking", "parent_unit_id": "`+unitID+`"}`,
		http.StatusCreated)
	// A duplicate unit code in the same building is refused by name, and a
	// lettable unit with no area never gets that far (units_lettable_has_area).
	post(t, mux, "/v1/properties/"+propertyID+"/units",
		`{"code": "1205", "unit_kind": "flat"}`, http.StatusUnprocessableEntity)
	post(t, mux, "/v1/properties/"+propertyID+"/units",
		`{"code": "1204", "unit_kind": "flat", "carpet_area_sqft": 900}`, http.StatusConflict)

	// Advertise it and publish.
	listing := post(t, mux, "/v1/listings", `{"property_id": "`+propertyID+`",
		"unit_id": "`+unitID+`", "headline": "Brand new 2BHK on the 12th floor",
		"locality": "Kadubeesanahalli", "city": "`+city+`", "state_code": "KA",
		"rent_minor": 3800000, "deposit_minor": 11400000, "maintenance_minor": 400000,
		"costs_confirmed": true, "bedrooms": 2}`, http.StatusCreated)
	post(t, mux, "/v1/listings/"+listing["id"].(string)+"/publish", `{}`, http.StatusOK)

	// A stranger finds it — no session, no seeds, a building that did not
	// exist at the top of this test.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/v1/public/listings?city="+city, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("public search = %d — %s", rec.Code, rec.Body)
	}
	var out struct {
		Listings []struct {
			Headline          string `json:"headline"`
			TotalMonthlyMinor int64  `json:"total_monthly_minor"`
		} `json:"listings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || len(out.Listings) != 1 {
		t.Fatalf("search found %d listings (%v) — %s", len(out.Listings), err, rec.Body)
	}
	if out.Listings[0].TotalMonthlyMinor != 4200000 {
		t.Fatalf("total = %d, want 4200000 (rent+maintenance)", out.Listings[0].TotalMonthlyMinor)
	}
}
