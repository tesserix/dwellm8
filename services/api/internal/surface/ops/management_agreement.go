package ops

import (
	"encoding/base64"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pdfdoc"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertydomain "github.com/tesserix/dwellm8/services/api/internal/property/domain"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// ManagementAgreementRoutes mounts the owner–manager agreement (#340). There is
// no e-signing yet: the firm prints this, both sides sign paper, and the
// executed copy is uploaded back as a management_agreement document (#339).
func (h *Handler) ManagementAgreementRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/management-agreement/fields", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.ManagementAgreementFields)
	r.Handle("POST /v1/ops/properties/{id}/management-agreement", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.PrintManagementAgreement)
}

// ManagementAgreementFields is what the firm has to fill before it can print,
// so the app can ask for exactly those and no more.
func (h *Handler) ManagementAgreementFields(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, struct {
		Fields           []string `json:"fields"`
		Supplied         []string `json:"supplied"`
		SaleNoticeMonths int      `json:"sale_notice_months"`
	}{
		Fields:   askedOf(propertydomain.AgreementFields()),
		Supplied: []string{"property_address"},
		// A term of the instrument, not a figure the firm sets per agreement.
		SaleNoticeMonths: propertydomain.SaleNoticeMonths,
	})
}

type agreementRequest struct {
	Fields map[string]string `json:"fields"`
}

// PrintManagementAgreement renders the instrument for one property. The address
// comes from the register rather than the request — an agreement over an
// address nobody holds is an agreement over nothing.
func (h *Handler) PrintManagementAgreement(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.propertyInScope(w, r); !ok {
		return
	}
	property, err := h.properties.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.log.Error("reading a property to print its agreement", "property", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the property")
		return
	}

	var req agreementRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	fields := map[string]string{}
	for k, v := range req.Fields {
		fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	fields["property_address"] = propertyAddress(property)

	agreement, err := propertydomain.BuildAgreement(fields)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	pdf, err := pdfdoc.Render(printable(agreement))
	if err != nil {
		h.log.Error("printing the management agreement", "property", property.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not print the agreement")
		return
	}

	filename := "management-agreement-" + slug(property.Code) + ".pdf"
	out := printedAgreement{
		Filename:    filename,
		ContentType: "application/pdf",
		// Base64 in JSON, not raw bytes: the phone writes this to a file to
		// share it, and React Native has no dependable binary body to read.
		PDF: base64.StdEncoding.EncodeToString(pdf),
	}
	h.fileTheAgreement(r, &out, property.ID, filename, pdf)
	writeJSON(w, http.StatusOK, out)
}

type printedAgreement struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	PDF         string `json:"pdf_base64"`
	DocumentID  string `json:"document_id,omitempty"`
	DownloadURL string `json:"download_url,omitempty"`
}

// fileTheAgreement keeps the printed copy in the firm's bucket and against the
// property, so what was issued can be read again beside the executed scan. A
// failure here leaves the caller its PDF rather than losing the print.
func (h *Handler) fileTheAgreement(r *http.Request, out *printedAgreement, propertyID, filename string, pdf []byte) {
	if h.blob == nil {
		return
	}
	org, ok := tenancy.From(r.Context())
	if !ok {
		return
	}
	ctx := r.Context()
	objectPath, err := h.blob.Put(ctx, auth.SurfaceOps, org.String(), filename, "application/pdf", pdf)
	if err != nil {
		h.log.Error("storing the printed agreement", "property", propertyID, "error", err)
		return
	}
	p, _ := auth.From(ctx)
	id, err := h.properties.RecordDocument(ctx, org.String(), propertyservice.Document{
		PropertyID: propertyID, Kind: "management_agreement", ObjectPath: objectPath,
		Filename: filename, ContentType: "application/pdf", UploadedBy: uploader(p),
	})
	if err != nil {
		h.log.Error("filing the printed agreement", "property", propertyID, "error", err)
		return
	}
	out.DocumentID = id
	if url, err := h.blob.DownloadURL(ctx, auth.SurfaceOps, objectPath); err == nil {
		out.DownloadURL = url
	} else {
		h.log.Error("minting an agreement download url", "property", propertyID, "error", err)
	}
}

// askedOf drops what the server fills in, leaving what the firm must supply.
func askedOf(all []string) []string {
	out := make([]string, 0, len(all))
	for _, f := range all {
		if f != "property_address" {
			out = append(out, f)
		}
	}
	return out
}

func propertyAddress(p propertyservice.Property) string {
	parts := []string{p.Name, p.AddressLine1, p.AddressLine2, p.Locality, p.City, p.StateCode, p.Pin}
	held := make([]string, 0, len(parts))
	for _, s := range parts {
		if strings.TrimSpace(s) != "" {
			held = append(held, strings.TrimSpace(s))
		}
	}
	return strings.Join(held, ", ")
}

func printable(a propertydomain.Agreement) pdfdoc.Document {
	// The footer names the document it is on: five instruments print through
	// here, and the wrong name at the foot of a signed page is the wrong page.
	d := pdfdoc.Document{Title: a.Title, Preamble: a.Recitals,
		Footer: "Dwellm8 " + strings.ToLower(a.Title)}
	for _, c := range a.Clauses {
		d.Sections = append(d.Sections, pdfdoc.Section{Number: c.Number, Heading: c.Heading, Body: c.Text})
	}
	for _, s := range a.Signatures {
		d.Signatures = append(d.Signatures, pdfdoc.SignatureBlock{Role: s.Role, Name: s.Name, Lines: s.Lines})
	}
	return d
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	if b.Len() == 0 {
		return "property"
	}
	return b.String()
}
