package resident

import (
	"errors"
	"net/http"
	"time"

	maintenanceservice "github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// ticketResponse is one ticket as the renter sees it.
type ticketResponse struct {
	TicketID        string                `json:"ticket_id"`
	Category        string                `json:"category"`
	Title           string                `json:"title"`
	Body            string                `json:"body,omitempty"`
	Status          string                `json:"status"`
	Liability       string                `json:"liability,omitempty"`
	LiabilityReason string                `json:"liability_reason,omitempty"`
	Slot            string                `json:"slot,omitempty"`
	Vendor          string                `json:"vendor,omitempty"`
	CostMinor       *int64                `json:"cost_minor,omitempty"`
	RaisedAt        string                `json:"raised_at"`
	Timeline        []ticketEventResponse `json:"timeline,omitempty"`
}

type ticketEventResponse struct {
	At    string `json:"at"`
	Actor string `json:"actor"`
	Body  string `json:"body"`
}

func presentTicket(t maintenanceservice.Ticket) ticketResponse {
	out := ticketResponse{
		TicketID: t.ID, Category: string(t.Category), Title: t.Title, Body: t.Body,
		Status: string(t.Status), Liability: t.Liability, LiabilityReason: t.LiabilityReason,
		Slot: t.Slot, Vendor: t.Vendor, CostMinor: t.CostMinor,
		RaisedAt: t.CreatedAt.Format(time.RFC3339),
	}
	for _, e := range t.Events {
		out.Timeline = append(out.Timeline, ticketEventResponse{
			At: e.At.Format(time.RFC3339), Actor: e.Actor, Body: e.Body,
		})
	}
	return out
}

// Tickets lists the tenancy's repair requests.
func (h *Handler) Tickets(w http.ResponseWriter, r *http.Request) {
	if h.tickets == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)

	list, err := h.tickets.ForLease(ctx, res.LeaseID, historyLimit)
	if err != nil {
		h.fail(w, "listing tickets", res.LeaseID, err)
		return
	}
	out := make([]ticketResponse, 0, len(list))
	for _, t := range list {
		out = append(out, presentTicket(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"lease_id": res.LeaseID, "tickets": out})
}

// Ticket is one request and its timeline.
func (h *Handler) Ticket(w http.ResponseWriter, r *http.Request) {
	if h.tickets == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)

	t, err := h.tickets.Read(ctx, r.PathValue("ticket"))
	if errors.Is(err, maintenanceservice.ErrNoTicket) {
		writeError(w, http.StatusNotFound, "no such ticket")
		return
	}
	if err != nil {
		h.fail(w, "reading a ticket", res.LeaseID, err)
		return
	}
	// A ticket reached through one tenancy must be a ticket of that tenancy —
	// the policy allows the renter's other lease; the URL must not.
	if t.LeaseID != res.LeaseID {
		writeError(w, http.StatusNotFound, "no such ticket")
		return
	}
	writeJSON(w, http.StatusOK, presentTicket(t))
}

type raiseRequest struct {
	Category string `json:"category"`
	Title    string `json:"title"`
	Body     string `json:"body"`
}

// RaiseTicket records a repair request against the tenancy.
//
// Property and unit come from the lease the session resolved, never from the
// body — a ticket that could name its own unit could be filed against anybody's.
func (h *Handler) RaiseTicket(w http.ResponseWriter, r *http.Request) {
	if h.tickets == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	var req raiseRequest
	if err := decode(w, r, &req); err != nil {
		return
	}

	ctx := session.Scope(r.Context(), res)
	t, err := h.leases.Tenancy(ctx, res.LeaseID, effective.DateOf(h.now(), ist))
	if err != nil {
		h.fail(w, "reading a tenancy before raising a ticket", res.LeaseID, err)
		return
	}

	out, err := h.tickets.Raise(ctx, maintenanceservice.Ticket{
		TenantID: res.TenantID.String(), LeaseID: res.LeaseID,
		PropertyID: t.PropertyID, UnitID: t.UnitID,
		RaisedBy: session.PartyID,
		Category: maintenanceservice.TicketCategory(req.Category),
		Title:    req.Title, Body: req.Body,
	})
	if errors.Is(err, maintenanceservice.ErrTicket) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		h.fail(w, "raising a ticket", res.LeaseID, err)
		return
	}
	writeJSON(w, http.StatusCreated, presentTicket(out))
}
