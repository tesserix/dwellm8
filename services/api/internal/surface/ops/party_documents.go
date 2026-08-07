package ops

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/blob"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// WithBlob wires the bucket the owner's copies land in. Nil leaves upload-url
// off, and a document already in the bucket can still be recorded.
func (h *Handler) WithBlob(b *blob.Store) *Handler {
	h.blob = b
	return h
}

// PartyDocumentRoutes mounts the owner's own paperwork (#318). An owner living
// abroad cannot bring an original to an office, so what is held is an attested
// copy, and which attestation it carries is the fact a bank or an assessing
// officer asks about later.
func (h *Handler) PartyDocumentRoutes(r *authz.Registrar) {
	r.Handle("POST /v1/ops/parties/{id}/documents/upload-url", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.PartyDocumentUploadURL)
	r.Handle("POST /v1/ops/parties/{id}/documents", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.RecordPartyDocument)
	r.Handle("GET /v1/ops/parties/{id}/documents", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.PartyDocuments)
}

// The kinds worth asking an owner for. The same list is a CHECK on the table;
// it is repeated here so an unknown kind is a sentence rather than a constraint
// violation with no field attached to it.
var partyDocumentKinds = map[string]bool{
	"pan_card": true, "aadhaar_masked": true, "passport": true, "oci_card": true,
	"visa": true, "residence_permit": true, "photograph": true, "signature": true,
	"overseas_address_proof": true, "address_proof": true,
	"tax_residency_certificate": true, "form_10f": true, "section_197_certificate": true,
	"power_of_attorney": true, "ownership_proof": true, "bank_proof": true,
}

// none is a scan taken beside the original; self is the signed copy an owner
// abroad posts; the rest are what a bank asks a non-resident for.
var attestations = map[string]bool{
	"none": true, "self": true, "notary": true, "indian_mission": true, "apostille": true,
}

type partyDocumentRequest struct {
	Kind        string `json:"kind"`
	ObjectPath  string `json:"object_path"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`

	IssuingCountry string `json:"issuing_country"`
	// Whole, because it was just read off the document. It leaves this function
	// as a mask and is written down nowhere else (ADR-0013).
	Number string `json:"number"`

	IssuedOn  string `json:"issued_on"`
	ExpiresOn string `json:"expires_on"`

	Attestation string `json:"attestation"`
	AttestedOn  string `json:"attested_on"`
	AttestedBy  string `json:"attested_by"`
}

type partyDocumentResponse struct {
	Kind         string `json:"kind"`
	Filename     string `json:"filename"`
	ContentType  string `json:"content_type"`
	NumberMasked string `json:"number_masked,omitempty"`

	IssuingCountry string `json:"issuing_country,omitempty"`
	IssuedOn       string `json:"issued_on,omitempty"`
	ExpiresOn      string `json:"expires_on,omitempty"`

	Attestation string `json:"attestation"`
	AttestedOn  string `json:"attested_on,omitempty"`
	AttestedBy  string `json:"attested_by,omitempty"`

	State      string `json:"state"`
	Reason     string `json:"reason,omitempty"`
	UploadedBy string `json:"uploaded_by"`
	CreatedAt  string `json:"created_at"`
}

// partyDocumentFrom is the boundary: the number arrives whole and leaves as a
// mask, and every fact the table has a CHECK on is refused here first.
func partyDocumentFrom(req partyDocumentRequest) (identityservice.PartyDocument, error) {
	var none identityservice.PartyDocument

	kind := strings.TrimSpace(req.Kind)
	if !partyDocumentKinds[kind] {
		return none, fmt.Errorf("%q is not a document this asks an owner for", kind)
	}
	if strings.TrimSpace(req.Filename) == "" || strings.TrimSpace(req.ContentType) == "" {
		return none, fmt.Errorf("send filename and content_type")
	}

	country := strings.ToUpper(strings.TrimSpace(req.IssuingCountry))
	if country != "" && !countryCode.MatchString(country) {
		return none, fmt.Errorf("issuing_country must be a two-letter country code")
	}
	issued, err := optionalDate(req.IssuedOn, "issued_on")
	if err != nil {
		return none, err
	}
	expires, err := optionalDate(req.ExpiresOn, "expires_on")
	if err != nil {
		return none, err
	}

	attestation := strings.TrimSpace(req.Attestation)
	if attestation == "" {
		attestation = "none"
	}
	if !attestations[attestation] {
		return none, fmt.Errorf("%q is not an attestation", attestation)
	}
	attested, err := optionalDate(req.AttestedOn, "attested_on")
	if err != nil {
		return none, err
	}
	if (attestation == "none") != (attested == "") {
		return none, fmt.Errorf(
			"an attested copy needs the date it was attested, and an unattested one has none")
	}
	attestedBy := strings.TrimSpace(req.AttestedBy)
	if attestation != "none" && attestation != "self" && attestedBy == "" {
		return none, fmt.Errorf("send attested_by — an endorsement names the notary or officer who made it")
	}

	return identityservice.PartyDocument{
		Kind: kind, Filename: strings.TrimSpace(req.Filename),
		ContentType:    strings.TrimSpace(req.ContentType),
		IssuingCountry: country, NumberMasked: maskDocumentNumber(req.Number),
		IssuedOn: issued, ExpiresOn: expires,
		Attestation: attestation, AttestedOn: attested, AttestedBy: attestedBy,
	}, nil
}

func optionalDate(raw, field string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", v); err != nil {
		return "", fmt.Errorf("%s must be a date, as YYYY-MM-DD", field)
	}
	return v, nil
}

// A document number has no shape common to every issuer, so it cannot go
// through pii.Mask. The rule is the same as the foreign TIN's: the last four so
// the owner can tell which document it came from, and nothing before them. A
// number of four characters or fewer identifies nothing once masked, so none is
// kept at all.
func maskDocumentNumber(raw string) string {
	var b strings.Builder
	for _, c := range strings.ToUpper(strings.TrimSpace(raw)) {
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') {
			b.WriteRune(c)
		}
	}
	n := b.String()
	const keep = 4
	if len(n) <= keep {
		return ""
	}
	return strings.Repeat(string(pii.MaskPrefix), len(n)-keep) + n[len(n)-keep:]
}

type partyUploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type partyUploadResponse struct {
	UploadURL  string `json:"upload_url"`
	ObjectPath string `json:"object_path"`
}

// PartyDocumentUploadURL mints a signed PUT against the owner's own prefix, not
// the firm's: the mandate lets a manager collect the paperwork, it does not move
// whose it is. The path comes back because recording it is a second call.
func (h *Handler) PartyDocumentUploadURL(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil || h.blob == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	party := r.PathValue("id")
	var req partyUploadRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Filename) == "" || strings.TrimSpace(req.ContentType) == "" {
		writeError(w, http.StatusUnprocessableEntity, "send filename and content_type")
		return
	}
	owner, ok := h.ownerOrgFor(w, r, party)
	if !ok {
		return
	}
	url, path, err := h.blob.UploadURL(r.Context(), auth.SurfaceOps, owner,
		strings.TrimSpace(req.Filename), strings.TrimSpace(req.ContentType))
	if err != nil {
		h.log.Error("minting an owner document upload url", "party", party, "error", err)
		writeError(w, http.StatusInternalServerError, "could not prepare the upload")
		return
	}
	writeJSON(w, http.StatusOK, partyUploadResponse{UploadURL: url, ObjectPath: path})
}

// RecordPartyDocument files a copy that has already reached the bucket.
func (h *Handler) RecordPartyDocument(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	party := r.PathValue("id")
	var req partyDocumentRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	doc, err := partyDocumentFrom(req)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	owner, ok := h.ownerOrgFor(w, r, party)
	if !ok {
		return
	}
	// The path is the one upload-url minted, and it is checked against the owner
	// it was minted for: a path accepted as sent is a claim on somebody else's
	// object, and the download URL after it would honour the claim.
	doc.ObjectPath = strings.TrimSpace(req.ObjectPath)
	if !strings.HasPrefix(doc.ObjectPath, "org/"+owner+"/") {
		writeError(w, http.StatusUnprocessableEntity,
			"object_path must be the one this owner's upload-url returned")
		return
	}
	// Who filed the copy is part of what the copy is worth: "the firm holds a
	// passport" is not an answer, "this manager filed it on that day" is.
	p, _ := auth.From(r.Context())
	doc.UploadedBy = uploader(p)
	if doc.UploadedBy == "" {
		writeError(w, http.StatusForbidden, "sign in first")
		return
	}

	if err := h.owners.RecordPartyDocument(r.Context(), party, doc); err != nil {
		// The request is not logged: it came in holding a document number.
		h.log.Error("recording an owner document", "party", party, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "that document was refused")
		return
	}
	h.readBackPartyDocuments(w, r, party, http.StatusCreated)
}

// PartyDocuments is what is held for one landlord, newest first.
func (h *Handler) PartyDocuments(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	party := r.PathValue("id")
	if !h.actsForParty(w, r, party) {
		return
	}
	h.readBackPartyDocuments(w, r, party, http.StatusOK)
}

func (h *Handler) readBackPartyDocuments(w http.ResponseWriter, r *http.Request, party string, status int) {
	held, err := h.owners.PartyDocuments(r.Context(), party)
	if err != nil {
		h.log.Error("reading owner documents", "party", party, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the documents")
		return
	}
	out := make([]partyDocumentResponse, 0, len(held))
	for _, d := range held {
		out = append(out, partyDocumentResponse{
			Kind: d.Kind, Filename: d.Filename, ContentType: d.ContentType,
			NumberMasked: d.NumberMasked, IssuingCountry: d.IssuingCountry,
			IssuedOn: d.IssuedOn, ExpiresOn: d.ExpiresOn,
			Attestation: d.Attestation, AttestedOn: d.AttestedOn, AttestedBy: d.AttestedBy,
			State: d.State, Reason: d.Reason, UploadedBy: d.UploadedBy, CreatedAt: d.CreatedAt,
		})
	}
	writeJSON(w, status, struct {
		PartyID   string                  `json:"party_id"`
		Documents []partyDocumentResponse `json:"documents"`
	}{PartyID: party, Documents: out})
}
