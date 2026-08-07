// Package owner is the landlord's own view of their portfolio — the `own`
// app's backend. Issue-less counterpart to internal/surface/resident: same
// reason to exist (ADR-0001 §3's composition-not-coupling seam), different
// audience.
//
// It is a **surface**, not a ninth module: it owns no table, defines no
// rule. It composes the property, lease and money services' public
// interfaces into the screens an owner opens — their portfolio, what each
// property has paid and owes, the monthly statement, and the documents
// against it — plus GCS-backed storage for those documents, always against
// the `own` app's bucket (internal/platform/blob), since this surface is
// reached from nowhere else.
package owner

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/activity"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/blob"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// ist matches internal/surface/resident: every date this surface renders is
// an Indian calendar date.
var ist = time.FixedZone("IST", 5*3600+1800)

const maxBody = 16 << 10
const activityLimit = 100

// Handler serves the owner surface.
type Handler struct {
	properties    *propertyservice.Properties
	leases        *leaseservice.Leases
	statements    *moneyservice.Statements
	organisations *identityservice.Organisations
	activity      activity.Feeder
	blob          *blob.Store
	log           *slog.Logger
	now           func() time.Time
}

// New wires the surface to the services it composes. blob may be nil — the
// document routes answer 404 rather than the process failing to start, the
// same optional-dependency shape WithActivity uses on the resident surface.
func New(p *propertyservice.Properties, l *leaseservice.Leases, s *moneyservice.Statements,
	orgs *identityservice.Organisations, feed activity.Feeder, b *blob.Store,
	log *slog.Logger, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{
		properties: p, leases: l, statements: s, organisations: orgs,
		activity: feed, blob: b, log: log, now: now,
	}
}

// Routes mounts the surface.
func (h *Handler) Routes(r *authz.Registrar) {
	r.Handle("GET /v1/owner/properties", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Properties)
	r.Handle("GET /v1/owner/properties/{id}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("property", "id")}, h.Property)
	r.Handle("GET /v1/owner/properties/{id}/statement", authz.Check{
		Relation: "can_view", Object: authz.PathObject("property", "id")}, h.Statement)
	r.Handle("GET /v1/owner/properties/{id}/documents", authz.Check{
		Relation: "can_view", Object: authz.PathObject("property", "id")}, h.ListDocuments)
	r.Handle("POST /v1/owner/properties/{id}/documents/upload-url", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("property", "id")}, h.UploadURL)
	r.Handle("GET /v1/owner/activity", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Activity)
}

// propertyResponse is one property as the owner sees it on their portfolio.
type propertyResponse struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2,omitempty"`
	Locality     string `json:"locality"`
	City         string `json:"city"`
	StateCode    string `json:"state_code"`
	Pin          string `json:"pin"`
	UnitCount    int    `json:"unit_count"`

	// Agency is who manages this property, from the organisation the property
	// row belongs to — empty when it is self-managed (that organisation's own
	// kind is "owner", not "agency").
	Agency string `json:"agency,omitempty"`

	// Occupied is false, and every field below it omitted, when the property
	// has no active tenancy — a vacancy, not an error.
	Occupied bool `json:"occupied"`

	LeaseID        string `json:"lease_id,omitempty"`
	LeaseExpiresOn string `json:"lease_expires_on,omitempty"`
	// BilledThroughOn is the last day rent has been invoiced for — the
	// closest fact this system has to "paid to date" without a per-tenant
	// party id in hand; see money/service/statement.go's BilledThrough.
	BilledThroughOn string `json:"billed_through_on,omitempty"`
}

// Properties lists the caller's portfolio.
func (h *Handler) Properties(w http.ResponseWriter, r *http.Request) {
	props, err := h.properties.List(r.Context())
	if err != nil {
		h.log.Error("listing properties", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read your properties")
		return
	}
	today := effective.DateOf(h.now(), ist)
	out := make([]propertyResponse, 0, len(props))
	for _, p := range props {
		out = append(out, h.present(r, p, today))
	}
	writeJSON(w, http.StatusOK, map[string]any{"properties": out})
}

// Property reads one property.
func (h *Handler) Property(w http.ResponseWriter, r *http.Request) {
	p, err := h.properties.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		h.fail(w, "reading a property", err)
		return
	}
	writeJSON(w, http.StatusOK, h.present(r, p, effective.DateOf(h.now(), ist)))
}

func (h *Handler) present(r *http.Request, p propertyservice.Property, today effective.Date) propertyResponse {
	out := propertyResponse{
		ID: p.ID, Code: p.Code, Name: p.Name, Kind: p.Kind,
		AddressLine1: p.AddressLine1, AddressLine2: p.AddressLine2,
		Locality: p.Locality, City: p.City, StateCode: p.StateCode, Pin: p.Pin,
		UnitCount: p.UnitCount,
	}

	if tenantID, err := h.properties.TenantOf(r.Context(), p.ID); err == nil {
		if name, kind, err := h.organisations.Name(r.Context(), tenancy.ID(tenantID)); err == nil && kind == "agency" {
			out.Agency = name
		}
	}

	t, ok, err := h.leases.ActiveOnProperty(r.Context(), p.ID, today)
	if err != nil {
		h.log.Error("reading the active tenancy on a property", "property", p.ID, "error", err)
		return out
	}
	if !ok {
		return out
	}
	out.Occupied, out.LeaseID = true, t.LeaseID
	if !t.Term.To().Zero() {
		out.LeaseExpiresOn = t.Term.To().String()
	}
	if through, err := h.statements.BilledThrough(r.Context(), t.LeaseID); err == nil && !through.IsZero() {
		out.BilledThroughOn = through.Format(dateLayout)
	}
	return out
}

// statementResponse is one property's month, net.
type statementResponse struct {
	PropertyID   string      `json:"property_id"`
	From         string      `json:"from"`
	To           string      `json:"to"`
	NetMinor     int64       `json:"net_amount_minor"`
	IncomeMinor  int64       `json:"income_amount_minor"`
	ExpenseMinor int64       `json:"expense_amount_minor"`
	Lines        []lineEntry `json:"lines"`
}

type lineEntry struct {
	Account     string `json:"account"`
	Label       string `json:"label"`
	AmountMinor int64  `json:"amount_minor"`
}

// Statement reads one property's statement for a month — ?month=2026-07,
// defaulting to the current one.
func (h *Handler) Statement(w http.ResponseWriter, r *http.Request) {
	propertyID := r.PathValue("id")
	month := h.now()
	if q := r.URL.Query().Get("month"); q != "" {
		parsed, err := time.Parse("2006-01", q)
		if err != nil {
			writeError(w, http.StatusBadRequest, "month must be YYYY-MM")
			return
		}
		month = parsed
	}

	st, err := h.statements.OwnerStatement(r.Context(), propertyID, month)
	if err != nil {
		h.fail(w, "reading a statement", err)
		return
	}
	lines := make([]lineEntry, 0, len(st.Lines))
	for _, l := range st.Lines {
		lines = append(lines, lineEntry{Account: l.Account, Label: l.Label, AmountMinor: int64(l.AmountMinor)})
	}
	writeJSON(w, http.StatusOK, statementResponse{
		PropertyID: st.PropertyID, From: st.From.Format(dateLayout), To: st.To.Format(dateLayout),
		NetMinor: int64(st.NetMinor), IncomeMinor: int64(st.IncomeMinor), ExpenseMinor: int64(st.ExpenseMinor),
		Lines: lines,
	})
}

// activityEntry mirrors the resident surface's, minus the resident-only
// filtering: this audience is the landlord's own organisation.
type activityEntry struct {
	Kind       string `json:"kind"`
	OccurredAt string `json:"occurred_at"`
	Body       string `json:"body,omitempty"`
}

// Activity is the whole-organisation feed — every module's outbox, read
// back, for the portfolio this session acts for.
func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	if h.activity == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	entries, err := h.activity.Feed(r.Context(), activity.Query{
		Audience: activity.AudienceOrg, Limit: activityLimit,
	})
	if err != nil {
		h.fail(w, "reading activity", err)
		return
	}
	out := make([]activityEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, activityEntry{Kind: e.Kind, OccurredAt: e.OccurredAt.Format(time.RFC3339), Body: e.Body})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// documentResponse is one stored document, with a fresh signed URL to fetch
// it — minted on read rather than stored, because a URL that outlives its
// signature is a broken link and one that never expires is a leak.
type documentResponse struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	UploadedAt  string `json:"uploaded_at"`
	DownloadURL string `json:"download_url,omitempty"`
}

// ListDocuments lists what is on file for a property, newest first, each with
// a signed URL minted at read time (#339).
func (h *Handler) ListDocuments(w http.ResponseWriter, r *http.Request) {
	propertyID := r.PathValue("id")
	held, _, err := h.properties.Documents(r.Context(), propertyID)
	if err != nil {
		h.fail(w, "reading a property's documents", err)
		return
	}
	out := make([]documentResponse, 0, len(held))
	for _, d := range held {
		item := documentResponse{
			ID: d.ID, Filename: d.Filename, ContentType: d.ContentType, UploadedAt: d.CreatedAt,
		}
		if h.blob != nil {
			url, err := h.blob.DownloadURL(r.Context(), auth.SurfaceOwn, d.ObjectPath)
			if err != nil {
				// A missing object is one broken row, not a failed list.
				h.log.Warn("signing a document url", "document", d.ID, "error", err)
			} else {
				item.DownloadURL = url
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"documents": out})
}

type uploadURLRequest struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
}

type uploadURLResponse struct {
	UploadURL string `json:"upload_url"`
}

// UploadURL mints a signed PUT URL for a new document against this
// property, scoped to the property's own organisation — the agency's, for a
// delegated property, not the caller's — so the object lands in the same
// tenant boundary the documents table's foreign key expects.
func (h *Handler) UploadURL(w http.ResponseWriter, r *http.Request) {
	if h.blob == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	propertyID := r.PathValue("id")
	var req uploadURLRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if req.Filename == "" || req.ContentType == "" {
		writeError(w, http.StatusUnprocessableEntity, "filename and content_type are required")
		return
	}

	tenantID, err := h.properties.TenantOf(r.Context(), propertyID)
	if err != nil {
		h.fail(w, "resolving a property's organisation", err)
		return
	}
	uploadURL, _, err := h.blob.UploadURL(r.Context(), auth.SurfaceOwn, tenantID, req.Filename, req.ContentType)
	if err != nil {
		h.log.Error("minting an upload url", "property", propertyID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not prepare the upload")
		return
	}
	writeJSON(w, http.StatusOK, uploadURLResponse{UploadURL: uploadURL})
}

const dateLayout = "2006-01-02"

func (h *Handler) fail(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, propertyservice.ErrNoProperty):
		writeError(w, http.StatusNotFound, "no such property")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "sign in again")
	default:
		h.log.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read that")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request: "+err.Error())
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
