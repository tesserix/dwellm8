package ops

import (
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/blob"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pdfdoc"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertydomain "github.com/tesserix/dwellm8/services/api/internal/property/domain"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// A template previews as a PDF (#350). The .docx behind it is what a firm
// edits; it is not something a manager can read on a phone, and not something
// an owner or a tenant can open at all.

type templatePreviewResponse struct {
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	PDF         string `json:"pdf_base64"`
	DownloadURL string `json:"download_url,omitempty"`
	ExpiresIn   int    `json:"expires_in_seconds"`
}

// TemplatePreview renders the blank instrument. The link it returns is the one
// shared with an owner or a tenant, so it is minted per request and expires
// with the rest of them — a link that leaks is a link to nothing.
func (h *Handler) TemplatePreview(w http.ResponseWriter, r *http.Request) {
	if h.templates == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	t, err := h.templates.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, propertyservice.ErrNoTemplate) {
			writeError(w, http.StatusNotFound, "no such template")
			return
		}
		h.log.Error("reading a template to preview it", "template", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the template")
		return
	}

	instrument, err := propertydomain.PreviewInstrument(t.Kind)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pdf, err := pdfdoc.Render(printable(instrument))
	if err != nil {
		h.log.Error("printing a template preview", "template", t.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not print the preview")
		return
	}

	filename := strings.ReplaceAll(t.Kind, "_", "-") + "-preview.pdf"
	out := templatePreviewResponse{
		Kind: t.Kind, Name: t.Name, Filename: filename, ContentType: "application/pdf",
		// Base64 in JSON, as the printed agreement does: React Native has no
		// dependable binary body, and the phone writes this to a file to share.
		PDF:       base64.StdEncoding.EncodeToString(pdf),
		ExpiresIn: int(blob.DownloadTTL.Seconds()),
	}
	if url, ok := h.previewLink(r, t.ID, filename, pdf); ok {
		out.DownloadURL = url
	}
	writeJSON(w, http.StatusOK, out)
}

// previewLink stores the rendered preview and signs a short-lived GET for it.
// A failure leaves the caller its PDF rather than losing the preview.
func (h *Handler) previewLink(r *http.Request, templateID, filename string, pdf []byte) (string, bool) {
	if h.blob == nil {
		return "", false
	}
	org, ok := tenancy.From(r.Context())
	if !ok {
		return "", false
	}
	ctx := r.Context()
	objectPath, err := h.blob.Put(ctx, auth.SurfaceOps, org.String(), filename, "application/pdf", pdf)
	if err != nil {
		h.log.Error("storing a template preview", "template", templateID, "error", err)
		return "", false
	}
	url, err := h.blob.DownloadURL(ctx, auth.SurfaceOps, objectPath)
	if err != nil {
		h.log.Error("minting a template preview url", "template", templateID, "error", err)
		return "", false
	}
	return url, true
}
