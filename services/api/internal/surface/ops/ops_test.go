package ops_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	discoveryservice "github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	discoverystore "github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	moneystore "github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/surface/ops"

	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
	propertystore "github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// The management firm's view, against a real database. Reuses the resident-
// scope fixture (isolationtest.SeedResidentFixtures) rather than its own: it
// already gives OrgOwner one property and two live leases with a raised,
// unpaid invoice each — exactly the roster this surface reads, and one
// fixture asserted from two audiences is one less way for them to drift
// apart.

func serve(t *testing.T) *http.ServeMux {
	mux, _ := serveWithPool(t)
	return mux
}

func serveWithPool(t *testing.T) (*http.ServeMux, tenancy.PlatformPool) {
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
	seedOwnership(t, tenancy.NewPlatformPool(plat))

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	payments := moneystore.NewPayments(pool)
	residents := identityservice.NewResidents(identitystore.New(tenancy.NewPlatformPool(plat)), log)

	mux := http.NewServeMux()
	listings := discoveryservice.NewListings(discoverystore.NewListings(pool),
		discoverystore.NewPublic(pool), func() effective.Date { return effective.DateOf(time.Now(), time.UTC) }, log)
	ops.New(
		propertyservice.New(propertystore.New(pool)),
		leaseservice.NewLeases(leasestore.New(pool), log),
		moneyservice.NewStatements(moneystore.NewLedger(pool), payments, nil),
		residents, nil, log, nil,
	).WithListings(listings).Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux, tenancy.NewPlatformPool(plat)
}

// seedFutureTenancy puts a unit of its own off the market from a date still
// ahead: the agreement is signed, the term has not begun. Fresh ids per run,
// because this suite commits and the fixture's own units are asserted on
// elsewhere.
func seedFutureTenancy(t *testing.T, plat tenancy.PlatformPool, from, to string) string {
	t.Helper()
	tok := token(t)
	unitID, lease, party := uuidFrom(tok, "5"), uuidFrom(tok, "6"), uuidFrom(tok, "7")
	code := "L" + tok[:5]
	err := tenancy.Platform(context.Background(), plat, "seeding a tenancy that has not started",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft)
				VALUES ($1, $2, $3, 'flat', $4, 1, 610.00)`,
				unitID, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				code); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
				VALUES ($1, $2, $3, $4, 'active', $5::date, $6::date)`,
				lease, isolationtest.OrgOwner.String(), isolationtest.PropertyGranted,
				unitID, from, to); err != nil {
				return err
			}
			if err := isolationtest.SeedLeaseTaxFacts(ctx, tx, isolationtest.OrgOwner.String(),
				lease, from); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `
				INSERT INTO lease_parties (tenant_id, lease_id, party_id, role, valid_from)
				VALUES ($1, $2, $3, 'tenant', $4::date)`,
				isolationtest.OrgOwner.String(), lease, party, from)
			return err
		})
	if err != nil {
		t.Fatalf("seeding a tenancy that has not started: %v", err)
	}
	return code
}

// seedOwnership records who owns the resident fixture's property. The
// resident-scope fixture never needs this — a renter's own view has no
// reason to name their landlord's owner — but PartiesOf does: a lease with
// no property_ownership row refuses rather than crediting rent to nobody
// (internal/lease/store/leases.go's Parties), which is the correct behaviour
// for a billing run and the reason this surface's test supplies the row a
// tenant-only fixture omits.
func seedOwnership(t *testing.T, plat tenancy.PlatformPool) {
	t.Helper()
	const ownerParty = "d3333333-0000-0000-0000-000000000001"
	err := tenancy.Platform(context.Background(), plat, "seeding the fixture property's ownership",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO property_ownership (tenant_id, property_id, owner_party_id, valid_from)
				VALUES ($1, $2, $3, date '2026-01-01')
				ON CONFLICT DO NOTHING`,
				isolationtest.OrgOwner.String(), isolationtest.PropertyGranted, ownerParty)
			return err
		})
	if err != nil {
		t.Fatalf("seeding ownership: %v", err)
	}
}

func call(t *testing.T, mux *http.ServeMux, org tenancy.ID, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(tenancy.With(r.Context(), org)))
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(w.Body).Decode(v); err != nil {
		t.Fatalf("decoding %s: %v", w.Body.String(), err)
	}
}

// A manager sees the firm's own portfolio and nobody else's — the same RLS
// read the owner surface's Properties uses, exercised as the firm's session
// rather than an owner's.
func TestOpsSeesItsOwnPortfolioOnly(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/properties")
	if w.Code != http.StatusOK {
		t.Fatalf("GET properties: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Properties []struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		} `json:"properties"`
	}
	decode(t, w, &out)
	if len(out.Properties) == 0 {
		t.Fatal("the firm's own property from the fixture should be visible")
	}

	// The second landlord's tree is a different organisation. Nothing here
	// asks for it, and RLS is what stops the roster including it — this
	// confirms scope by asking again as the outsider and seeing a disjoint set.
	w = call(t, mux, isolationtest.OrgOutsider, http.MethodGet, "/v1/ops/properties")
	if w.Code != http.StatusOK {
		t.Fatalf("GET properties as outsider: %d %s", w.Code, w.Body.String())
	}
	var outsider struct {
		Properties []struct {
			ID string `json:"id"`
		} `json:"properties"`
	}
	decode(t, w, &outsider)
	for _, p := range out.Properties {
		for _, op := range outsider.Properties {
			if p.ID == op.ID {
				t.Fatalf("property %s visible to both organisations — RLS is not isolating the portfolio", p.ID)
			}
		}
	}
}

// The record a manager opens on site: the property, every lettable unit in
// it, and for the let ones who is in them and on what terms. The fixture's
// building has 101 and 102 tenanted, 103 empty, and two parking slots that
// are not lettable units and have no business on this list (#251).
func TestOpsPropertyRecordCarriesItsUnitsAndTheirTenancies(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertyGranted)
	if w.Code != http.StatusOK {
		t.Fatalf("GET the property: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Property struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Locality string `json:"locality"`
		} `json:"property"`
		Units []struct {
			ID        string `json:"id"`
			Code      string `json:"code"`
			Kind      string `json:"kind"`
			Occupancy string `json:"occupancy"`
			Tenant    string `json:"tenant"`
			LeaseID   string `json:"lease_id"`
			RentMinor int64  `json:"rent_amount_minor"`
			LeaseEnds string `json:"lease_ends"`
		} `json:"units"`
	}
	decode(t, w, &out)

	if out.Property.ID != isolationtest.PropertyGranted || out.Property.Name == "" {
		t.Fatalf("the property asked for is the one returned, got %+v", out.Property)
	}
	byCode := map[string]int{}
	for i, u := range out.Units {
		byCode[u.Code] = i
		if u.Kind == "parking" {
			t.Errorf("a parking slot is not a lettable unit, got %s", u.Code)
		}
	}
	for _, code := range []string{"101", "102", "103"} {
		if _, ok := byCode[code]; !ok {
			t.Fatalf("unit %s is missing from %+v", code, out.Units)
		}
	}

	let := out.Units[byCode["101"]]
	if let.Occupancy != "occupied" || let.LeaseID == "" {
		t.Errorf("101 is let in the fixture, got %+v", let)
	}
	if let.Tenant == "" {
		t.Error("a let unit names who is in it")
	}
	if let.RentMinor <= 0 {
		t.Errorf("a let unit carries the rent in force, got %d", let.RentMinor)
	}
	if let.LeaseEnds != "2026-12-31" {
		t.Errorf("the term ends when the lease says, got %q", let.LeaseEnds)
	}

	empty := out.Units[byCode["103"]]
	if empty.LeaseID != "" || empty.Tenant != "" {
		t.Errorf("103 has no tenancy in the fixture, got %+v", empty)
	}
}

// Between signature and move-in a flat is neither free nor occupied, and it is
// exactly then that a manager must not re-let it: the seeded unit is empty
// today and spoken for from next year (#304).
func TestOpsAUnitLetFromAFutureDateSaysSo(t *testing.T) {
	mux, plat := serveWithPool(t)
	code := seedFutureTenancy(t, plat, "2027-03-01", "2028-02-29")

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertyGranted)
	if w.Code != http.StatusOK {
		t.Fatalf("GET the property: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Units []struct {
			Code      string `json:"code"`
			Occupancy string `json:"occupancy"`
			LetFrom   string `json:"let_from"`
			LeaseID   string `json:"lease_id"`
		} `json:"units"`
	}
	decode(t, w, &out)

	for _, u := range out.Units {
		if u.Code != code {
			continue
		}
		if u.LetFrom != "2027-03-01" {
			t.Errorf("%s is let from 2027-03-01, got let_from %q", code, u.LetFrom)
		}
		if u.LeaseID == "" {
			t.Errorf("%s names the tenancy that has taken it", code)
		}
		return
	}
	t.Fatalf("unit %s is missing from %+v", code, out.Units)
}

// Another organisation's building is not a 403 telling them it exists.
func TestOpsCannotOpenAnotherOrganisationsProperty(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertySecond)
	if w.Code != http.StatusNotFound {
		t.Fatalf("opening the second landlord's property: %d %s, want 404", w.Code, w.Body.String())
	}
}

// The roster carries what each live tenancy owes, derived from the ledger —
// the fixture raises one invoice per lease and pays neither, so both should
// show the full rent as due.
func TestOpsArrearsReadsTheLiveRoster(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/arrears")
	if w.Code != http.StatusOK {
		t.Fatalf("GET arrears: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Arrears []struct {
			LeaseID   string `json:"lease_id"`
			RentMinor int64  `json:"rent_amount_minor"`
			DueMinor  int64  `json:"due_amount_minor"`
		} `json:"arrears"`
	}
	decode(t, w, &out)

	found := map[string]bool{isolationtest.LeasePriyaOwner: false, isolationtest.LeaseRohitOwner: false}
	for _, a := range out.Arrears {
		if _, ok := found[a.LeaseID]; !ok {
			continue
		}
		found[a.LeaseID] = true
		if a.RentMinor != 2500000 || a.DueMinor != 2500000 {
			t.Errorf("lease %s: rent %d due %d, want 2500000 and 2500000 — one unpaid invoice, "+
				"nothing more", a.LeaseID, a.RentMinor, a.DueMinor)
		}
	}
	for lease, ok := range found {
		if !ok {
			t.Errorf("lease %s from the fixture is missing from the roster", lease)
		}
	}
}

// Today aggregates exactly the roster Arrears reads: two tenancies, both in
// arrears for the full rent.
func TestOpsTodayAggregatesTheRoster(t *testing.T) {
	mux := serve(t)

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/today")
	if w.Code != http.StatusOK {
		t.Fatalf("GET today: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		ActiveTenancies   int   `json:"active_tenancies"`
		RentRollMinor     int64 `json:"rent_roll_amount_minor"`
		OutstandingMinor  int64 `json:"outstanding_amount_minor"`
		TenanciesInArrear int   `json:"tenancies_in_arrears"`
	}
	decode(t, w, &out)

	if out.ActiveTenancies < 2 {
		t.Fatalf("active_tenancies = %d, want at least the fixture's 2", out.ActiveTenancies)
	}
	if out.TenanciesInArrear < 2 {
		t.Errorf("tenancies_in_arrears = %d, want at least the fixture's 2 unpaid tenancies", out.TenanciesInArrear)
	}
	if out.RentRollMinor < 5000000 {
		t.Errorf("rent_roll_amount_minor = %d, want at least 5000000 (two tenancies at 2500000)", out.RentRollMinor)
	}
	if out.OutstandingMinor < 5000000 {
		t.Errorf("outstanding_amount_minor = %d, want at least 5000000", out.OutstandingMinor)
	}
}

// A tenancy signed for next year has no rent in force today, so counting it
// as active puts a number on the tile the money beside it contradicts (#305).
func TestOpsTodayCountsOnlyTenanciesThatHaveStarted(t *testing.T) {
	mux, plat := serveWithPool(t)

	before := today(t, mux)
	seedFutureTenancy(t, plat, "2027-03-01", "2028-02-29")
	after := today(t, mux)

	if after.ActiveTenancies != before.ActiveTenancies {
		t.Errorf("active_tenancies went %d -> %d on a tenancy that has not started",
			before.ActiveTenancies, after.ActiveTenancies)
	}
	if after.StartingTenancies != before.StartingTenancies+1 {
		t.Errorf("starting_tenancies = %d, want %d — the tile still has to say it exists",
			after.StartingTenancies, before.StartingTenancies+1)
	}
}

type todayTiles struct {
	ActiveTenancies   int   `json:"active_tenancies"`
	StartingTenancies int   `json:"starting_tenancies"`
	RentRollMinor     int64 `json:"rent_roll_amount_minor"`
	OutstandingMinor  int64 `json:"outstanding_amount_minor"`
	TenanciesInArrear int   `json:"tenancies_in_arrears"`
}

func today(t *testing.T, mux *http.ServeMux) todayTiles {
	t.Helper()
	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/today")
	if w.Code != http.StatusOK {
		t.Fatalf("GET today: %d %s", w.Code, w.Body.String())
	}
	var out todayTiles
	decode(t, w, &out)
	return out
}

// The organisation boundary that guards every other route: no tenant in
// context, no answer.
func TestOpsRefusesWithNoOrganisationInContext(t *testing.T) {
	mux := serve(t)
	r := httptest.NewRequest(http.MethodGet, "/v1/ops/properties", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("GET properties with no tenant: %d %s, want the store's ErrNoTenant surfaced as a 500",
			w.Code, w.Body.String())
	}
}
