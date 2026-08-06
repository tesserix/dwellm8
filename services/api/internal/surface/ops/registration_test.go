package ops_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/registration"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/surface/ops"
)

// What a managing firm must hold before it may take somebody else's property
// on (#282). The rules themselves are tested in identity/domain/registration;
// what matters here is that the wire never carries a whole PAN either way.

type registrationRows struct {
	profile identitystore.ManagerProfile
	saved   identitystore.ManagerProfile
	filed   []identitystore.Registration
	docs    map[registration.Kind]registration.Held
}

func (r *registrationRows) ManagerProfile(context.Context, tenancy.ID) (identitystore.ManagerProfile, error) {
	if r.profile.LegalName == "" {
		return identitystore.ManagerProfile{}, identitystore.ErrNoProfile
	}
	return r.profile, nil
}

func (r *registrationRows) SaveManagerProfile(_ context.Context, _ tenancy.ID, p identitystore.ManagerProfile) error {
	r.saved, r.profile = p, p
	if r.profile.State == "" {
		r.profile.State = "submitted"
	}
	return nil
}

func (r *registrationRows) AddRegistration(_ context.Context, _ tenancy.ID, reg identitystore.Registration) error {
	reg.ID = "reg-1"
	r.filed = append(r.filed, reg)
	return nil
}

func (r *registrationRows) LiveRegistrations(_ context.Context, _ tenancy.ID, on time.Time) ([]identitystore.Registration, error) {
	live := make([]identitystore.Registration, 0, len(r.filed))
	for _, reg := range r.filed {
		if !on.Before(reg.ValidFrom) && on.Before(reg.ValidTo) {
			live = append(live, reg)
		}
	}
	return live, nil
}

func (r *registrationRows) RecordDocument(_ context.Context, _ tenancy.ID, d identitystore.ManagerDocument) error {
	if r.docs == nil {
		r.docs = map[registration.Kind]registration.Held{}
	}
	r.docs[registration.Kind(d.Kind)] = registration.Held{Accepted: true, ExpiresOn: d.ExpiresOn}
	return nil
}

func (r *registrationRows) HeldDocuments(context.Context, tenancy.ID) (map[registration.Kind]registration.Held, error) {
	return r.docs, nil
}

func serveRegistration(t *testing.T) (*http.ServeMux, *registrationRows) {
	t.Helper()
	rows := &registrationRows{}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mux := http.NewServeMux()
	ops.New(nil, nil, nil, nil, nil, log, nil).
		WithRegistrations(identityservice.NewRegistrations(rows)).
		RegistrationRoutes(authz.NewRegistrar(mux, &authz.Guard{}))
	return mux, rows
}

func put(t *testing.T, mux *http.ServeMux, org tenancy.ID, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	return putAs(t, mux, org, auth.Principal{}, path, body)
}

func putAs(t *testing.T, mux *http.ServeMux, org tenancy.ID, who auth.Principal,
	path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}
	r := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	ctx := auth.With(tenancy.With(r.Context(), org), who)
	mux.ServeHTTP(w, r.WithContext(ctx))
	return w
}

type registrationBody struct {
	LegalName    string `json:"legal_name"`
	Constitution string `json:"constitution"`
	PANMasked    string `json:"pan_masked"`
	State        string `json:"state"`
	Authorities  []struct {
		StateCode string `json:"state_code"`
		ValidTo   string `json:"valid_to"`
	} `json:"authorities"`
	Required []struct {
		Kind    string `json:"kind"`
		Subject string `json:"subject"`
	} `json:"required"`
	Outstanding []struct {
		Kind string `json:"kind"`
	} `json:"outstanding"`
	Fields    []string `json:"fields"`
	MayManage bool     `json:"may_manage"`
}

func TestAFirmWithNothingFiledIsToldEverythingItNeeds(t *testing.T) {
	mux, _ := serveRegistration(t)

	w := call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/registration")
	if w.Code != http.StatusOK {
		t.Fatalf("GET registration: %d %s", w.Code, w.Body.String())
	}
	var out registrationBody
	decode(t, w, &out)

	if out.State != "draft" {
		t.Errorf("a firm that has filed nothing is a draft, got %q", out.State)
	}
	if len(out.Required) == 0 || len(out.Outstanding) != len(out.Required) {
		t.Fatalf("holding nothing, every requirement is outstanding: %d of %d",
			len(out.Outstanding), len(out.Required))
	}
	if out.MayManage {
		t.Error("a firm holding no registration may not take a mandate")
	}
}

func TestOnlyTheMaskOfAPANEverComesBack(t *testing.T) {
	mux, rows := serveRegistration(t)

	w := put(t, mux, isolationtest.OrgFirm, "/v1/ops/registration", map[string]any{
		"legal_name": "Menon Estates LLP", "constitution": "llp", "pan": "ABCDE1234F",
		"registrar_id": "AAA-1234", "address_line1": "2nd Floor, Chandra Arcade",
		"city": "Kochi", "state_code": "KL", "pin_code": "682020",
		"contact_email": "pm@example.test", "contact_phone": "+919847012345",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT registration: %d %s", w.Code, w.Body.String())
	}

	if rows.saved.PANMasked != "XXXXXX234F" {
		t.Errorf("the store must receive the mask, got %q", rows.saved.PANMasked)
	}
	// The whole number must not survive anywhere on the way back out.
	if body := w.Body.String(); bytes.Contains([]byte(body), []byte("ABCDE1234F")) {
		t.Fatalf("a whole PAN reached the response:\n%s", body)
	}
}

func TestCorrespondenceFallsBackToTheSignedInAddressRatherThanBeingBlank(t *testing.T) {
	mux, rows := serveRegistration(t)

	w := putAs(t, mux, isolationtest.OrgFirm, auth.Principal{Email: "manager@example.test"},
		"/v1/ops/registration", map[string]any{
			"legal_name": "Samyak Rout", "constitution": "proprietorship",
			"address_line1": "2nd Floor, Chandra Arcade", "city": "Kochi",
			"state_code": "KL", "pin_code": "682020", "contact_phone": "+919847012345",
		})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT registration: %d %s", w.Code, w.Body.String())
	}

	// Notices under the Act are served at this address; a blank one is a firm
	// that cannot be reached (#286).
	if rows.saved.ContactEmail != "manager@example.test" {
		t.Errorf("contact email must fall back to the caller's, got %q", rows.saved.ContactEmail)
	}
}

func TestAnAddressGivenOnTheFormBeatsTheSignedInOne(t *testing.T) {
	mux, rows := serveRegistration(t)

	w := putAs(t, mux, isolationtest.OrgFirm, auth.Principal{Email: "manager@example.test"},
		"/v1/ops/registration", map[string]any{
			"legal_name": "Samyak Rout", "address_line1": "x", "city": "Kochi",
			"state_code": "KL", "pin_code": "682020",
			"contact_email": "office@example.test",
		})
	if w.Code != http.StatusOK {
		t.Fatalf("PUT registration: %d %s", w.Code, w.Body.String())
	}
	if rows.saved.ContactEmail != "office@example.test" {
		t.Errorf("the firm's own address must win, got %q", rows.saved.ContactEmail)
	}
}

func TestARefusalNamesTheOneFieldThatIsWrong(t *testing.T) {
	cases := []struct {
		name  string
		body  map[string]any
		field string
		says  string
	}{
		{"a GSTIN that is not one", map[string]any{"gstin": "ZZ99"}, "gstin", "GSTIN"},
		{"a TAN that is not one", map[string]any{"tan": "CH1S12345E"}, "tan", "TAN"},
		{"a PIN that India does not issue", map[string]any{"pin_code": "082020"}, "pin_code", "PIN"},
		{"a PAN that is not one", map[string]any{"pan": "1234567890"}, "pan", "PAN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mux, rows := serveRegistration(t)
			body := map[string]any{
				"legal_name": "Samyak Rout", "address_line1": "2nd Floor, Chandra Arcade",
				"city": "Kochi", "state_code": "KL", "pin_code": "682020",
				"contact_phone": "+919847012345",
			}
			for k, v := range c.body {
				body[k] = v
			}

			w := put(t, mux, isolationtest.OrgFirm, "/v1/ops/registration", body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("must be refused, got %d %s", w.Code, w.Body.String())
			}
			var out struct {
				Error string `json:"error"`
				Field string `json:"field"`
			}
			decode(t, w, &out)
			if out.Field != c.field {
				t.Errorf("the refusal must name the field, got %q want %q", out.Field, c.field)
			}
			if !strings.Contains(out.Error, c.says) {
				t.Errorf("the message must be about %s, got %q", c.says, out.Error)
			}
			if rows.saved.LegalName != "" {
				t.Error("nothing may be saved when a field is refused")
			}
		})
	}
}

func TestAPANThatIsNotAPANIsRefusedRatherThanStored(t *testing.T) {
	mux, rows := serveRegistration(t)

	w := put(t, mux, isolationtest.OrgFirm, "/v1/ops/registration", map[string]any{
		"legal_name": "Menon Estates LLP", "constitution": "llp", "pan": "1234567890",
		"address_line1": "x", "city": "Kochi", "state_code": "KL", "pin_code": "682020",
		"contact_email": "pm@example.test", "contact_phone": "+919847012345",
	})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a malformed PAN must be refused, got %d %s", w.Code, w.Body.String())
	}
	if rows.saved.LegalName != "" {
		t.Error("nothing may be saved when the PAN is refused")
	}
}

func TestALLPIsAskedForTheEntitysOwnPANAndAProprietorForTheirs(t *testing.T) {
	mux, rows := serveRegistration(t)
	rows.profile = identitystore.ManagerProfile{LegalName: "Menon Estates LLP", Constitution: "llp", State: "submitted"}

	var llp registrationBody
	decode(t, call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/registration"), &llp)

	subject := map[string]string{}
	for _, r := range llp.Required {
		subject[r.Kind] = r.Subject
	}
	if subject["pan_card"] != "firm" {
		t.Errorf("an LLP files the entity's PAN, got %q", subject["pan_card"])
	}
	if _, ok := subject["llp_agreement"]; !ok {
		t.Error("an LLP must produce its agreement")
	}
	if !contains(llp.Fields, "registrar_id") {
		t.Error("an LLP must give its LLPIN")
	}

	rows.profile = identitystore.ManagerProfile{LegalName: "Meera Menon", Constitution: "proprietorship", State: "submitted"}
	var sole registrationBody
	decode(t, call(t, mux, isolationtest.OrgFirm, http.MethodGet, "/v1/ops/registration"), &sole)

	subject = map[string]string{}
	for _, r := range sole.Required {
		subject[r.Kind] = r.Subject
	}
	if subject["pan_card"] != "person" {
		t.Errorf("a proprietor files their own PAN, got %q", subject["pan_card"])
	}
	if contains(sole.Fields, "registrar_id") {
		t.Error("a proprietor has no registrar to be registered with")
	}
}

func TestAnAgentRegistrationNeedsARealValidityWindow(t *testing.T) {
	mux, rows := serveRegistration(t)

	for _, bad := range []map[string]any{
		{"state_code": "KL", "number": "K-RERA/AG/1", "valid_from": "2031-03-31", "valid_to": "2026-04-01"},
		{"state_code": "KL", "number": "K-RERA/AG/1", "valid_from": "01/04/2026", "valid_to": "2031-03-31"},
		{"state_code": "K", "number": "K-RERA/AG/1", "valid_from": "2026-04-01", "valid_to": "2031-03-31"},
		{"state_code": "KL", "number": "", "valid_from": "2026-04-01", "valid_to": "2031-03-31"},
	} {
		w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/registration/authorities", bad)
		if w.Code != http.StatusUnprocessableEntity {
			t.Errorf("%v must be refused, got %d %s", bad, w.Code, w.Body.String())
		}
	}
	if len(rows.filed) != 0 {
		t.Fatalf("nothing may be filed from a refused request, got %d", len(rows.filed))
	}
}

func TestAFiledRegistrationComesBackOnTheChecklist(t *testing.T) {
	mux, rows := serveRegistration(t)
	rows.profile = identitystore.ManagerProfile{LegalName: "Menon Estates", Constitution: "proprietorship", State: "submitted"}

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/registration/authorities", map[string]any{
		"state_code": "kl", "number": "K-RERA/AG/123",
		"valid_from": "2020-04-01", "valid_to": "2099-03-31",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("POST authority: %d %s", w.Code, w.Body.String())
	}
	var out registrationBody
	decode(t, w, &out)
	if len(out.Authorities) != 1 || out.Authorities[0].StateCode != "KL" {
		t.Fatalf("the filed registration must come back, upper-cased: %+v", out.Authorities)
	}
	if out.Authorities[0].ValidTo != "2099-03-31" {
		t.Errorf("the expiry is the date it was filed with, got %q", out.Authorities[0].ValidTo)
	}
}

func TestADocumentOnFileLeavesTheChecklist(t *testing.T) {
	mux, rows := serveRegistration(t)
	rows.profile = identitystore.ManagerProfile{LegalName: "Menon Estates", Constitution: "proprietorship", State: "submitted"}

	w := post(t, mux, isolationtest.OrgFirm, "/v1/ops/registration/documents", map[string]any{
		"kind": "pan_card", "object_path": "firm/pan.pdf",
		"filename": "pan.pdf", "content_type": "application/pdf",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("POST document: %d %s", w.Code, w.Body.String())
	}
	var out registrationBody
	decode(t, w, &out)
	for _, o := range out.Outstanding {
		if o.Kind == "pan_card" {
			t.Fatal("a document on file must leave the outstanding list")
		}
	}
	if len(out.Outstanding) == 0 {
		t.Error("the rest of the checklist is still outstanding")
	}
}

func contains(all []string, want string) bool {
	for _, v := range all {
		if v == want {
			return true
		}
	}
	return false
}
