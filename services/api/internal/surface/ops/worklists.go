package ops

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	communityservice "github.com/tesserix/dwellm8/services/api/internal/community/service"
	maintenanceservice "github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// maxBody bounds a worklist request; an action on a ticket is a few hundred bytes.
const maxBody = 16 << 10

const worklistLimit = 200

// WithTickets adds the maintenance tickets seam (#237, ops leg).
func (h *Handler) WithTickets(t *maintenanceservice.Tickets) *Handler {
	h.tickets = t
	return h
}

// WithCommunity adds the conversation and the gate (#238, ops leg).
func (h *Handler) WithCommunity(c *communityservice.Community) *Handler {
	h.community = c
	return h
}

// WorklistRoutes mounts the manager's side of tickets, messages and the gate.
// Reads are can_view; acting on a ticket, replying, or deciding at the gate is
// can_operate — doing the work is what staff are for, per the checklist routes'
// same split.
func (h *Handler) WorklistRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/tickets", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Tickets)
	r.Handle("GET /v1/ops/tickets/{id}", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Ticket)
	r.Handle("POST /v1/ops/tickets/{id}/advance", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.AdvanceTicket)
	r.Handle("GET /v1/ops/threads", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Threads)
	r.Handle("GET /v1/ops/tenancies/{lease}/messages", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Messages)
	r.Handle("POST /v1/ops/tenancies/{lease}/messages", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.SendMessage)
	r.Handle("GET /v1/ops/passes", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Passes)
	r.Handle("POST /v1/ops/passes/{id}/state", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.SetPassState)
}

type ticketResponse struct {
	TicketID        string                `json:"ticket_id"`
	LeaseID         string                `json:"lease_id"`
	Unit            string                `json:"unit,omitempty"`
	Property        string                `json:"property,omitempty"`
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
		TicketID: t.ID, LeaseID: t.LeaseID, Unit: t.UnitCode, Property: t.PropertyName,
		Category: string(t.Category), Title: t.Title, Body: t.Body,
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

// Tickets is the firm's maintenance worklist. ?settled=true reads history.
func (h *Handler) Tickets(w http.ResponseWriter, r *http.Request) {
	if h.tickets == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	settled := r.URL.Query().Get("settled") == "true"
	list, err := h.tickets.ForOrg(r.Context(), settled, worklistLimit)
	if err != nil {
		h.log.Error("listing tickets", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the worklist")
		return
	}
	out := make([]ticketResponse, 0, len(list))
	for _, t := range list {
		out = append(out, presentTicket(t))
	}
	writeJSON(w, http.StatusOK, map[string]any{"tickets": out})
}

// Ticket is one request and its timeline.
func (h *Handler) Ticket(w http.ResponseWriter, r *http.Request) {
	if h.tickets == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	t, err := h.tickets.Read(r.Context(), r.PathValue("id"))
	if errors.Is(err, maintenanceservice.ErrNoTicket) {
		writeError(w, http.StatusNotFound, "no such ticket")
		return
	}
	if err != nil {
		h.log.Error("reading a ticket", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the ticket")
		return
	}
	writeJSON(w, http.StatusOK, presentTicket(t))
}

type advanceRequest struct {
	Action          string `json:"action"`
	Slot            string `json:"slot"`
	Vendor          string `json:"vendor"`
	Liability       string `json:"liability"`
	LiabilityReason string `json:"liability_reason"`
	CostMinor       *int64 `json:"cost_minor"`
	Note            string `json:"note"`
}

// AdvanceTicket applies one manager action — acknowledge, schedule, assess,
// start, resolve or cancel — and answers with the ticket as it now stands.
func (h *Handler) AdvanceTicket(w http.ResponseWriter, r *http.Request) {
	if h.tickets == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req advanceRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	t, err := h.tickets.Advance(r.Context(), r.PathValue("id"),
		maintenanceservice.TicketAction(req.Action), maintenanceservice.TicketPatch{
			Slot: req.Slot, Vendor: req.Vendor,
			Liability: req.Liability, LiabilityReason: req.LiabilityReason,
			CostMinor: req.CostMinor, Note: req.Note,
		})
	switch {
	case errors.Is(err, maintenanceservice.ErrNoTicket):
		writeError(w, http.StatusNotFound, "no such ticket")
		return
	case errors.Is(err, maintenanceservice.ErrTicket):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	case err != nil:
		h.log.Error("advancing a ticket", "ticket", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not advance the ticket")
		return
	}
	writeJSON(w, http.StatusOK, presentTicket(t))
}

type threadResponse struct {
	LeaseID    string `json:"lease_id"`
	Unit       string `json:"unit"`
	Property   string `json:"property"`
	LastBody   string `json:"last_body"`
	LastSender string `json:"last_sender"`
	LastAt     string `json:"last_at"`
	Messages   int    `json:"messages"`
}

// Threads is the manager's inbox: one row per tenancy with a conversation.
func (h *Handler) Threads(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	list, err := h.community.Threads(r.Context(), worklistLimit)
	if err != nil {
		h.log.Error("listing threads", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the inbox")
		return
	}
	out := make([]threadResponse, 0, len(list))
	for _, t := range list {
		out = append(out, threadResponse{
			LeaseID: t.LeaseID, Unit: t.UnitCode, Property: t.PropertyName,
			LastBody: t.LastBody, LastSender: t.LastSender,
			LastAt: t.LastAt.Format(time.RFC3339), Messages: t.Messages,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": out})
}

type messageResponse struct {
	MessageID string `json:"message_id"`
	Sender    string `json:"sender"`
	Body      string `json:"body"`
	SentAt    string `json:"sent_at"`
}

// Messages is one tenancy's conversation, oldest first.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	thread, err := h.community.Thread(r.Context(), r.PathValue("lease"), worklistLimit)
	if err != nil {
		h.log.Error("reading a thread", "lease", r.PathValue("lease"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the conversation")
		return
	}
	out := make([]messageResponse, 0, len(thread))
	for _, m := range thread {
		out = append(out, messageResponse{
			MessageID: m.ID, Sender: m.Sender, Body: m.Body,
			SentAt: m.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"lease_id": r.PathValue("lease"), "messages": out})
}

type sendRequest struct {
	Body string `json:"body"`
}

// SendMessage appends the manager's reply. Property and unit come from the
// lease, never the request.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req sendRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	leaseID := r.PathValue("lease")
	tenant, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	t, err := h.leases.Tenancy(r.Context(), leaseID, effective.DateOf(h.now(), ist))
	if err != nil {
		writeError(w, http.StatusNotFound, "no such tenancy")
		return
	}
	m, err := h.community.Send(r.Context(), communityservice.Message{
		TenantID: tenant.String(), LeaseID: leaseID,
		PropertyID: t.PropertyID, UnitID: t.UnitID,
		Sender: "manager", Body: req.Body,
	})
	if errors.Is(err, communityservice.ErrMessage) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		h.log.Error("sending a reply", "lease", leaseID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not send the reply")
		return
	}
	writeJSON(w, http.StatusCreated, messageResponse{
		MessageID: m.ID, Sender: m.Sender, Body: m.Body,
		SentAt: m.CreatedAt.Format(time.RFC3339),
	})
}

type passResponse struct {
	PassID    string `json:"pass_id"`
	LeaseID   string `json:"lease_id"`
	Unit      string `json:"unit,omitempty"`
	Property  string `json:"property,omitempty"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Code      string `json:"code"`
	State     string `json:"state"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to,omitempty"`
}

func presentPass(p communityservice.Pass) passResponse {
	out := passResponse{
		PassID: p.ID, LeaseID: p.LeaseID, Unit: p.UnitCode, Property: p.PropertyName,
		Name: p.Name, Kind: string(p.Kind), Code: p.Code, State: p.State,
		ValidFrom: p.ValidFrom.Format(time.RFC3339),
	}
	if !p.ValidTo.IsZero() {
		out.ValidTo = p.ValidTo.Format(time.RFC3339)
	}
	return out
}

// Passes is the gate's worklist.
func (h *Handler) Passes(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	list, err := h.community.PassesForOrg(r.Context(), worklistLimit)
	if err != nil {
		h.log.Error("listing gate passes", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the gate")
		return
	}
	out := make([]passResponse, 0, len(list))
	for _, p := range list {
		out = append(out, presentPass(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"passes": out})
}

type passStateRequest struct {
	State string `json:"state"`
}

// SetPassState records what happened at the gate: arrived, inside, left, denied.
func (h *Handler) SetPassState(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req passStateRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	p, err := h.community.SetPassState(r.Context(), r.PathValue("id"), req.State)
	switch {
	case errors.Is(err, communityservice.ErrNoPass):
		writeError(w, http.StatusNotFound, "no such gate pass")
		return
	case errors.Is(err, communityservice.ErrPass):
		writeError(w, http.StatusUnprocessableEntity, "state is arrived, inside, left or denied")
		return
	case err != nil:
		h.log.Error("setting a pass state", "pass", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not record the gate decision")
		return
	}
	writeJSON(w, http.StatusOK, presentPass(p))
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	return decodeUpTo(w, r, v, maxBody)
}

// decodeUpTo is decode with a stated ceiling, for the one request that carries
// a photograph rather than a few hundred bytes of ticket.
func decodeUpTo(w http.ResponseWriter, r *http.Request, v any, limit int) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, int64(limit)))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request: "+err.Error())
		return err
	}
	return nil
}
