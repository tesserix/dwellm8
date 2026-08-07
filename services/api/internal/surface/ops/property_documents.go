package ops

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// PropertyDocumentRoutes mounts the paperwork behind a property (#339). What
// onboarding is really asking for is the deed — or the power of attorney where
// the owner authorised somebody else to act — because a firm letting a flat on
// somebody's say-so has no answer when the real owner appears.
func (h *Handler) PropertyDocumentRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/properties/{id}/documents", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.PropertyDocuments)
	r.Handle("POST /v1/ops/properties/{id}/documents", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.RecordPropertyDocument)
	r.Handle("POST /v1/ops/properties/{id}/documents/upload-url", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.PropertyDocumentUploadURL)
}

type propertyDocumentResponse struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	UploadedBy  string `json:"uploaded_by"`
	CreatedAt   string `json:"created_at"`
}

type evidenceResponse struct {
	Proven   bool     `json:"proven"`
	Held     []string `json:"held"`
	Missing  []string `json:"missing"`
	Advisory []string `json:"advisory"`
}

type propertyDocumentRequest struct {
	Kind        string `json:"kind"`
	ObjectPath  string `json:"object_path"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

// PropertyDocuments is what is held against one property, and what it proves.
func (h *Handler) PropertyDocuments(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.propertyInScope(w, r); !ok {
		return
	}
	h.readBackPropertyDocuments(w, r, r.PathValue("id"), http.StatusOK)
}

// RecordPropertyDocument files a document that has already reached the bucket.
func (h *Handler) RecordPropertyDocument(w http.ResponseWriter, r *http.Request) {
	org, ok := h.propertyInScope(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req propertyDocumentRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Filename) == "" || strings.TrimSpace(req.ContentType) == "" {
		writeError(w, http.StatusUnprocessableEntity, "send filename and content_type")
		return
	}
	kind := strings.TrimSpace(req.Kind)
	if !propertyservice.KnownDocumentKind(kind) {
		writeError(w, http.StatusUnprocessableEntity,
			strconv.Quote(kind)+" is not a document this holds against a property")
		return
	}
	// The path is the one upload-url minted. A path accepted as sent is a claim
	// on another organisation's object, and the download URL would honour it.
	objectPath := strings.TrimSpace(req.ObjectPath)
	if !strings.HasPrefix(objectPath, "org/"+org+"/") {
		writeError(w, http.StatusUnprocessableEntity,
			"object_path must be the one this property's upload-url returned")
		return
	}
	p, _ := auth.From(r.Context())
	uploadedBy := uploader(p)
	if uploadedBy == "" {
		writeError(w, http.StatusForbidden, "sign in first")
		return
	}

	_, err := h.properties.RecordDocument(r.Context(), org, propertyservice.Document{
		PropertyID:  id,
		Kind:        kind,
		ObjectPath:  objectPath,
		Filename:    strings.TrimSpace(req.Filename),
		ContentType: strings.TrimSpace(req.ContentType),
		UploadedBy:  uploadedBy,
	})
	if err != nil {
		h.log.Error("filing a property document", "property", id, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "that document was refused")
		return
	}
	h.readBackPropertyDocuments(w, r, id, http.StatusCreated)
}

// PropertyDocumentUploadURL mints a signed PUT against the firm's own prefix.
func (h *Handler) PropertyDocumentUploadURL(w http.ResponseWriter, r *http.Request) {
	org, ok := h.propertyInScope(w, r)
	if !ok {
		return
	}
	if h.blob == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req partyUploadRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Filename) == "" || strings.TrimSpace(req.ContentType) == "" {
		writeError(w, http.StatusUnprocessableEntity, "send filename and content_type")
		return
	}
	url, path, err := h.blob.UploadURL(r.Context(), auth.SurfaceOps, org,
		strings.TrimSpace(req.Filename), strings.TrimSpace(req.ContentType))
	if err != nil {
		h.log.Error("minting a property document upload url", "property", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not prepare the upload")
		return
	}
	writeJSON(w, http.StatusOK, partyUploadResponse{UploadURL: url, ObjectPath: path})
}

// propertyInScope answers 404 for a property this session cannot see — the read
// is RLS-scoped, so another firm's property is indistinguishable from no
// property, which is the answer to give.
func (h *Handler) propertyInScope(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := h.properties.Get(r.Context(), id); err != nil {
		if errors.Is(err, propertyservice.ErrNoProperty) {
			writeError(w, http.StatusNotFound, "no such property")
			return "", false
		}
		h.log.Error("reading a property", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the property")
		return "", false
	}
	org, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusForbidden, "sign in first")
		return "", false
	}
	return org.String(), true
}

func (h *Handler) readBackPropertyDocuments(w http.ResponseWriter, r *http.Request, id string, status int) {
	held, ownership, err := h.properties.Documents(r.Context(), id)
	if err != nil {
		h.log.Error("reading property documents", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the documents")
		return
	}
	out := make([]propertyDocumentResponse, 0, len(held))
	for _, d := range held {
		out = append(out, propertyDocumentResponse{
			ID: d.ID, Kind: d.Kind, Filename: d.Filename, ContentType: d.ContentType,
			UploadedBy: d.UploadedBy, CreatedAt: d.CreatedAt,
		})
	}
	writeJSON(w, status, struct {
		PropertyID string                     `json:"property_id"`
		Documents  []propertyDocumentResponse `json:"documents"`
		Ownership  evidenceResponse           `json:"ownership"`
	}{
		PropertyID: id, Documents: out,
		Ownership: evidenceResponse{
			Proven: ownership.Proven, Held: ownership.Held,
			Missing: ownership.Missing, Advisory: ownership.Advisory,
		},
	})
}
