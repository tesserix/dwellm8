package e2e_test

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	discoveryhttp "github.com/tesserix/dwellm8/services/api/internal/discovery/http"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The manager's side of a tenant application (#258, #259), over HTTP and
// under a mandate: the firm managing the property finds the application in
// its queue, records who is applying, who else moves in, and where they have
// lived — and is told about the hole in those five years rather than having
// to spot it.

func packHarness(t *testing.T) (*http.ServeMux, tenancy.Pool, tenancy.PlatformPool, string) {
	t.Helper()
	mux, unit := harness(t)
	reqPool, platPool := pools(t)
	req, plat := reqPool, tenancy.NewPlatformPool(platPool)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	residents := identityservice.NewResidents(identitystore.New(plat), log)
	leases := leaseservice.NewLeases(leasestore.New(req), log).WithResidents(residents)
	listingsStore := store.NewListings(req)
	prospects := service.NewProspects(store.NewProspects(plat), service.DevVerifier{Log: log}, log)
	apps := store.NewApplications(req, plat)

	queue := ginx.Engine()
	discoveryhttp.NewApplications(apps, listingsStore, prospects,
		service.FromLeases{Leases: leases}, log).OpsRoutes(ginx.New(queue, &authz.Guard{}))
	for _, p := range []string{
		"GET /v1/ops/applications",
		"POST /v1/ops/applications/{id}/review",
		"POST /v1/ops/applications/{id}/accept",
		"POST /v1/ops/applications/{id}/decline",
	} {
		mux.Handle(p, queue)
	}

	public := ginx.Engine()
	discoveryhttp.NewApplications(apps, listingsStore, prospects,
		service.FromLeases{Leases: leases}, log).PublicRoutes(ginx.New(public, &authz.Guard{}))
	for _, p := range []string{
		"POST /v1/public/listings/{id}/applications",
		"GET /v1/public/applications",
		"POST /v1/public/applications/{id}/withdraw",
	} {
		mux.Handle(p, public)
	}

	packs := ginx.Engine()
	discoveryhttp.NewApplicants(store.NewApplicants(req, plat), log).
		OwnerRoutes(ginx.New(packs, &authz.Guard{}))
	for _, p := range []string{
		"GET /v1/ops/applications/{id}/profile",
		"PUT /v1/ops/applications/{id}/profile",
		"POST /v1/ops/applications/{id}/profile/submit",
		"PUT /v1/ops/applications/{id}/profile/people",
		"GET /v1/ops/applications/{id}/addresses",
		"PUT /v1/ops/applications/{id}/addresses",
	} {
		mux.Handle(p, packs)
	}
	return mux, req, plat, unit
}

func TestTheManagerCollectsTheApplicantPack(t *testing.T) {
	mux, _, plat, unit := packHarness(t)
	grant := isolationtest.SeedGrant(t, plat, isolationtest.Grant{
		Grantor:     isolationtest.OrgOwner,
		Grantee:     isolationtest.OrgFirm,
		Permissions: []string{"property.read", "property.write"},
	})
	managing := func(r *http.Request) *http.Request {
		ctx := tenancy.WithGrant(tenancy.With(r.Context(), isolationtest.OrgFirm), grant)
		return r.WithContext(ctx)
	}

	application := applyForUnit(t, mux, unit)

	// The firm sees the owner's application in its queue.
	out := do(t, mux, managing(httptest.NewRequest("GET", "/v1/ops/applications", nil)), http.StatusOK)
	queued := out["applications"].([]any)
	found := false
	for _, a := range queued {
		if a.(map[string]any)["id"] == application {
			found = true
		}
	}
	if !found {
		t.Fatalf("the firm's queue = %v; want the application it manages", queued)
	}

	// The pack, then the household, then five years that do not add up.
	do(t, mux, managing(httptest.NewRequest("PUT", "/v1/ops/applications/"+application+"/profile",
		strings.NewReader(`{"full_name":"Meera Menon","occupants":2,"tax_residency":"non_resident"}`))),
		http.StatusOK)
	do(t, mux, managing(httptest.NewRequest("PUT", "/v1/ops/applications/"+application+"/profile/people",
		strings.NewReader(`{"people":[{"role":"co_applicant","full_name":"Arun Menon","relationship":"spouse","phone":"+919847033222"}]}`))),
		http.StatusOK)

	history := do(t, mux, managing(httptest.NewRequest("PUT", "/v1/ops/applications/"+application+"/addresses",
		strings.NewReader(`{"addresses":[
			{"kind":"rented","line1":"12 MG Road","city":"Bengaluru","state_code":"KA","pin":"560038","from":"2023-07","landlord_name":"R Iyer","landlord_phone":"+919847033222"},
			{"kind":"family","line1":"7 Panampilly Nagar","city":"Kochi","state_code":"KL","pin":"682036","from":"2019-01","to":"2023-02"}]}`))),
		http.StatusOK)
	if history["complete"].(bool) {
		t.Fatalf("a four-month hole read as a complete history: %v", history)
	}
	gap := history["gaps"].([]any)[0].(map[string]any)
	if gap["from"] != "2023-03" || gap["to"] != "2023-06" {
		t.Fatalf("gap = %v; want 2023-03 to 2023-06", gap)
	}

	// The pack carries the answer the manager acts on.
	pack := do(t, mux, managing(httptest.NewRequest("GET", "/v1/ops/applications/"+application+"/profile", nil)),
		http.StatusOK)
	if pack["address_history_complete"].(bool) {
		t.Fatalf("the pack claims a complete history: %v", pack)
	}
	if len(pack["people"].([]any)) != 1 {
		t.Fatalf("household = %v; want the spouse", pack["people"])
	}

	do(t, mux, managing(httptest.NewRequest("POST", "/v1/ops/applications/"+application+"/profile/submit", nil)),
		http.StatusOK)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, managing(httptest.NewRequest("POST",
		"/v1/ops/applications/"+application+"/profile/submit", nil)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("submitting twice = %d, want 409 — %s", rec.Code, rec.Body)
	}
}

// applyForUnit publishes the unit and applies to it as a verified stranger,
// which is the only way an application exists at all.
func applyForUnit(t *testing.T, mux *http.ServeMux, unit string) string {
	t.Helper()
	city := fmt.Sprintf("Packpur-%d", time.Now().UnixNano())
	out := do(t, mux, scoped(httptest.NewRequest("POST", "/v1/listings", strings.NewReader(fmt.Sprintf(`{
		"property_id": %q, "unit_id": %q, "headline": "Quiet 2BHK near the park",
		"locality": "Indiranagar", "city": %q, "state_code": "KA", "bedrooms": 2,
		"rent_minor": 3200000, "deposit_minor": 9600000, "costs_confirmed": true}`,
		isolationtest.PropertyGranted, unit, city)))), http.StatusCreated)
	listing := out["id"].(string)
	do(t, mux, scoped(httptest.NewRequest("POST", "/v1/listings/"+listing+"/publish", nil)), http.StatusOK)

	out = do(t, mux, httptest.NewRequest("POST", "/v1/public/prospects", strings.NewReader("{}")),
		http.StatusCreated)
	token := out["token"].(string)
	phone := fmt.Sprintf("+9198%08d", time.Now().UnixNano()%100000000)
	do(t, mux, withProspect(httptest.NewRequest("POST", "/v1/public/prospects/verify",
		strings.NewReader(fmt.Sprintf(`{"phone":%q}`, phone))), token), http.StatusOK)
	do(t, mux, withProspect(httptest.NewRequest("POST", "/v1/public/prospects/confirm",
		strings.NewReader(fmt.Sprintf(`{"phone":%q,"code":"000000"}`, phone))), token), http.StatusOK)

	moveIn := time.Now().AddDate(0, 1, 0).Format("2006-01-02")
	out = do(t, mux, withProspect(httptest.NewRequest("POST",
		"/v1/public/listings/"+listing+"/applications",
		strings.NewReader(fmt.Sprintf(`{"move_in":%q,"term_months":11}`, moveIn))), token),
		http.StatusCreated)
	return out["id"].(string)
}

func withProspect(r *http.Request, token string) *http.Request {
	r.Header.Set("X-Dwellm8-Prospect", token)
	return r
}
