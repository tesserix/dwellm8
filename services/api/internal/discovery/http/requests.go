package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Viewing requests (#331), both sides: what a prospect asks for when the
// published times do not suit, and the manager's answer to it.

type proposalResponse struct {
	ID         string `json:"id"`
	ProposedBy string `json:"proposed_by"`
	StartsAt   string `json:"starts_at"`
	State      string `json:"state"`
}

type requestResponse struct {
	ID       string             `json:"id"`
	Kind     string             `json:"kind"`
	State    string             `json:"state"`
	Headline string             `json:"headline,omitempty"`
	Message  string             `json:"message,omitempty"`
	Outcome  string             `json:"outcome,omitempty"`
	Times    []proposalResponse `json:"times"`
	// Disclosed on confirmation only, by the store.
	MeetingPoint string `json:"meeting_point,omitempty"`
	MeetingLink  string `json:"meeting_link,omitempty"`
	ScheduledFor string `json:"scheduled_for,omitempty"`
	ListingID    string `json:"listing_id"`
	CreatedAt    string `json:"created_at"`
}

func toRequest(r store.Request) requestResponse {
	out := requestResponse{
		ID: r.Enquiry.ID, Kind: r.Enquiry.Kind, State: r.Enquiry.State,
		Headline: r.Enquiry.Headline, Message: r.Enquiry.Message, Outcome: r.Outcome,
		MeetingPoint: r.MeetingPoint, MeetingLink: r.MeetingLink,
		ListingID: r.Enquiry.ListingID,
		CreatedAt: r.Enquiry.CreatedAt.UTC().Format(time.RFC3339),
		Times:     make([]proposalResponse, 0, len(r.Proposals)),
	}
	if r.Enquiry.ScheduledFor != nil {
		out.ScheduledFor = r.Enquiry.ScheduledFor.UTC().Format(time.RFC3339)
	}
	for _, p := range r.Proposals {
		out.Times = append(out.Times, proposalResponse{
			ID: p.ID, ProposedBy: p.ProposedBy, State: p.State,
			StartsAt: p.StartsAt.UTC().Format(time.RFC3339),
		})
	}
	return out
}

func toRequests(list []store.Request) []requestResponse {
	out := make([]requestResponse, 0, len(list))
	for _, r := range list {
		out = append(out, toRequest(r))
	}
	return out
}

// RequestViewing asks for a private or online viewing at the prospect's times.
func (h *Inspections) RequestViewing(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListingID string   `json:"listing_id"`
		Kind      string   `json:"kind"`
		Message   string   `json:"message"`
		Times     []string `json:"times"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	at := make([]time.Time, 0, len(req.Times))
	for _, s := range req.Times {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "times must be RFC 3339")
			return
		}
		at = append(at, t)
	}
	out, err := h.inspections.RequestViewing(r.Context(), token(r), req.ListingID,
		req.Kind, req.Message, at)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, toRequest(out))
	case errors.Is(err, store.ErrPastTime), errors.Is(err, store.ErrNotARequest):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, store.ErrTooManyRequests):
		writeError(w, http.StatusConflict, err.Error())
	default:
		h.respondRequestError(w, err, "requesting a viewing")
	}
}

// ProspectRequests is the prospect's own requests and the answers to them.
func (h *Inspections) ProspectRequests(w http.ResponseWriter, r *http.Request) {
	list, err := h.inspections.ProspectRequests(r.Context(), token(r))
	if err != nil {
		h.respondRequestError(w, err, "reading viewing requests")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": toRequests(list)})
}

// AcceptCounter takes the time the manager offered back.
func (h *Inspections) AcceptCounter(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProposalID string `json:"proposal_id"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	b, err := h.inspections.AcceptCounter(r.Context(), token(r), r.PathValue("id"), req.ProposalID)
	h.respondConfirmation(w, b, err)
}

// OwnerRequests is the manager's queue of requests still to answer.
func (h *Inspections) OwnerRequests(w http.ResponseWriter, r *http.Request) {
	list, err := h.inspections.OwnerRequests(r.Context())
	if err != nil {
		h.respondListError(w, err, "reading viewing requests")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"requests": toRequests(list)})
}

// AcceptRequest confirms one of the times the prospect proposed.
func (h *Inspections) AcceptRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProposalID   string `json:"proposal_id"`
		MeetingPoint string `json:"meeting_point"`
		MeetingLink  string `json:"meeting_link"`
		AssignedTo   string `json:"assigned_to"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	b, err := h.inspections.AcceptRequest(r.Context(), r.PathValue("id"), req.ProposalID,
		store.Confirmation{MeetingPoint: req.MeetingPoint, MeetingLink: req.MeetingLink,
			AssignedTo: req.AssignedTo}, events.Actor{Kind: events.ActorSystem})
	h.respondConfirmation(w, b, err)
}

// CounterRequest offers a time of the manager's own, once.
func (h *Inspections) CounterRequest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartsAt     string `json:"starts_at"`
		MeetingPoint string `json:"meeting_point"`
		MeetingLink  string `json:"meeting_link"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	at, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "starts_at must be RFC 3339")
		return
	}
	p, err := h.inspections.CounterRequest(r.Context(), r.PathValue("id"), at,
		store.Confirmation{MeetingPoint: req.MeetingPoint, MeetingLink: req.MeetingLink},
		events.Actor{Kind: events.ActorSystem})
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, proposalResponse{
			ID: p.ID, ProposedBy: p.ProposedBy, State: p.State,
			StartsAt: p.StartsAt.UTC().Format(time.RFC3339)})
	default:
		h.respondRequestError(w, err, "countering a viewing request")
	}
}

// DeclineRequest closes the request with a reason the prospect is told.
func (h *Inspections) DeclineRequest(w http.ResponseWriter, r *http.Request) {
	err := h.inspections.DeclineRequest(r.Context(), r.PathValue("id"),
		events.Actor{Kind: events.ActorSystem})
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{
			"id": r.PathValue("id"), "state": "closed", "outcome": "declined_by_owner"})
	default:
		h.respondRequestError(w, err, "declining a viewing request")
	}
}

func (h *Inspections) respondConfirmation(w http.ResponseWriter, b store.Booked, err error) {
	if err == nil {
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": b.Enquiry.ID, "state": b.Enquiry.State,
			"starts_at":     b.StartsAt.UTC().Format(time.RFC3339),
			"meeting_point": b.MeetingPoint,
			"meeting_link":  b.MeetingLink,
		})
		return
	}
	h.respondRequestError(w, err, "confirming a viewing request")
}

func (h *Inspections) respondRequestError(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	case errors.Is(err, store.ErrNotVerified):
		writeError(w, http.StatusForbidden, "verify a phone number before asking for a viewing")
	case errors.Is(err, store.ErrNotAnswerable):
		writeError(w, http.StatusConflict, "that request has already been answered")
	case errors.Is(err, store.ErrNoProposal):
		writeError(w, http.StatusNotFound, "no such open proposed time")
	case errors.Is(err, store.ErrPastTime), errors.Is(err, service.ErrNoMeetingPlace):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, store.ErrSlotClash):
		writeError(w, http.StatusConflict, "another viewing is already booked at that time")
	case errors.Is(err, store.ErrListingNotLive), errors.Is(err, store.ErrNoListing):
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "this listing is no longer available — search for similar homes in the same locality"})
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "could not answer the viewing request")
	}
}
