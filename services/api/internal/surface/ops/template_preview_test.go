package ops_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// A template previews as a PDF a phone can read (#350). Before this the only
// thing behind the tap was the .docx, which no manager could check and no
// owner or tenant could open.

type templatePreview struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	PDF         string `json:"pdf_base64"`
	DownloadURL string `json:"download_url"`
	ExpiresIn   int    `json:"expires_in_seconds"`
}

func previewOf(t *testing.T, mux *http.ServeMux, kind string) templatePreview {
	t.Helper()
	out := templates(t, mux, "/v1/ops/document-templates?kind="+kind)
	if len(out.Templates) == 0 {
		t.Fatalf("the library has no %s to preview", kind)
	}
	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/document-templates/"+out.Templates[0].ID+"/preview")
	if w.Code != http.StatusOK {
		t.Fatalf("previewing %s: %d %s", kind, w.Code, w.Body.String())
	}
	var p templatePreview
	decode(t, w, &p)
	return p
}

func TestEveryInstrumentPreviewsAsAPDF(t *testing.T) {
	mux := serve(t)
	for _, kind := range []string{
		"management_agreement", "onboarding_checklist",
		"rent_agreement", "lease_deed", "power_of_attorney",
	} {
		t.Run(kind, func(t *testing.T) {
			p := previewOf(t, mux, kind)
			if p.ContentType != "application/pdf" {
				t.Errorf("a preview a phone can read is a PDF, got %q", p.ContentType)
			}
			if !strings.HasSuffix(p.Filename, ".pdf") {
				t.Errorf("the filename does not name a PDF: %q", p.Filename)
			}
			raw, err := base64.StdEncoding.DecodeString(p.PDF)
			if err != nil {
				t.Fatalf("decoding the preview: %v", err)
			}
			if !strings.HasPrefix(string(raw), "%PDF") {
				t.Errorf("what came back is not a PDF at all: %.8q", raw)
			}
		})
	}
}

// The link is the thing that gets shared with an owner or a tenant, so how
// long it lives is part of the answer rather than something to assume.
func TestAPreviewSaysHowLongItsLinkLives(t *testing.T) {
	mux := serve(t)
	p := previewOf(t, mux, "rent_agreement")
	if p.ExpiresIn <= 0 || p.ExpiresIn > 3600 {
		t.Errorf("a shared link lives minutes, not %d seconds", p.ExpiresIn)
	}
}

func TestPreviewingATemplateThatDoesNotExistIs404(t *testing.T) {
	mux := serve(t)
	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/document-templates/11111111-2222-3333-4444-555555555555/preview")
	if w.Code != http.StatusNotFound {
		t.Fatalf("a template nobody holds is a 404, got %d %s", w.Code, w.Body.String())
	}
}
