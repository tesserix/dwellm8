// Package http is the maintenance module's HTTP surface.
//
// One action fires a whole process, and the two settlement routes are separate from
// it because they are separate acts with separate refusals: firing can fail for want
// of a template, settling can fail because a step this one waits on is not done, and
// a single endpoint would have to report both through one response.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// maxBody is a ceiling on a checklist request. Firing one is a few hundred bytes;
// anything near this is not one.
const maxBody = 32 << 10

// Checklists handles the checklist routes.
type Checklists struct {
	checklists *service.Checklists
	log        *slog.Logger
}

// NewChecklists builds the handler.
func NewChecklists(s *service.Checklists, log *slog.Logger) *Checklists {
	return &Checklists{checklists: s, log: log}
}

// Routes mounts the module. Each route names its ADR-0020 check.
//
// Firing and abandoning are can_manage on the unit: starting a move-out commits the
// organisation to a process that will refuse to let the tenancy close, which is more
// than day-to-day operation. Settling a task is can_operate, because doing the work
// is what staff and field agents are for.
func (h *Checklists) Routes(r *authz.Registrar) {
	r.Handle("POST /v1/checklists", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.Start)
	r.Handle("GET /v1/checklists", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Portfolio)
	r.Handle("GET /v1/checklists/{id}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("checklist", "id")}, h.Read)
	r.Handle("POST /v1/checklists/{id}/steps/{step}/complete", authz.Check{
		Relation: "can_operate", Object: authz.PathObject("checklist", "id")}, h.Complete)
	r.Handle("POST /v1/checklists/{id}/steps/{step}/skip", authz.Check{
		Relation: "can_operate", Object: authz.PathObject("checklist", "id")}, h.Skip)
	r.Handle("POST /v1/checklists/{id}/finish", authz.Check{
		Relation: "can_operate", Object: authz.PathObject("checklist", "id")}, h.Finish)
	r.Handle("POST /v1/checklists/{id}/abandon", authz.Check{
		Relation: "can_manage", Object: authz.PathObject("checklist", "id")}, h.Abandon)
}

type startRequest struct {
	Process string `json:"process"`
	// PropertyKind is what the template is resolved against. The caller knows it,
	// and sending it saves a read of a row the caller already has.
	PropertyKind string `json:"property_kind"`
	PropertyID   string `json:"property_id"`
	UnitID       string `json:"unit_id"`
	LeaseID      string `json:"lease_id"`
	// AnchorOn is the date the steps are measured from. Required: a checklist dated
	// from today is right on the day it is fired and wrong every day after.
	AnchorOn string `json:"anchor_on"`
}

type taskResponse struct {
	ID        string   `json:"id"`
	StepCode  string   `json:"step_code"`
	Title     string   `json:"title"`
	Position  int      `json:"position"`
	Blocking  bool     `json:"blocking"`
	Owner     string   `json:"owner_role"`
	Assignee  string   `json:"assignee_party_id,omitempty"`
	DueOn     string   `json:"due_on"`
	State     string   `json:"state"`
	DependsOn []string `json:"depends_on,omitempty"`
}

type checklistResponse struct {
	ID              string `json:"id"`
	Process         string `json:"process"`
	TemplateID      string `json:"template_id"`
	TemplateVersion int    `json:"template_version"`
	PropertyID      string `json:"property_id"`
	UnitID          string `json:"unit_id,omitempty"`
	LeaseID         string `json:"lease_id,omitempty"`
	AnchorOn        string `json:"anchor_on"`
	State           string `json:"state"`
	// Outstanding is the blocking work, repeated out of the task list so a client
	// can render "what stops this closing" without filtering.
	Outstanding []taskResponse `json:"blocking_outstanding"`
	Tasks       []taskResponse `json:"tasks"`
}

func render(c service.Checklist) checklistResponse {
	out := checklistResponse{
		ID: c.ID, Process: string(c.Process),
		TemplateID: c.TemplateID, TemplateVersion: c.TemplateVersion,
		PropertyID: c.PropertyID, UnitID: c.UnitID, LeaseID: c.LeaseID,
		AnchorOn: c.AnchorOn.String(), State: string(c.State),
		Tasks: make([]taskResponse, 0, len(c.Tasks)),
	}
	for _, t := range c.Tasks {
		out.Tasks = append(out.Tasks, renderTask(t))
	}
	for _, t := range c.Outstanding() {
		out.Outstanding = append(out.Outstanding, renderTask(t))
	}
	return out
}

func renderTask(t service.Task) taskResponse {
	return taskResponse{
		ID: t.ID, StepCode: t.StepCode, Title: t.Title, Position: t.Position,
		Blocking: t.Blocking, Owner: string(t.Owner), Assignee: t.Assignee,
		DueOn: t.DueOn.String(), State: string(t.State), DependsOn: t.DependsOn,
	}
}

// Start fires a checklist.
func (h *Checklists) Start(w http.ResponseWriter, r *http.Request) {
	var req startRequest
	if !decode(w, r, &req) {
		return
	}

	anchor, err := effective.ParseDate(req.AnchorOn)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity,
			"send anchor_on as YYYY-MM-DD — it is the date every step is measured from")
		return
	}

	c, err := h.checklists.Start(r.Context(),
		domain.Process(req.Process), domain.PropertyKind(req.PropertyKind),
		domain.Subject{PropertyID: req.PropertyID, UnitID: req.UnitID, LeaseID: req.LeaseID},
		anchor, actingParty(r))
	h.respond(w, r, c, err, http.StatusCreated)
}

// Read returns one checklist.
func (h *Checklists) Read(w http.ResponseWriter, r *http.Request) {
	c, err := h.checklists.Read(r.Context(), r.PathValue("id"))
	h.respond(w, r, c, err, http.StatusOK)
}

// Complete settles one step.
func (h *Checklists) Complete(w http.ResponseWriter, r *http.Request) {
	c, err := h.checklists.Complete(r.Context(),
		r.PathValue("id"), r.PathValue("step"), actingParty(r))
	h.respond(w, r, c, err, http.StatusOK)
}

type skipRequest struct {
	Reason string `json:"reason"`
}

// Skip settles a non-blocking step with a reason.
func (h *Checklists) Skip(w http.ResponseWriter, r *http.Request) {
	var req skipRequest
	if !decode(w, r, &req) {
		return
	}
	c, err := h.checklists.Skip(r.Context(),
		r.PathValue("id"), r.PathValue("step"), actingParty(r), req.Reason)
	h.respond(w, r, c, err, http.StatusOK)
}

// Finish closes a checklist.
func (h *Checklists) Finish(w http.ResponseWriter, r *http.Request) {
	c, err := h.checklists.Finish(r.Context(), r.PathValue("id"))
	h.respond(w, r, c, err, http.StatusOK)
}

type abandonRequest struct {
	Reason string `json:"reason"`
}

// Abandon stops a checklist, with a reason.
func (h *Checklists) Abandon(w http.ResponseWriter, r *http.Request) {
	var req abandonRequest
	if !decode(w, r, &req) {
		return
	}
	c, err := h.checklists.Abandon(r.Context(), r.PathValue("id"), req.Reason)
	h.respond(w, r, c, err, http.StatusOK)
}

type progressResponse struct {
	ChecklistID         string `json:"id"`
	Process             string `json:"process"`
	PropertyID          string `json:"property_id"`
	UnitID              string `json:"unit_id,omitempty"`
	LeaseID             string `json:"lease_id,omitempty"`
	State               string `json:"state"`
	Tasks               int    `json:"tasks"`
	Settled             int    `json:"settled"`
	Outstanding         int    `json:"outstanding"`
	BlockingOutstanding int    `json:"blocking_outstanding"`
	NextDueOn           string `json:"next_due_on,omitempty"`
	// DaysOverdue is the story's edge case made visible: a checklist nobody
	// abandoned and nobody finished reads as one under way until this is non-zero.
	DaysOverdue int `json:"days_overdue,omitempty"`
}

// Portfolio lists progress across the organisation, the late ones first.
func (h *Checklists) Portfolio(w http.ResponseWriter, r *http.Request) {
	state := domain.State(r.URL.Query().Get("state"))
	rows, err := h.checklists.Portfolio(r.Context(), state, 0)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]progressResponse, 0, len(rows))
	for _, p := range rows {
		row := progressResponse{
			ChecklistID: p.ChecklistID, Process: string(p.Process),
			PropertyID: p.PropertyID, UnitID: p.UnitID, LeaseID: p.LeaseID,
			State: string(p.State), Tasks: p.Tasks, Settled: p.Settled,
			Outstanding: p.Outstanding, BlockingOutstanding: p.BlockingOutstanding,
			DaysOverdue: p.DaysOverdue,
		}
		if !p.NextDueOn.Zero() {
			row.NextDueOn = p.NextDueOn.String()
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"checklists": out})
}

// respond maps the module's errors once, so every route refuses the same way.
func (h *Checklists) respond(w http.ResponseWriter, r *http.Request,
	c service.Checklist, err error, ok int) {

	if err == nil {
		writeJSON(w, ok, render(c))
		return
	}
	h.fail(w, r, err)
}

func (h *Checklists) fail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, service.ErrNoChecklist):
		writeError(w, http.StatusNotFound, "no such checklist")
	case errors.Is(err, service.ErrNoTemplate):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	// 409 rather than 422: nothing about the request is wrong, and the same request
	// succeeds once the outstanding step is done. ADR-0032 §4.
	case errors.Is(err, service.ErrBlocked), errors.Is(err, domain.ErrDependency):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrTemplate), errors.Is(err, domain.ErrAnchorRequired):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("checklist request failed", "path", r.URL.Path, "error", err)
		writeError(w, http.StatusInternalServerError, "could not complete the request")
	}
}

// actingParty is who is doing the work, as a party uuid, or empty.
//
// Empty for an Ops request today, and deliberately not authz.Subject(): that returns
// `user:<uid>`, which is an authorisation subject and not a party id — the same
// distinction that keeps a role label out of the outbox's actor_id. The columns are
// nullable until the request principal carries a party uuid (#229), at which point
// this is the one line that changes.
func actingParty(r *http.Request) string {
	if party, ok := tenancy.ResidentFrom(r.Context()); ok {
		return party.String()
	}
	return ""
}

func decode(w http.ResponseWriter, r *http.Request, into any) bool {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return false
	}
	if len(body) == 0 {
		return true // every body here is optional; the handler validates what it needs
	}
	if err := json.Unmarshal(body, into); err != nil {
		writeError(w, http.StatusBadRequest, "the request is not valid JSON")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
