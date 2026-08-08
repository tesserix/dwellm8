package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
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
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
	propertystore "github.com/tesserix/dwellm8/services/api/internal/property/store"
	"github.com/tesserix/dwellm8/services/api/internal/surface/ops"
)

// The manager-led onboarding, against a real database (#240). One landlord's
// books stay in one place: a second property for an owner the firm already
// acts for joins the organisation minted the first time, under the same
// mandate, and never mints a second.

func serveOnboarding(t *testing.T) *http.ServeMux {
	mux, _ := serveOnboardingWithPool(t)
	return mux
}

func serveOnboardingWithPool(t *testing.T) (*http.ServeMux, tenancy.PlatformPool) {
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
	platPool := tenancy.NewPlatformPool(plat)
	principals := identitystore.New(platPool)
	owners := identityservice.NewOwners(principals, log)

	mux := http.NewServeMux()
	h := ops.New(
		propertyservice.New(propertystore.New(pool)),
		leaseservice.NewLeases(leasestore.New(pool), log),
		moneyservice.NewStatements(moneystore.NewLedger(pool), moneystore.NewPayments(pool), nil),
		identityservice.NewResidents(principals, log), nil, log, nil,
	).WithOwners(owners)
	registrar := authz.NewRegistrar(mux, &authz.Guard{})
	h.OnboardingRoutes(registrar)
	h.PortfolioRoutes(registrar)
	h.OwnershipRoutes(registrar)
	h.TaxProfileRoutes(registrar)
	h.PartyDocumentRoutes(registrar)
	return mux, platPool
}

// A firm can hold more than one mandate over the same owner — a portfolio-wide
// one and a narrower one written later. The switcher is a list of owners, so it
// shows that owner once whatever the paperwork behind it.
func TestAnOwnerAppearsOnceHoweverManyMandates(t *testing.T) {
	mux, plat := serveOnboardingWithPool(t)

	isolationtest.SeedGrant(t, plat, isolationtest.Grant{
		Grantor: isolationtest.OrgOwner, Grantee: isolationtest.OrgFirm,
		Permissions: []string{"property.read"},
	})
	isolationtest.SeedGrant(t, plat, isolationtest.Grant{
		Grantor: isolationtest.OrgOwner, Grantee: isolationtest.OrgFirm,
		Permissions: []string{"property.read", "money.collect"},
	})

	w := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/portfolios")
	if w.Code != http.StatusOK {
		t.Fatalf("GET portfolios: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Portfolios []struct {
			OwnerOrgID string `json:"owner_org_id"`
		} `json:"portfolios"`
	}
	decode(t, w, &list)
	var seen int
	for _, p := range list.Portfolios {
		if p.OwnerOrgID == isolationtest.OrgOwner.String() {
			seen++
		}
	}
	if seen != 1 {
		t.Errorf("the owner appears %d times in the switcher, want once", seen)
	}
}

type onboarded struct {
	OwnerOrgID   string   `json:"owner_org_id"`
	OwnerPartyID string   `json:"owner_party_id"`
	GrantID      string   `json:"grant_id"`
	CreatedOrg   bool     `json:"created_organisation"`
	PropertyID   string   `json:"property_id"`
	UnitIDs      []string `json:"unit_ids"`
}

func post(t *testing.T, mux *http.ServeMux, org tenancy.ID, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(tenancy.With(r.Context(), org)))
	return w
}

// A new owner arrives with their first property: an organisation of their
// own, the firm's mandate over it, and the units in their books.
func TestOnboardingANewOwnerMintsTheirBooks(t *testing.T) {
	mux := serveOnboarding(t)
	phone := uniquePhone(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": "Meera Sharma", "phone": phone},
		"property": fullProperty("BPG", "Brigade Palm Grove"),
		"units":    []map[string]any{{"code": "101", "kind": "flat", "carpet_area_sqft": 980}, {"code": "102", "kind": "flat", "carpet_area_sqft": 1120}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("onboarding: %d %s", w.Code, w.Body.String())
	}
	var out onboarded
	decode(t, w, &out)
	if !out.CreatedOrg {
		t.Error("a number nobody has used should mint the owner a new organisation")
	}
	if out.OwnerOrgID == "" || out.GrantID == "" {
		t.Fatalf("the owner needs an organisation and the firm a mandate: %+v", out)
	}
	if out.PropertyID == "" || len(out.UnitIDs) != 2 {
		t.Errorf("the property and both units should land: %+v", out)
	}
}

// The first tenancy in the same breath: the lease lives in the owner's books,
// names the tenant by the number that will claim their Live sign-in, and
// activates through ADR-0024's tax gate on the facts the manager confirmed.
func TestOnboardingActivatesTheFirstTenancy(t *testing.T) {
	mux := serveOnboarding(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": "Meera Sharma", "phone": uniquePhone(t)},
		"property": fullProperty("BPG", "Brigade Palm Grove"),
		"units":    []map[string]any{{"code": "101", "kind": "flat", "carpet_area_sqft": 980}},
		"tenancy": map[string]any{
			"unit_code": "101",
			"tenant":    map[string]any{"name": "Arjun Rao", "phone": uniquePhone(t)},
			"start_on":  "2026-08-06", "end_on": "2027-07-05",
			"rent_amount_minor": 4200000, "deposit_amount_minor": 12600000, "due_day": 5,
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("onboarding with a tenancy: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		OwnerOrgID string `json:"owner_org_id"`
		LeaseID    string `json:"lease_id"`
		LeaseState string `json:"lease_state"`
		LeaseNote  string `json:"lease_note"`
	}
	decode(t, w, &out)
	if out.LeaseID == "" {
		t.Fatalf("the tenancy should exist: %+v", out)
	}
	if out.LeaseState != "active" {
		t.Errorf("the tenancy is %q, want active — %s", out.LeaseState, out.LeaseNote)
	}
}

// A tenancy the billing run cannot attribute is a tenancy that is never
// invoiced, and it fails silently every half hour rather than at onboarding.
// Onboarding therefore records who owns the property, not just who was
// onboarded (#302).
func TestAnOnboardedTenancyCanBeBilled(t *testing.T) {
	mux := serveOnboarding(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": "Meera Sharma", "phone": uniquePhone(t)},
		"property": fullProperty("BPG", "Brigade Palm Grove"),
		"units":    []map[string]any{{"code": "101", "kind": "flat", "carpet_area_sqft": 980}},
		"tenancy": map[string]any{
			"unit_code": "101",
			"tenant":    map[string]any{"name": "Arjun Rao", "phone": uniquePhone(t)},
			"start_on":  "2026-08-06", "end_on": "2027-07-05",
			"rent_amount_minor": 4200000, "deposit_amount_minor": 12600000, "due_day": 5,
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("onboarding with a tenancy: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		OwnerOrgID   string `json:"owner_org_id"`
		OwnerPartyID string `json:"owner_party_id"`
		LeaseID      string `json:"lease_id"`
	}
	decode(t, w, &out)

	// The billing run's own question, asked the way the billing run asks it.
	pool, err := pgxpool.New(context.Background(), os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(pool.Close)
	on, err := effective.ParseDate("2026-08-06")
	if err != nil {
		t.Fatalf("start date: %v", err)
	}
	ctx := tenancy.With(context.Background(), tenancy.ID(out.OwnerOrgID))
	tenantParty, ownerParty, err := leasestore.New(pool).Parties(ctx, out.LeaseID, on)
	if err != nil {
		t.Fatalf("the onboarded tenancy cannot be billed: %v", err)
	}
	if tenantParty == "" {
		t.Error("no tenant on the onboarded tenancy")
	}
	if ownerParty != out.OwnerPartyID {
		t.Errorf("rent would be credited to %q, want the onboarded owner %q", ownerParty, out.OwnerPartyID)
	}
}

// The second property for the same owner. The manager names the owner they
// already act for rather than retyping them, and the property joins the books
// that already exist under the mandate already held.
func TestSecondPropertyJoinsTheSameOwnersBooks(t *testing.T) {
	mux := serveOnboarding(t)
	phone := uniquePhone(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": "Meera Sharma", "phone": phone},
		"property": fullProperty("BPG", "Brigade Palm Grove"),
		"units":    []map[string]any{{"code": "101", "kind": "flat", "carpet_area_sqft": 980}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("first onboarding: %d %s", w.Code, w.Body.String())
	}
	var first onboarded
	decode(t, w, &first)

	w = post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"org_id": first.OwnerOrgID},
		"property": fullProperty("SLV", "Sobha Lake View"),
		"units":    []map[string]any{{"code": "A1", "kind": "flat", "carpet_area_sqft": 750}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("second property: %d %s", w.Code, w.Body.String())
	}
	var second onboarded
	decode(t, w, &second)

	if second.CreatedOrg {
		t.Error("a second property must not mint the owner a second organisation")
	}
	if second.OwnerOrgID != first.OwnerOrgID {
		t.Errorf("the property landed in %s, want the owner's own books %s", second.OwnerOrgID, first.OwnerOrgID)
	}
	if second.GrantID != first.GrantID {
		t.Errorf("the mandate should be the one already held: got %s, want %s", second.GrantID, first.GrantID)
	}
	if second.PropertyID == first.PropertyID {
		t.Error("the second property should be a property of its own")
	}
	// The same owner, so the same party — and the rent on both properties is
	// credited to them (#302).
	if second.OwnerPartyID == "" || second.OwnerPartyID != first.OwnerPartyID {
		t.Errorf("the second property is owned by %q, want the same owner %q",
			second.OwnerPartyID, first.OwnerPartyID)
	}

	// And the switcher shows one owner holding both, not two owners holding one.
	w = call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/portfolios")
	if w.Code != http.StatusOK {
		t.Fatalf("GET portfolios: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Portfolios []struct {
			OwnerOrgID    string `json:"owner_org_id"`
			PropertyCount int    `json:"property_count"`
		} `json:"portfolios"`
	}
	decode(t, w, &list)
	var found int
	for _, p := range list.Portfolios {
		if p.OwnerOrgID == first.OwnerOrgID {
			found++
			if p.PropertyCount != 2 {
				t.Errorf("the owner holds %d properties, want 2", p.PropertyCount)
			}
		}
	}
	if found != 1 {
		t.Errorf("the owner appears %d times in the switcher, want once", found)
	}
}

// Naming an organisation the firm holds no live mandate over is refused —
// otherwise any firm could file a property into any landlord's books.
func TestPropertyRefusedForAnOwnerTheFirmDoesNotAct(t *testing.T) {
	mux := serveOnboarding(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"org_id": isolationtest.OrgOutsider.String()},
		"property": fullProperty("XXX", "Someone Else's"),
		"units":    []map[string]any{{"code": "1", "kind": "flat", "carpet_area_sqft": 600}},
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("onboarding into an unmandated portfolio: %d %s, want 403", w.Code, w.Body.String())
	}
}

// fullProperty is an address a property may actually be registered at —
// every field domain.PropertyDraft.Validate insists on.
func fullProperty(code, name string) map[string]any {
	return map[string]any{
		"code": code, "name": name, "kind": "building",
		"address_line1": "12 Palm Avenue", "locality": "Whitefield",
		"city": "Bengaluru", "state_code": "KA", "pin": "560066",
	}
}

// uniquePhone keeps each run's owner distinct — the onboarding is idempotent
// on the number, so a fixed one would join the previous run's organisation.
func uniquePhone(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("+9198%08d", rand.IntN(100000000))
}

// The solo manager onboards their own flat (#268): no second organisation, no
// mandate, and the property lands in the books they already sign in to.
func TestTheSoloManagerOnboardsTheirOwnProperty(t *testing.T) {
	mux := serveOnboarding(t)

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": "Meera Menon", "phone": uniquePhone(t), "self": true},
		"property": fullProperty(fmt.Sprintf("MMN%05d", rand.IntN(100000)), "Menon Nivas"),
		"units":    []map[string]any{{"code": "G1", "kind": "flat", "carpet_area_sqft": 860}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("onboarding my own property: %d %s", w.Code, w.Body.String())
	}
	var out onboarded
	decode(t, w, &out)
	if out.OwnerOrgID != isolationtest.OrgFirm.String() {
		t.Fatalf("my flat landed in %s, not my own books %s", out.OwnerOrgID, isolationtest.OrgFirm)
	}
	if out.GrantID != "" {
		t.Errorf("a mandate %s was minted over my own property", out.GrantID)
	}
	if out.CreatedOrg {
		t.Error("a second organisation was created for a property I own myself")
	}
	if out.PropertyID == "" || len(out.UnitIDs) != 1 {
		t.Errorf("the property and its unit should land: %+v", out)
	}

	// And it shows in the switcher as held rather than mandated.
	r := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/portfolios")
	var list struct {
		Portfolios []struct {
			OwnerOrgID  string `json:"owner_org_id"`
			GrantID     string `json:"grant_id"`
			SelfManaged bool   `json:"self_managed"`
		} `json:"portfolios"`
	}
	decode(t, r, &list)
	found := false
	for _, p := range list.Portfolios {
		if p.OwnerOrgID == isolationtest.OrgFirm.String() {
			found = true
			if !p.SelfManaged || p.GrantID != "" {
				t.Errorf("my own books read as mandated: %+v", p)
			}
		}
	}
	if !found {
		t.Errorf("the switcher = %+v; want my own books in it", list.Portfolios)
	}
}

// The second flat the solo manager owns: the wizard offers their own books in
// the list of owners, and picking them sends the organisation and no phone —
// there is nothing left to reserve. #361.
func TestTheSoloManagerOnboardsASecondPropertyTheyOwn(t *testing.T) {
	mux := serveOnboarding(t)

	mine := func(code, name string) onboarded {
		t.Helper()
		w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
			"owner":    map[string]any{"org_id": isolationtest.OrgFirm.String()},
			"property": fullProperty(code, name),
			"units":    []map[string]any{{"code": "A1", "kind": "flat", "carpet_area_sqft": 900}},
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("onboarding a property I own: %d %s", w.Code, w.Body.String())
		}
		var out onboarded
		decode(t, w, &out)
		return out
	}

	two := mine(fmt.Sprintf("MMB%05d", rand.IntN(100000)), "Menon Heights")
	if two.OwnerOrgID != isolationtest.OrgFirm.String() {
		t.Errorf("my flat landed in %s, not my own books", two.OwnerOrgID)
	}
	if two.OwnerPartyID == "" {
		t.Error("no party for the rent to be credited to")
	}
	if two.GrantID != "" {
		t.Errorf("a mandate %s was minted over my own property", two.GrantID)
	}
	if two.PropertyID == "" || len(two.UnitIDs) != 1 {
		t.Errorf("the property and its unit should land: %+v", two)
	}

	// Every flat I own is credited to the same me, however many I onboard.
	three := mine(fmt.Sprintf("MMC%05d", rand.IntN(100000)), "Menon Court")
	if three.OwnerPartyID != two.OwnerPartyID {
		t.Errorf("the third flat is credited to %s, not %s", three.OwnerPartyID, two.OwnerPartyID)
	}
}
