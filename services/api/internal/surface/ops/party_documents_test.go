package ops_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The owner's own paperwork (#318). An owner abroad self-attests a copy, and
// what the endpoint keeps is the mask and the attestation — never the number.

// The path is the one upload-url would have minted, under the owner's own
// prefix — the surface refuses any other, so a firm cannot claim an object it
// was never given.
func ownerOrgOf(t *testing.T, mux *http.ServeMux, party string) string {
	t.Helper()
	w := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/portfolios")
	if w.Code != http.StatusOK {
		t.Fatalf("GET portfolios: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Portfolios []struct {
			OwnerPartyID string `json:"owner_party_id"`
			OwnerOrgID   string `json:"owner_org_id"`
		} `json:"portfolios"`
	}
	decode(t, w, &list)
	for _, p := range list.Portfolios {
		if p.OwnerPartyID == party {
			return p.OwnerOrgID
		}
	}
	t.Fatalf("the switcher does not name party %s", party)
	return ""
}

// The copy is filed by somebody, and who is part of what it is worth — the
// surface refuses a session with no principal behind it.
func file(t *testing.T, mux *http.ServeMux, org tenancy.ID, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx := auth.With(tenancy.With(r.Context(), org), auth.Principal{Email: "manager@example.test"})
	mux.ServeHTTP(w, r.WithContext(ctx))
	return w
}

func aSelfAttestedPassport(org string) map[string]any {
	return map[string]any{
		"kind": "passport", "object_path": "org/" + org + "/documents/" + uuid.NewString() + ".pdf",
		"filename": "passport.pdf", "content_type": "application/pdf",
		"issuing_country": "IN", "number": "L898902C3",
		"issued_on": "2019-05-02", "expires_on": "2029-05-01",
		"attestation": "self", "attested_on": "2026-08-01",
	}
}

func TestAnOwnersSelfAttestedCopyIsFiledAndReadBackMasked(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	w := file(t, mux, isolationtest.OrgFirm, "/v1/ops/parties/"+party+"/documents", aSelfAttestedPassport(ownerOrgOf(t, mux, party)))
	if w.Code != http.StatusCreated {
		t.Fatalf("filing the copy: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/parties/"+party+"/documents")
	if w.Code != http.StatusOK {
		t.Fatalf("reading them back: %d %s", w.Code, w.Body.String())
	}
	var list struct {
		Documents []struct {
			Kind         string `json:"kind"`
			NumberMasked string `json:"number_masked"`
			Attestation  string `json:"attestation"`
			AttestedOn   string `json:"attested_on"`
			ExpiresOn    string `json:"expires_on"`
		} `json:"documents"`
	}
	decode(t, w, &list)
	if len(list.Documents) != 1 {
		t.Fatalf("held %d documents", len(list.Documents))
	}
	d := list.Documents[0]
	if d.Kind != "passport" || d.Attestation != "self" || d.AttestedOn != "2026-08-01" {
		t.Errorf("the copy came back as %+v", d)
	}
	if d.NumberMasked != "XXXXX89C3" && d.NumberMasked[len(d.NumberMasked)-4:] != "02C3" {
		t.Errorf("the mask reads %q", d.NumberMasked)
	}
	if body := w.Body.String(); strings.Contains(body, "L898902C3") {
		t.Error("the whole passport number came back out of the endpoint")
	}
}

// ADR-0013 lives at this boundary: the number arrives whole because it was just
// read off a document, and nothing downstream may ever see it that way.
func TestTheWholeNumberNeverReachesTheStore(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	doc := aSelfAttestedPassport(ownerOrgOf(t, mux, party))
	doc["number"] = "L898902C3"
	if w := file(t, mux, isolationtest.OrgFirm, "/v1/ops/parties/"+party+"/documents", doc); w.Code != http.StatusCreated {
		t.Fatalf("filing the copy: %d %s", w.Code, w.Body.String())
	}

	w := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/parties/"+party+"/documents")
	if strings.Contains(w.Body.String(), "L898902C3") {
		t.Error("the number was stored whole and came back")
	}
}

func TestAnEndorsementMustNameWhoMadeIt(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	doc := aSelfAttestedPassport(ownerOrgOf(t, mux, party))
	doc["attestation"] = "indian_mission"
	w := file(t, mux, isolationtest.OrgFirm, "/v1/ops/parties/"+party+"/documents", doc)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an unnamed mission endorsement was accepted: %d %s", w.Code, w.Body.String())
	}
}

// A copy nobody filed is a copy nobody can be asked about later.
func TestACopyFiledByNobodyIsRefused(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/parties/"+party+"/documents",
		aSelfAttestedPassport(ownerOrgOf(t, mux, party)))
	if w.Code != http.StatusForbidden {
		t.Fatalf("a copy was filed with nobody behind it: %d %s", w.Code, w.Body.String())
	}
}

// The path is the one upload-url minted. Any other is a claim on an object the
// firm was never given, and the download URL after it would honour the claim.
func TestAPathFromAnotherOrganisationIsRefused(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	doc := aSelfAttestedPassport(ownerOrgOf(t, mux, party))
	doc["object_path"] = "org/" + isolationtest.OrgOutsider.String() + "/documents/" + uuid.NewString() + ".pdf"
	w := file(t, mux, isolationtest.OrgFirm, "/v1/ops/parties/"+party+"/documents", doc)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("another organisation's object was claimed: %d %s", w.Code, w.Body.String())
	}
}

func TestADocumentKindNobodyAsksForIsRefused(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	doc := aSelfAttestedPassport(ownerOrgOf(t, mux, party))
	doc["kind"] = "selfie"
	w := file(t, mux, isolationtest.OrgFirm, "/v1/ops/parties/"+party+"/documents", doc)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown kind was accepted: %d %s", w.Code, w.Body.String())
	}
}

// Same hole as the tax profile's: the authz check is against the caller's own
// organisation, and the party comes off the path.
func TestAFirmWithNoMandateCannotReadAnOwnersDocuments(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	if w := file(t, mux, isolationtest.OrgFirm, "/v1/ops/parties/"+party+"/documents", aSelfAttestedPassport(ownerOrgOf(t, mux, party))); w.Code != http.StatusCreated {
		t.Fatalf("filing the copy: %d %s", w.Code, w.Body.String())
	}

	w := call(t, mux, isolationtest.OrgOutsider, http.MethodGet, "/v1/ops/parties/"+party+"/documents")
	if w.Code != http.StatusNotFound {
		t.Fatalf("a firm with no mandate read the owner's paperwork: %d %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "passport") {
		t.Error("the refusal quoted what was held")
	}
}

func TestAFirmWithNoMandateCannotFileAgainstAnOwner(t *testing.T) {
	mux := serveOnboarding(t)
	party, _ := onboardAnOwner(t, mux, "Anjali Menon")

	w := file(t, mux, isolationtest.OrgOutsider, "/v1/ops/parties/"+party+"/documents", aSelfAttestedPassport(ownerOrgOf(t, mux, party)))
	if w.Code != http.StatusNotFound {
		t.Fatalf("a firm with no mandate filed against the owner: %d %s", w.Code, w.Body.String())
	}
}
