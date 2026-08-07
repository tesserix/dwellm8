package ops_test

import (
	"context"
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
	statutorystore "github.com/tesserix/dwellm8/services/api/internal/platform/statutory/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	tdsstore "github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
	propertystore "github.com/tesserix/dwellm8/services/api/internal/property/store"
	"github.com/tesserix/dwellm8/services/api/internal/surface/ops"
)

// What the rent is short by, and why (#318). The decision matrix has had no
// production caller since ADR-0024 landed: the section was chosen when the lease
// was created and never read again, so nothing resolved a rate. This is that
// caller, and it composes the three modules the answer needs — the tenancy's
// facts, the payee's own profile, and the registry the numbers live in.

func serveDeduction(t *testing.T) *http.ServeMux {
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

	table, err := statutorystore.New(pool).Table(ctx)
	if err != nil {
		t.Fatalf("loading the statutory registry: %v", err)
	}
	matrix, err := tds.New(table)
	if err != nil {
		t.Fatalf("binding the matrix: %v", err)
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	platPool := tenancy.NewPlatformPool(plat)
	principals := identitystore.New(platPool)

	mux := http.NewServeMux()
	h := ops.New(
		propertyservice.New(propertystore.New(pool)),
		leaseservice.NewLeases(leasestore.New(pool), log),
		moneyservice.NewStatements(moneystore.NewLedger(pool), moneystore.NewPayments(pool), nil),
		identityservice.NewResidents(principals, log), nil, log, nil,
	).WithOwners(identityservice.NewOwners(principals, log)).
		WithTDS(matrix, tdsstore.New(pool))
	registrar := authz.NewRegistrar(mux, &authz.Guard{})
	h.OnboardingRoutes(registrar)
	h.TaxProfileRoutes(registrar)
	h.DeductionRoutes(registrar)
	return mux
}

// A landlord with a tenancy, onboarded in one call: the deductor class and the
// residency are what select the section, so every case below differs only in
// those and in what the owner has furnished.
func aTenancy(t *testing.T, mux *http.ServeMux, class, residency string, rentMinor int64) (party, grant, lease string) {
	t.Helper()
	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": "Deduction Owner", "phone": uniquePhone(t)},
		"property": fullProperty(fmt.Sprintf("DED%05d", rand.IntN(100000)), "Deduction House"),
		"units":    []map[string]any{{"code": "101", "kind": "flat", "carpet_area_sqft": 900}},
		"tenancy": map[string]any{
			"unit_code": "101",
			"tenant":    map[string]any{"name": "Kavya Iyer", "phone": uniquePhone(t)},
			"start_on":  "2026-04-01", "end_on": "2027-03-31",
			"rent_amount_minor": rentMinor, "deposit_amount_minor": rentMinor * 3, "due_day": 5,
			"deductor_class": class, "landlord_residency": residency,
		},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("onboarding a tenancy: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		OwnerPartyID string `json:"owner_party_id"`
		GrantID      string `json:"grant_id"`
		LeaseID      string `json:"lease_id"`
		LeaseState   string `json:"lease_state"`
		LeaseNote    string `json:"lease_note"`
	}
	decode(t, w, &out)
	if out.LeaseState != "active" {
		t.Fatalf("the tenancy did not go live: %s — %s", out.LeaseState, out.LeaseNote)
	}
	return out.OwnerPartyID, out.GrantID, out.LeaseID
}

func furnish(t *testing.T, mux *http.ServeMux, grant, party string, profile map[string]any) {
	t.Helper()
	w := putUnderGrant(t, mux, isolationtest.OrgFirm, grant,
		"/v1/ops/parties/"+party+"/tax-profile", profile)
	if w.Code != http.StatusOK {
		t.Fatalf("recording the tax profile: %d %s", w.Code, w.Body.String())
	}
}

func getUnderGrant(t *testing.T, mux *http.ServeMux, org tenancy.ID, grant, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, path, nil)
	ctx := tenancy.WithGrant(tenancy.With(r.Context(), org), tenancy.GrantID(grant))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(ctx))
	return w
}

type deductionBody struct {
	Section          string `json:"section"`
	Determined       bool   `json:"rate_determined"`
	RateBps          int    `json:"rate_bps"`
	Crossed          bool   `json:"deductible"`
	MonthlyRentMinor int64  `json:"monthly_rent_minor"`
	AnnualRentMinor  int64  `json:"annual_rent_minor"`
	WithheldMinor    int64  `json:"withheld_minor"`
	ThresholdMinor   int64  `json:"threshold_minor"`
	Because          string `json:"because"`
	Payee            struct {
		PartyID      string `json:"party_id"`
		PANFurnished bool   `json:"pan_furnished"`
		Form         string `json:"form"`
	} `json:"payee"`
}

// ₹75,000 a month from a business tenant to a resident landlord: section 194-I,
// ten per cent, and the year is over the threshold the proviso sets.
func TestABusinessTenantDeductsAtSection194IOnAResidentLandlord(t *testing.T) {
	mux := serveDeduction(t)
	party, grant, lease := aTenancy(t, mux, "business", "resident", 75_000_00)
	furnish(t, mux, grant, party, map[string]any{
		"residency": "resident", "residence_country": "IN", "pan": "ABCPD1234E",
		"source": "owner_declaration", "valid_from": "2026-04-01",
	})

	w := getUnderGrant(t, mux, isolationtest.OrgFirm, grant,
		"/v1/ops/tenancies/"+lease+"/deduction?on=2026-08-06")
	if w.Code != http.StatusOK {
		t.Fatalf("asking what to deduct: %d %s", w.Code, w.Body.String())
	}
	var got deductionBody
	decode(t, w, &got)
	switch {
	case got.Section != "194i":
		t.Errorf("a business tenant and a resident landlord were put on section %q", got.Section)
	case !got.Determined || got.RateBps != 1000:
		t.Errorf("deducted at %d basis points, want 1000", got.RateBps)
	case !got.Crossed:
		t.Errorf("₹9,00,000 a year did not cross the threshold of %d paise", got.ThresholdMinor)
	case got.WithheldMinor != 7_500_00:
		t.Errorf("withheld %d paise on ₹75,000, want 750000 — the rate is applied once, here",
			got.WithheldMinor)
	case got.Payee.PartyID != party || !got.Payee.PANFurnished:
		t.Errorf("the answer names payee %+v", got.Payee)
	}
}

// The landlord nobody has asked. §206AA's floor is twenty per cent, and the
// unanswered profile deducts more rather than less — the safe direction.
func TestALandlordWhoHasFurnishedNoPANIsDeductedAtSection206AAsFloor(t *testing.T) {
	mux := serveDeduction(t)
	party, grant, lease := aTenancy(t, mux, "business", "resident", 75_000_00)
	furnish(t, mux, grant, party, map[string]any{
		"residency": "resident", "residence_country": "IN",
		"source": "owner_declaration", "valid_from": "2026-04-01",
	})

	w := getUnderGrant(t, mux, isolationtest.OrgFirm, grant,
		"/v1/ops/tenancies/"+lease+"/deduction?on=2026-08-06")
	if w.Code != http.StatusOK {
		t.Fatalf("asking what to deduct: %d %s", w.Code, w.Body.String())
	}
	var got deductionBody
	decode(t, w, &got)
	if got.RateBps != 2000 || got.WithheldMinor != 15_000_00 {
		t.Errorf("a landlord with no PAN was deducted at %d basis points, %d paise — section "+
			"206AA's floor is 20%%", got.RateBps, got.WithheldMinor)
	}
	if got.Payee.PANFurnished {
		t.Error("the answer says a PAN was furnished when none was")
	}
}

// Section 195 has no rate row on purpose: the rate is the Act's or a treaty's,
// read with the landlord's residency certificate. The honest answer is that no
// rate is determined — not a plausible number, and not a 500.
func TestANonResidentLandlordIsToldNoRateIsDeterminedRatherThanGivenOne(t *testing.T) {
	mux := serveDeduction(t)
	party, grant, lease := aTenancy(t, mux, "business", "non_resident", 75_000_00)
	furnish(t, mux, grant, party, map[string]any{
		"residency": "non_resident", "residence_country": "AE", "pan": "ABCPD1234E",
		"payee_form": "company", "source": "owner_declaration", "valid_from": "2026-04-01",
	})

	w := getUnderGrant(t, mux, isolationtest.OrgFirm, grant,
		"/v1/ops/tenancies/"+lease+"/deduction?on=2026-08-06")
	if w.Code != http.StatusOK {
		t.Fatalf("asking what to deduct on a section 195 tenancy: %d %s", w.Code, w.Body.String())
	}
	var got deductionBody
	decode(t, w, &got)
	switch {
	case got.Section != "195":
		t.Errorf("a non-resident landlord was put on section %q", got.Section)
	case got.Determined || got.RateBps != 0 || got.WithheldMinor != 0:
		t.Errorf("a rate was invented for section 195: %d basis points, %d paise",
			got.RateBps, got.WithheldMinor)
	case got.Because == "":
		t.Error("nothing was said about why no rate came back")
	case got.Payee.Form != "company":
		t.Errorf("the payee's legal form read back as %q — it picks the surcharge ladder",
			got.Payee.Form)
	}
}

// A tenancy in books this firm holds no mandate over. 404 rather than 403: that
// a lease exists at all is not something an outsider is entitled to learn.
func TestATenancyOutsideTheFirmsMandateHasNoDeductionToRead(t *testing.T) {
	mux := serveDeduction(t)
	_, _, lease := aTenancy(t, mux, "business", "resident", 75_000_00)

	w := call(t, mux, isolationtest.OrgOutsider, http.MethodGet,
		"/v1/ops/tenancies/"+lease+"/deduction?on=2026-08-06")
	if w.Code != http.StatusNotFound {
		t.Fatalf("an outsider read a deduction on somebody else's tenancy: %d %s",
			w.Code, w.Body.String())
	}
}
