// Package http is the discovery module's HTTP surface, in two halves: the
// owner's listing and enquiry management here, and the anonymous public site
// in public.go. They are split because they authenticate differently — one is
// a signed-in organisation, the other a stranger holding a browsing token —
// and a handler that served both would have to keep asking which it was.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// maxBody bounds a listing request. A listing is text and numbers; anything
// near this is not one.
const maxBody = 128 << 10

// Listings handles the owner's side of the funnel.
type Listings struct {
	listings  *service.Listings
	enquiries *service.Enquiries
	log       *slog.Logger
}

// NewListings builds the handler.
func NewListings(l *service.Listings, e *service.Enquiries, log *slog.Logger) *Listings {
	return &Listings{listings: l, enquiries: e, log: log}
}

// Routes mounts the owner surface. Each route names its ADR-0020 check.
func (h *Listings) Routes(r *authz.Registrar) {
	r.Handle("POST /v1/listings", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.Create)
	r.Handle("GET /v1/listings", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.List)
	r.Handle("GET /v1/listings/{id}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("listing", "id")}, h.Get)
	r.Handle("POST /v1/listings/{id}/publish", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("listing", "id")}, h.move(h.publish))
	r.Handle("POST /v1/listings/{id}/pause", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("listing", "id")}, h.move(h.listings.Pause))
	r.Handle("POST /v1/listings/{id}/resume", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("listing", "id")}, h.move(h.listings.Resume))
	r.Handle("POST /v1/listings/{id}/withdraw", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("listing", "id")}, h.move(h.listings.Withdraw))

	r.Handle("GET /v1/enquiries", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Pipeline)
	r.Handle("POST /v1/enquiries/{id}/respond", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.Respond)
	r.Handle("POST /v1/enquiries/{id}/advance", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.Advance)
	r.Handle("POST /v1/enquiries/{id}/convert", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.Convert)
}

// createRequest is the wire shape. Money is amount_minor and an integer,
// everywhere (ADR-0007).
type createRequest struct {
	PropertyID   string `json:"property_id"`
	UnitID       string `json:"unit_id"`
	UnitParentID string `json:"unit_parent_id"`

	Headline  string `json:"headline"`
	Locality  string `json:"locality"`
	City      string `json:"city"`
	StateCode string `json:"state_code"`

	RentMinor         int64 `json:"rent_minor"`
	DepositMinor      int64 `json:"deposit_minor"`
	MaintenanceMinor  int64 `json:"maintenance_minor"`
	ParkingMinor      int64 `json:"parking_minor"`
	OtherMonthlyMinor int64 `json:"other_monthly_minor"`
	OneTimeMinor      int64 `json:"one_time_minor"`
	CostsConfirmed    bool  `json:"costs_confirmed"`

	Bedrooms       int     `json:"bedrooms"`
	CarpetAreaSqft float64 `json:"carpet_area_sqft"`
	AvailableFrom  string  `json:"available_from"`

	// Agent registration capture (#143). Omitted means the owner lists.
	ListerKind        string `json:"lister_kind"`
	AgentRegistration string `json:"agent_registration"`
}

// Create writes a draft from live inventory.
func (h *Listings) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	d := domain.Draft{
		PropertyID: req.PropertyID, UnitID: req.UnitID, UnitParentID: req.UnitParentID,
		Headline: req.Headline, Locality: req.Locality, City: req.City, StateCode: req.StateCode,
		Costs: domain.Costs{
			RentMinor: req.RentMinor, DepositMinor: req.DepositMinor,
			MaintenanceMinor: req.MaintenanceMinor, ParkingMinor: req.ParkingMinor,
			OtherMonthlyMinor: req.OtherMonthlyMinor, OneTimeMinor: req.OneTimeMinor,
			Confirmed: req.CostsConfirmed,
		},
		Bedrooms: req.Bedrooms, CarpetAreaSqft: req.CarpetAreaSqft,
		ListerKind: req.ListerKind, AgentRegistration: req.AgentRegistration,
	}
	if req.AvailableFrom != "" {
		var err error
		if d.AvailableFrom, err = effective.ParseDate(req.AvailableFrom); err != nil {
			writeError(w, http.StatusBadRequest, "available_from must be a date, as YYYY-MM-DD")
			return
		}
	}

	id, err := h.listings.Create(r.Context(), d)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "state": string(domain.StateDraft)})
	case errors.Is(err, domain.ErrDraft):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("creating a listing", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create the listing")
	}
}

// publish adapts Publish to the shared move shape.
func (h *Listings) publish(ctx context.Context, id string, actor events.Actor) error {
	return h.listings.Publish(ctx, id, actor)
}

// move wraps the four lifecycle verbs: same path shape, same refusals.
func (h *Listings) move(f func(ctx context.Context, id string, actor events.Actor) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "no listing named")
			return
		}
		err := f(r.Context(), id, events.Actor{Kind: events.ActorSystem})
		switch {
		case err == nil:
			l, gerr := h.listings.Get(r.Context(), id)
			if gerr != nil {
				writeJSON(w, http.StatusOK, map[string]string{"id": id})
				return
			}
			writeJSON(w, http.StatusOK, map[string]string{"id": id, "state": string(l.State)})
		case errors.Is(err, store.ErrNoListing):
			writeError(w, http.StatusNotFound, "no such listing")
		case errors.Is(err, domain.ErrTransition):
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, domain.ErrDisclosure), errors.Is(err, domain.ErrDraft):
			// 422: something about the listing is missing, and the message names it.
			writeError(w, http.StatusUnprocessableEntity, err.Error())
		case errors.Is(err, service.ErrOccupied), errors.Is(err, store.ErrAlreadyAdvertised):
			// 409: the request is fine and the world disagrees. The message names
			// the tenancy or the other advert.
			writeError(w, http.StatusConflict, err.Error())
		case errors.Is(err, tenancy.ErrNoTenant):
			writeError(w, http.StatusUnauthorized, "no organisation in this request")
		default:
			h.log.Error("moving a listing", "listing", id, "error", err)
			writeError(w, http.StatusInternalServerError, "could not update the listing")
		}
	}
}

// listingResponse is the owner's view, costs itemised.
type listingResponse struct {
	ID          string `json:"id"`
	PropertyID  string `json:"property_id"`
	UnitID      string `json:"unit_id"`
	State       string `json:"state"`
	PublishedAt string `json:"published_at,omitempty"`

	Headline  string `json:"headline"`
	Locality  string `json:"locality"`
	City      string `json:"city"`
	StateCode string `json:"state_code"`

	RentMinor         int64 `json:"rent_minor"`
	DepositMinor      int64 `json:"deposit_minor"`
	MaintenanceMinor  int64 `json:"maintenance_minor"`
	ParkingMinor      int64 `json:"parking_minor"`
	OtherMonthlyMinor int64 `json:"other_monthly_minor"`
	OneTimeMinor      int64 `json:"one_time_minor"`
	CostsConfirmed    bool  `json:"costs_confirmed"`
	TotalMonthlyMinor int64 `json:"total_monthly_minor"`
	TotalOneTimeMinor int64 `json:"total_one_time_minor"`

	Bedrooms      int    `json:"bedrooms,omitempty"`
	AvailableFrom string `json:"available_from,omitempty"`
}

func toResponse(l store.Listing) listingResponse {
	out := listingResponse{
		ID: l.ID, PropertyID: l.PropertyID, UnitID: l.UnitID, State: string(l.State),
		Headline: l.Headline, Locality: l.Locality, City: l.City, StateCode: l.StateCode,
		RentMinor: l.Costs.RentMinor, DepositMinor: l.Costs.DepositMinor,
		MaintenanceMinor: l.Costs.MaintenanceMinor, ParkingMinor: l.Costs.ParkingMinor,
		OtherMonthlyMinor: l.Costs.OtherMonthlyMinor, OneTimeMinor: l.Costs.OneTimeMinor,
		CostsConfirmed:    l.Costs.Confirmed,
		TotalMonthlyMinor: l.Costs.TotalMonthlyMinor(),
		TotalOneTimeMinor: l.Costs.TotalOneTimeMinor(),
		Bedrooms:          l.Bedrooms,
	}
	if l.PublishedAt != nil {
		out.PublishedAt = l.PublishedAt.UTC().Format(time.RFC3339)
	}
	if !l.AvailableFrom.Zero() {
		out.AvailableFrom = l.AvailableFrom.String()
	}
	return out
}

// Get is one listing, the owner's view.
func (h *Listings) Get(w http.ResponseWriter, r *http.Request) {
	l, err := h.listings.Get(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, toResponse(l))
	case errors.Is(err, store.ErrNoListing):
		writeError(w, http.StatusNotFound, "no such listing")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("reading a listing", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the listing")
	}
}

// List is the owner's adverts, filterable by state.
func (h *Listings) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.listings.List(r.Context(), r.URL.Query().Get("state"))
	switch {
	case err == nil:
		out := make([]listingResponse, 0, len(list))
		for _, l := range list {
			out = append(out, toResponse(l))
		}
		writeJSON(w, http.StatusOK, map[string]any{"listings": out})
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("listing listings", "error", err)
		writeError(w, http.StatusInternalServerError, "could not list the listings")
	}
}

// enquiryResponse is one pipeline row.
type enquiryResponse struct {
	ID            string `json:"id"`
	ListingID     string `json:"listing_id"`
	Headline      string `json:"headline,omitempty"`
	Kind          string `json:"kind"`
	State         string `json:"state"`
	Message       string `json:"message,omitempty"`
	ContactMasked string `json:"contact_masked,omitempty"`
	ScheduledFor  string `json:"scheduled_for,omitempty"`
	CreatedAt     string `json:"created_at"`
}

func toEnquiryResponse(e store.Enquiry) enquiryResponse {
	out := enquiryResponse{
		ID: e.ID, ListingID: e.ListingID, Headline: e.Headline, Kind: e.Kind,
		State: e.State, Message: e.Message, ContactMasked: e.ContactMasked,
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if e.ScheduledFor != nil {
		out.ScheduledFor = e.ScheduledFor.UTC().Format(time.RFC3339)
	}
	return out
}

// Pipeline is the owner's enquiry queue.
func (h *Listings) Pipeline(w http.ResponseWriter, r *http.Request) {
	list, err := h.enquiries.Pipeline(r.Context(), r.URL.Query().Get("state"))
	switch {
	case err == nil:
		out := make([]enquiryResponse, 0, len(list))
		for _, e := range list {
			out = append(out, toEnquiryResponse(e))
		}
		writeJSON(w, http.StatusOK, map[string]any{"enquiries": out})
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("reading the pipeline", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the enquiries")
	}
}

// Respond records the owner's engagement and returns the masked bridge when a
// provider is wired — the number either side dials, never a real one.
func (h *Listings) Respond(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	b, err := h.enquiries.Respond(r.Context(), id, events.Actor{Kind: events.ActorSystem})
	switch {
	case err == nil:
		out := map[string]string{"id": id, "state": "owner_responded"}
		if b.ProxyMasked != "" {
			out["proxy_number"] = b.ProxyMasked
			out["bridge_expires_at"] = b.ExpiresAt.UTC().Format(time.RFC3339)
		}
		writeJSON(w, http.StatusOK, out)
	case errors.Is(err, store.ErrNoEnquiry):
		writeError(w, http.StatusNotFound, "no such enquiry")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("responding to an enquiry", "enquiry", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not respond")
	}
}

// Advance moves an enquiry along the pipeline.
func (h *Listings) Advance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req struct {
		State string `json:"state"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	err := h.enquiries.Advance(r.Context(), id, req.State, events.Actor{Kind: events.ActorSystem})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "state": req.State})
	case errors.Is(err, store.ErrNoEnquiry):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("advancing an enquiry", "enquiry", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not advance the enquiry")
	}
}

// convertRequest is what the manager confirms at conversion (#142). The name
// and phone are typed here because the funnel deliberately never held them.
type convertRequest struct {
	StartOn     string `json:"start_on"`
	EndOn       string `json:"end_on"`
	TenantName  string `json:"tenant_name"`
	TenantPhone string `json:"tenant_phone"`
	TenantEmail string `json:"tenant_email"`
	RentMinor   int64  `json:"rent_minor"`
}

// Convert turns an engaged enquiry into a lease draft with everything carried
// over, and takes the listing off the market.
func (h *Listings) Convert(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req convertRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	start, err := effective.ParseDate(req.StartOn)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "start_on must be a date, as YYYY-MM-DD")
		return
	}
	c := service.Conversion{
		Start: start, TenantName: req.TenantName, TenantPhone: req.TenantPhone,
		TenantEmail: req.TenantEmail, RentMinor: req.RentMinor,
	}
	if req.EndOn != "" {
		if c.End, err = effective.ParseDate(req.EndOn); err != nil {
			writeError(w, http.StatusUnprocessableEntity, "end_on must be a date, as YYYY-MM-DD")
			return
		}
	}

	leaseID, err := h.enquiries.Convert(r.Context(), id, c, events.Actor{Kind: events.ActorSystem})
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{
			"enquiry_id": id, "lease_id": leaseID, "lease_state": "draft"})
	case errors.Is(err, service.ErrNotConvertible):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNoEnquiry), errors.Is(err, store.ErrNoListing):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		// The lease module's own refusals (a draft that will not validate) come
		// through as 422 with its message.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request: "+err.Error())
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
