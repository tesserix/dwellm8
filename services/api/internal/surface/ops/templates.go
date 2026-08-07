package ops

import (
	"errors"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// WithTemplates wires the instrument library (#341). Nil leaves the routes
// answering "not here yet" rather than half-serving them.
func (h *Handler) WithTemplates(t *propertyservice.Templates) *Handler {
	h.templates = t
	return h
}

// TemplateRoutes mounts the agreements a firm issues. There is no e-signing:
// a manager downloads the .docx, it is signed on paper, and the executed copy
// comes back as a document against the property.
func (h *Handler) TemplateRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/document-templates", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.DocumentTemplates)
	r.Handle("GET /v1/ops/document-templates/{id}", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Template)
	r.Handle("POST /v1/ops/document-templates/upload-url", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.TemplateUploadURL)
	r.Handle("POST /v1/ops/document-templates", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.ReviseTemplate)
}

type templateResponse struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Jurisdiction string   `json:"jurisdiction"`
	Name         string   `json:"name"`
	Version      int      `json:"version"`
	IsDefault    bool     `json:"is_default"`
	Filename     string   `json:"filename"`
	ContentType  string   `json:"content_type"`
	MergeFields  []string `json:"merge_fields"`
	PublishedAt  string   `json:"published_at,omitempty"`
	SupersededAt string   `json:"superseded_at,omitempty"`
	UploadedBy   string   `json:"uploaded_by"`
	DownloadURL  string   `json:"download_url,omitempty"`
}

func templateOut(t propertyservice.Template) templateResponse {
	fields := t.MergeFields
	if fields == nil {
		fields = []string{}
	}
	return templateResponse{
		ID: t.ID, Kind: t.Kind, Jurisdiction: t.Jurisdiction, Name: t.Name,
		Version: t.Version, IsDefault: t.IsDefault, Filename: t.Filename,
		ContentType: t.ContentType, MergeFields: fields,
		PublishedAt: t.PublishedAt, SupersededAt: t.SupersededAt, UploadedBy: t.UploadedBy,
	}
}

// Templates lists what the firm issues: one live instrument per kind, its own
// revision where it has one. ?history=true adds the superseded versions, which
// is what a signed copy from two years ago has to be read against.
func (h *Handler) DocumentTemplates(w http.ResponseWriter, r *http.Request) {
	if h.templates == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	kind := strings.TrimSpace(r.URL.Query().Get("kind"))
	read := h.templates.Live
	if r.URL.Query().Get("history") == "true" {
		read = h.templates.History
	}
	held, err := read(r.Context(), kind)
	if err != nil {
		h.log.Error("listing document templates", "kind", kind, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the templates")
		return
	}
	out := make([]templateResponse, 0, len(held))
	for _, t := range held {
		out = append(out, templateOut(t))
	}
	writeJSON(w, http.StatusOK, struct {
		Templates []templateResponse `json:"templates"`
	}{Templates: out})
}

// Template reads one, with a short-lived URL to download the .docx behind it —
// the download the manager edits and uploads back.
func (h *Handler) Template(w http.ResponseWriter, r *http.Request) {
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
		h.log.Error("reading a document template", "template", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the template")
		return
	}
	out := templateOut(t)
	if h.blob != nil {
		url, err := h.blob.DownloadURL(r.Context(), auth.SurfaceOps, t.ObjectPath)
		if err != nil {
			h.log.Error("minting a template download url", "template", t.ID, "error", err)
		} else {
			out.DownloadURL = url
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type templateUploadRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

// TemplateUploadURL mints a signed PUT for the firm's edited copy.
func (h *Handler) TemplateUploadURL(w http.ResponseWriter, r *http.Request) {
	if h.templates == nil || h.blob == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req templateUploadRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Filename) == "" || strings.TrimSpace(req.ContentType) == "" {
		writeError(w, http.StatusUnprocessableEntity, "send filename and content_type")
		return
	}
	org, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "no organisation in context")
		return
	}
	url, path, err := h.blob.UploadURL(r.Context(), auth.SurfaceOps, org.String(),
		strings.TrimSpace(req.Filename), strings.TrimSpace(req.ContentType))
	if err != nil {
		h.log.Error("minting a template upload url", "error", err)
		writeError(w, http.StatusInternalServerError, "could not prepare the upload")
		return
	}
	writeJSON(w, http.StatusOK, partyUploadResponse{UploadURL: url, ObjectPath: path})
}

type templateRequest struct {
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	ObjectPath  string   `json:"object_path"`
	Filename    string   `json:"filename"`
	ContentType string   `json:"content_type"`
	MergeFields []string `json:"merge_fields"`
}

// ReviseTemplate publishes the firm's own version of an instrument, which
// supersedes whatever it issued before.
func (h *Handler) ReviseTemplate(w http.ResponseWriter, r *http.Request) {
	if h.templates == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req templateRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.Filename) == "" ||
		strings.TrimSpace(req.ContentType) == "" {
		writeError(w, http.StatusUnprocessableEntity, "send name, filename and content_type")
		return
	}
	org, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusInternalServerError, "no organisation in context")
		return
	}
	// The same rule as the owner's documents: the path is the one upload-url
	// minted for this organisation, never one the client chose.
	objectPath := strings.TrimSpace(req.ObjectPath)
	if !strings.HasPrefix(objectPath, "org/"+org.String()+"/") {
		writeError(w, http.StatusUnprocessableEntity,
			"object_path must be the one this firm's upload-url returned")
		return
	}
	p, _ := auth.From(r.Context())
	who := uploader(p)
	if who == "" {
		writeError(w, http.StatusForbidden, "sign in first")
		return
	}

	out, err := h.templates.Revise(r.Context(), org.String(), propertyservice.Template{
		Kind: strings.TrimSpace(req.Kind), Name: strings.TrimSpace(req.Name),
		ObjectPath: objectPath, Filename: strings.TrimSpace(req.Filename),
		ContentType: strings.TrimSpace(req.ContentType),
		MergeFields: req.MergeFields, UploadedBy: who,
	})
	if err != nil {
		h.log.Error("revising a document template", "kind", req.Kind, "error", err)
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, templateOut(out))
}
