package ops_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The authz check on this route is against the caller's own organisation, but
// the party comes off the path and its books are resolved under the platform
// pool, which no RLS policy covers. Without an explicit mandate check, a manager
// entitled over their own firm reaches every owner in the system (#318).

func onboardAnOwner(t *testing.T, mux *http.ServeMux, name string) (party, grant string) {
	t.Helper()
	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/onboardings", map[string]any{
		"owner":    map[string]any{"name": name, "phone": uniquePhone(t)},
		"property": fullProperty("TXP", "Tax Profile Court"),
		"units":    []map[string]any{{"code": "101", "kind": "flat", "carpet_area_sqft": 900}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("onboarding: %d %s", w.Code, w.Body.String())
	}
	var owner struct {
		OwnerPartyID string `json:"owner_party_id"`
		GrantID      string `json:"grant_id"`
	}
	decode(t, w, &owner)
	return owner.OwnerPartyID, owner.GrantID
}

func aResidentProfile() map[string]any {
	return map[string]any{
		"residency": "resident", "residence_country": "IN", "pan": "ABCPD1234E",
		"source": "owner_declaration", "valid_from": "2026-04-01",
	}
}

func putWithoutAMandate(t *testing.T, mux *http.ServeMux, org tenancy.ID, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	r := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(tenancy.With(r.Context(), org)))
	return w
}

func TestAFirmRecordsTheTaxProfileOfAnOwnerItActsFor(t *testing.T) {
	mux := serveOnboarding(t)
	party, grant := onboardAnOwner(t, mux, "Anjali Menon")

	w := putUnderGrant(t, mux, isolationtest.OrgFirm, grant,
		"/v1/ops/parties/"+party+"/tax-profile", aResidentProfile())
	if w.Code != http.StatusOK {
		t.Fatalf("recording the profile: %d %s", w.Code, w.Body.String())
	}

	var got struct {
		PANFurnished      bool   `json:"pan_furnished"`
		Residency         string `json:"residency"`
		Rule37BCFurnished bool   `json:"rule_37bc_furnished"`
	}
	decode(t, w, &got)
	if !got.PANFurnished || got.Residency != "resident" {
		t.Errorf("read back as %+v", got)
	}
	if got.Rule37BCFurnished {
		t.Error("a resident was marked as having furnished rule 37BC's particulars")
	}
	if body := w.Body.String(); bytes.Contains([]byte(body), []byte("ABCPD1234E")) {
		t.Error("the PAN came back whole")
	}
}

func TestAnotherFirmCannotRecordThatOwnersTaxProfile(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Vikram Rao")

	w := putWithoutAMandate(t, mux, isolationtest.OrgOwner, "/v1/ops/parties/"+party+"/tax-profile",
		map[string]any{
			"residency": "non_resident", "residence_country": "AE",
			"source": "owner_declaration", "valid_from": "2026-04-01",
		})
	if w.Code != http.StatusNotFound {
		t.Fatalf("an organisation with no mandate over this owner got %d %s — it can set the "+
			"rate somebody else's rent is deducted at", w.Code, w.Body.String())
	}
}

func TestAnotherFirmCannotReadThatOwnersTaxProfile(t *testing.T) {
	mux := serveOnboarding(t)
	party, grant := onboardAnOwner(t, mux, "Latha Iyer")

	if w := putUnderGrant(t, mux, isolationtest.OrgFirm, grant,
		"/v1/ops/parties/"+party+"/tax-profile", aResidentProfile()); w.Code != http.StatusOK {
		t.Fatalf("recording the profile: %d %s", w.Code, w.Body.String())
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/ops/parties/"+party+"/tax-profile", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r.WithContext(tenancy.With(r.Context(), isolationtest.OrgOwner)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("an organisation with no mandate read it: %d %s", w.Code, w.Body.String())
	}
}

// The switcher names the owner but the tax profile is written against the
// party, so a firm returning to an owner it onboarded weeks ago has no way to
// reach the profile without it (#318).
func TestTheSwitcherNamesThePartyTheTaxProfileIsWrittenAgainst(t *testing.T) {
	mux := serveOnboarding(t)
	party, grant := onboardAnOwner(t, mux, "Nandini Prasad")

	w := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/portfolios")
	if w.Code != http.StatusOK {
		t.Fatalf("GET portfolios: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Portfolios []struct {
			OwnerPartyID string `json:"owner_party_id"`
		} `json:"portfolios"`
	}
	decode(t, w, &list)

	var found bool
	for _, p := range list.Portfolios {
		if p.OwnerPartyID == party {
			found = true
		}
	}
	if !found {
		t.Fatalf("the onboarded owner's party %s is on no portfolio in the switcher", party)
	}

	// And it is the party the route accepts, not merely a well-formed id.
	if w := putUnderGrant(t, mux, isolationtest.OrgFirm, grant,
		"/v1/ops/parties/"+party+"/tax-profile", aResidentProfile()); w.Code != http.StatusOK {
		t.Fatalf("the party the switcher named was refused: %d %s", w.Code, w.Body.String())
	}
}
