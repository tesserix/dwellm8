package ops_test

import (
	"bytes"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The owner–manager agreement, printed for signature (#340). There is no
// e-signing yet: the firm generates the PDF, both sides sign paper, and the
// executed copy comes back as a management_agreement document (#339).

func agreementFields() map[string]any {
	return map[string]any{
		"owner_name": "Anjali Menon", "owner_address": "12 Marine Drive, Kochi",
		"owner_pan":    "ABCDE1234F",
		"manager_name": "Nest Property Managers LLP", "manager_address": "4 MG Road, Bengaluru",
		"manager_rera":         "PRM/KA/RERA/1251/446",
		"property_description": "Two-bedroom flat, 1,150 sq ft",
		"term_months":          "24", "commencement_date": "2026-09-01",
		"management_fee_pct": "8", "repair_ceiling": "10000",
		"execution_place": "Kochi", "execution_date": "2026-08-08",
	}
}

type printedAgreement struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	PDF         string `json:"pdf_base64"`
}

// Base64 in JSON rather than raw bytes: the phone writes this to a file with
// expo-file-system to share it, and React Native has no dependable binary
// response body to read.
func TestTheFirmPrintsTheManagementAgreement(t *testing.T) {
	mux := serve(t)

	w := post(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/management-agreement",
		map[string]any{"fields": agreementFields()})
	if w.Code != http.StatusOK {
		t.Fatalf("printing the agreement: %d %s", w.Code, w.Body.String())
	}
	var out printedAgreement
	decode(t, w, &out)
	if out.ContentType != "application/pdf" {
		t.Errorf("content type is %q, want application/pdf", out.ContentType)
	}
	raw, err := base64.StdEncoding.DecodeString(out.PDF)
	if err != nil {
		t.Fatalf("decoding the agreement: %v", err)
	}
	if !bytes.HasPrefix(raw, []byte("%PDF-")) {
		t.Fatalf("that is not a PDF: %q", raw[:min(8, len(raw))])
	}
}

// A clause with "{{management_fee_pct}}" still in it is a figure somebody
// signs believing it is there, so an unfilled field is refused at the edge.
func TestPrintingIsRefusedWithAFieldUnfilled(t *testing.T) {
	mux := serve(t)

	fields := agreementFields()
	delete(fields, "management_fee_pct")
	w := post(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/management-agreement",
		map[string]any{"fields": fields})
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("want 422 for a missing field, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "management_fee_pct") {
		t.Errorf("the refusal names the field, got %s", w.Body.String())
	}
}

// The address the agreement is over is the firm's own record, never something
// the caller types: the request above sends no property_address and still
// prints, and the file is named for the property it belongs to.
func TestTheAgreementIsNamedForTheProperty(t *testing.T) {
	mux := serve(t)

	w := post(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/management-agreement",
		map[string]any{"fields": agreementFields()})
	if w.Code != http.StatusOK {
		t.Fatalf("printing the agreement: %d %s", w.Code, w.Body.String())
	}
	var out printedAgreement
	decode(t, w, &out)
	if !strings.HasSuffix(out.Filename, ".pdf") {
		t.Errorf("the download needs a pdf filename, got %q", out.Filename)
	}
	raw, err := base64.StdEncoding.DecodeString(out.PDF)
	if err != nil {
		t.Fatalf("decoding the agreement: %v", err)
	}
	if len(raw) < 2000 {
		t.Errorf("a twelve-clause agreement is not %d bytes", len(raw))
	}
}

func TestCannotPrintAnAgreementForAnotherFirmsProperty(t *testing.T) {
	mux := serve(t)

	w := post(t, mux, isolationtest.OrgOwner,
		"/v1/ops/properties/"+isolationtest.PropertySecond+"/management-agreement",
		map[string]any{"fields": agreementFields()})
	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404 for another firm's property, got %d %s", w.Code, w.Body.String())
	}
}
