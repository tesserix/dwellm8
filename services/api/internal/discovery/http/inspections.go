package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Inspections handles the viewing loop, both sides.
type Inspections struct {
	inspections *service.Inspections
	log         *slog.Logger
}

// NewInspections builds the handler.
func NewInspections(s *service.Inspections, log *slog.Logger) *Inspections {
	return &Inspections{inspections: s, log: log}
}

// OwnerRoutes mounts the authenticated side.
func (h *Inspections) OwnerRoutes(r *authz.Registrar) {
	r.Handle("POST /v1/listings/{id}/slots", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("listing", "id")}, h.PublishSlot)
	r.Handle("GET /v1/listings/{id}/slots", authz.Check{
		Relation: "can_view", Object: authz.PathObject("listing", "id")}, h.OwnerSlots)
	r.Handle("GET /v1/listings/{id}/feedback", authz.Check{
		Relation: "can_view", Object: authz.PathObject("listing", "id")}, h.Feedback)
	r.Handle("POST /v1/slots/{id}/close", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.CloseSlot)
	r.Handle("GET /v1/inspections", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.DayView)
	r.Handle("POST /v1/enquiries/{id}/outcome", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.Outcome)
	r.Handle("POST /v1/enquiries/{id}/cancel", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.OwnerCancel)

	// The request loop (#331).
	r.Handle("GET /v1/viewing-requests", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.OwnerRequests)
	r.Handle("POST /v1/enquiries/{id}/accept", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.AcceptRequest)
	r.Handle("POST /v1/enquiries/{id}/counter", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.CounterRequest)
	r.Handle("POST /v1/enquiries/{id}/decline", authz.Check{
		Relation: "can_respond", Object: authz.PathObject("listing_enquiry", "id")}, h.DeclineRequest)
}

// PublicRoutes mounts the prospect side, Open for the funnel's reason.
func (h *Inspections) PublicRoutes(r *authz.Registrar) {
	r.Open("GET /v1/public/listings/{id}/slots", openReason, h.OpenSlots)
	r.Open("POST /v1/public/inspections", openReason, h.Book)
	r.Open("POST /v1/public/inspections/{id}/reschedule", openReason, h.Reschedule)
	r.Open("POST /v1/public/inspections/{id}/cancel", openReason, h.ProspectCancel)
	r.Open("POST /v1/public/viewing-requests", openReason, h.RequestViewing)
	r.Open("GET /v1/public/viewing-requests", openReason, h.ProspectRequests)
	r.Open("POST /v1/public/viewing-requests/{id}/accept", openReason, h.AcceptCounter)
}

// slotResponse shows a time and how many places remain — never who booked.
type slotResponse struct {
	ID           string `json:"id"`
	StartsAt     string `json:"starts_at"`
	DurationMins int    `json:"duration_mins"`
	Remaining    int    `json:"remaining"`
	AssignedTo   string `json:"assigned_to,omitempty"`
	MeetingPoint string `json:"meeting_point,omitempty"`
	State        string `json:"state,omitempty"`
	// Owner only: how many people the manager would be turning away by moving
	// or cancelling this one. A prospect sees places left, never the count.
	Capacity int `json:"capacity,omitempty"`
	Booked   int `json:"booked,omitempty"`
	// The clock this viewing happens on, so the manager's phone shows the hour
	// the person at the door will arrive rather than its own (#334).
	Zone string `json:"zone,omitempty"`
}

func toSlot(s store.Slot, ownerView bool) slotResponse {
	out := slotResponse{
		ID: s.ID, StartsAt: s.StartsAt.UTC().Format(time.RFC3339),
		DurationMins: s.DurationMins, Remaining: s.Capacity - s.Booked,
	}
	if ownerView {
		out.AssignedTo = s.AssignedTo
		out.MeetingPoint = s.MeetingPoint
		out.State = s.State
		out.Capacity = s.Capacity
		out.Booked = s.Booked
		out.Zone = s.Zone
	}
	return out
}

func toSlots(list []store.Slot, ownerView bool) []slotResponse {
	out := make([]slotResponse, 0, len(list))
	for _, s := range list {
		out = append(out, toSlot(s, ownerView))
	}
	return out
}

// PublishSlot offers a viewing time.
func (h *Inspections) PublishSlot(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartsAt     string `json:"starts_at"`
		DurationMins int    `json:"duration_mins"`
		Capacity     int    `json:"capacity"`
		AssignedTo   string `json:"assigned_to"`
		MeetingPoint string `json:"meeting_point"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	starts, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "starts_at must be RFC 3339")
		return
	}
	id, err := h.inspections.PublishSlot(r.Context(), r.PathValue("id"), store.SlotDraft{
		StartsAt: starts, DurationMins: req.DurationMins, Capacity: req.Capacity,
		AssignedTo: req.AssignedTo, MeetingPoint: req.MeetingPoint,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
	case errors.Is(err, store.ErrSlotClash):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNoListing):
		writeError(w, http.StatusNotFound, "no such listing")
	case errors.Is(err, store.ErrNoSlot):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("publishing a slot", "error", err)
		writeError(w, http.StatusInternalServerError, "could not publish the slot")
	}
}

// OwnerSlots is the listing's calendar with fill.
func (h *Inspections) OwnerSlots(w http.ResponseWriter, r *http.Request) {
	list, err := h.inspections.OwnerSlots(r.Context(), r.PathValue("id"))
	if err != nil {
		h.respondListError(w, err, "reading slots")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": toSlots(list, true)})
}

// CloseSlot stops new bookings.
func (h *Inspections) CloseSlot(w http.ResponseWriter, r *http.Request) {
	err := h.inspections.CloseSlot(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "state": "closed"})
	case errors.Is(err, store.ErrNoSlot):
		writeError(w, http.StatusNotFound, "no such open slot")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("closing a slot", "error", err)
		writeError(w, http.StatusInternalServerError, "could not close the slot")
	}
}

// DayView is the day's viewings, in order — what the person attending sees.
func (h *Inspections) DayView(w http.ResponseWriter, r *http.Request) {
	on := time.Now()
	if v := r.URL.Query().Get("on"); v != "" {
		var err error
		if on, err = time.Parse("2006-01-02", v); err != nil {
			writeError(w, http.StatusBadRequest, "on must be a date, as YYYY-MM-DD")
			return
		}
	} else {
		on = time.Date(on.Year(), on.Month(), on.Day(), 0, 0, 0, 0, on.Location())
	}
	list, err := h.inspections.DayView(r.Context(), on)
	if err != nil {
		h.respondListError(w, err, "reading the day view")
		return
	}
	type visit struct {
		enquiryResponse
		StartsAt string `json:"starts_at"`
	}
	out := make([]visit, 0, len(list))
	for _, b := range list {
		out = append(out, visit{toEnquiryResponse(b.Enquiry), b.StartsAt.UTC().Format(time.RFC3339)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"inspections": out})
}

// Outcome records what happened at the viewing (#141).
func (h *Inspections) Outcome(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Outcome    string   `json:"outcome"`
		Objections []string `json:"objections"`
		Note       string   `json:"note"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	err := h.inspections.RecordOutcome(r.Context(), r.PathValue("id"), store.Outcome{
		Outcome: req.Outcome, Objections: req.Objections, Note: req.Note,
	}, events.Actor{Kind: events.ActorSystem})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{
			"id": r.PathValue("id"), "state": "completed", "outcome": req.Outcome})
	case errors.Is(err, service.ErrBadOutcome), errors.Is(err, service.ErrBadObjection):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, store.ErrNotReschedulable):
		writeError(w, http.StatusConflict, "the viewing is not open to an outcome")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("recording an outcome", "error", err)
		writeError(w, http.StatusInternalServerError, "could not record the outcome")
	}
}

// OwnerCancel calls a viewing off from the owner's side.
func (h *Inspections) OwnerCancel(w http.ResponseWriter, r *http.Request) {
	err := h.inspections.CancelByOwner(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{
			"id": r.PathValue("id"), "state": "closed", "outcome": "cancelled_by_owner"})
	case errors.Is(err, store.ErrNotReschedulable):
		writeError(w, http.StatusConflict, "no scheduled viewing to cancel")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("cancelling a viewing", "error", err)
		writeError(w, http.StatusInternalServerError, "could not cancel the viewing")
	}
}

// Feedback is the anonymised aggregate on a listing (#141).
func (h *Inspections) Feedback(w http.ResponseWriter, r *http.Request) {
	f, err := h.inspections.ListingFeedback(r.Context(), r.PathValue("id"))
	if err != nil {
		h.respondListError(w, err, "reading feedback")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"concluded": f.Completed, "interested": f.Interested, "no_shows": f.NoShows,
		"objections": f.Objections,
	})
}

// OpenSlots lists a live listing's bookable times.
func (h *Inspections) OpenSlots(w http.ResponseWriter, r *http.Request) {
	list, err := h.inspections.OpenSlots(r.Context(), r.PathValue("id"))
	if err != nil {
		h.log.Error("listing open slots", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the slots")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"slots": toSlots(list, false)})
}

// Book holds a place. A full slot answers 409 with the adjacent alternatives
// rather than dropping the prospect (#139's race).
func (h *Inspections) Book(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListingID string `json:"listing_id"`
		SlotID    string `json:"slot_id"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	booked, alts, err := h.inspections.Book(r.Context(), token(r), req.ListingID, req.SlotID)
	h.respondBooking(w, booked, alts, err)
}

// Reschedule moves the booking, keeping the enquiry's context.
func (h *Inspections) Reschedule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SlotID string `json:"slot_id"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	booked, alts, err := h.inspections.Reschedule(r.Context(), token(r), r.PathValue("id"), req.SlotID)
	h.respondBooking(w, booked, alts, err)
}

// ProspectCancel releases the place.
func (h *Inspections) ProspectCancel(w http.ResponseWriter, r *http.Request) {
	err := h.inspections.CancelByProspect(r.Context(), token(r), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{
			"id": r.PathValue("id"), "state": "closed", "outcome": "cancelled_by_prospect"})
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	case errors.Is(err, store.ErrNotReschedulable):
		writeError(w, http.StatusConflict, "no scheduled viewing to cancel")
	default:
		h.log.Error("cancelling a viewing", "error", err)
		writeError(w, http.StatusInternalServerError, "could not cancel the viewing")
	}
}

func (h *Inspections) respondBooking(w http.ResponseWriter, b store.Booked, alts []store.Slot, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": b.Enquiry.ID, "state": b.Enquiry.State,
			"starts_at":     b.StartsAt.UTC().Format(time.RFC3339),
			"meeting_point": b.MeetingPoint,
		})
	case errors.Is(err, store.ErrSlotFull):
		// The race's loser: refused by name, with the next open times attached.
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":        "that slot has just filled — here are the next open times",
			"alternatives": toSlots(alts, false),
		})
	case errors.Is(err, store.ErrNotVerified):
		writeError(w, http.StatusForbidden, "verify a phone number before booking a viewing")
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	case errors.Is(err, store.ErrListingNotLive), errors.Is(err, store.ErrNoListing):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "this listing is no longer available — search for similar homes in the same locality"})
	case errors.Is(err, store.ErrNotReschedulable):
		writeError(w, http.StatusConflict, "the viewing is not open to change")
	default:
		h.log.Error("booking a viewing", "error", err)
		writeError(w, http.StatusInternalServerError, "could not book the viewing")
	}
}

func (h *Inspections) respondListError(w http.ResponseWriter, err error, what string) {
	if errors.Is(err, tenancy.ErrNoTenant) {
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
		return
	}
	h.log.Error(what, "error", err)
	writeError(w, http.StatusInternalServerError, "could not read the data")
}
