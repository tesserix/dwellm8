// Package automations is the settings surface for ADR-0033's prebuilt automations.
//
// A surface rather than a module, and it owns nothing: the engine holds the rules
// and the tables, and this composes the catalogue, the resolved settings and the
// recent activity into the one screen a manager needs — what is running, what it
// would do, and what it is waiting on.
//
// The activity beside each switch is not decoration. A toggle with nothing next to
// it asks somebody to believe an automation is working; a toggle that says "acted
// 14 times, last on Tuesday" is a toggle they can act on.
package automations

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/automation"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

const maxBody = 16 << 10

// Reader is the slice of the engine's store this surface reads.
type Reader interface {
	Activity(ctx context.Context) (map[automation.Key]automation.Activity, error)
	History(ctx context.Context, subject automation.Subject, limit int) ([]automation.RunRecord, error)
	Pending(ctx context.Context, limit int) ([]automation.Approval, error)
	Decide(ctx context.Context, id, state, reason, by string) error
}

// Automations handles the settings routes.
type Automations struct {
	runner *automation.Runner
	store  Reader
	log    *slog.Logger
}

// New builds the handler.
func New(r *automation.Runner, s Reader, log *slog.Logger) *Automations {
	return &Automations{runner: r, store: s, log: log}
}

// Routes mounts the surface. Each route names its ADR-0020 check.
//
// Reading is can_view and changing is can_administer, deliberately: switching off
// the arrears ladder changes what every tenant of this organisation experiences,
// which is an owner's decision rather than a manager's day-to-day operation. The
// approval queue is can_approve_spend, because that is exactly what it is.
func (h *Automations) Routes(r *authz.Registrar) {
	r.Handle("GET /v1/automations", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.List)
	r.Handle("PUT /v1/automations/{key}", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Set)
	r.Handle("GET /v1/automations/history/{kind}/{id}", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.History)
	r.Handle("GET /v1/automations/approvals", authz.Check{
		Relation: "can_approve_spend", Object: authz.Organisation()}, h.Approvals)
	r.Handle("POST /v1/automations/approvals/{id}", authz.Check{
		Relation: "can_approve_spend", Object: authz.Organisation()}, h.Decide)
}

type paramResponse struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
	Unit    string `json:"unit"`
	Value   int64  `json:"value"`
	Default int64  `json:"default"`
	Min     int64  `json:"min"`
	Max     int64  `json:"max"`
}

type automationResponse struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
	Trigger string `json:"trigger"`
	On      string `json:"on,omitempty"`

	Enabled          bool  `json:"enabled"`
	EnabledByDefault bool  `json:"enabled_by_default"`
	CeilingMinor     int64 `json:"approval_ceiling_minor"`

	Params []paramResponse `json:"params"`
	// Overridden names what this organisation changed, so a screen can show
	// "customised" beside the ones that are and nothing beside the rest.
	Overridden []string `json:"overridden,omitempty"`

	Runs      int    `json:"runs"`
	Acted     int    `json:"acted"`
	Awaiting  int    `json:"awaiting_approval"`
	Failed    int    `json:"failed"`
	LastRunAt string `json:"last_run_at,omitempty"`
}

// List returns every automation this build ships, resolved for this organisation.
//
// The catalogue rather than the rows, which is the whole of ADR-0033 §1: an
// organisation that has configured nothing still sees eight automations here, all
// running, because that is what is actually happening.
func (h *Automations) List(w http.ResponseWriter, r *http.Request) {
	settings, catalogue, err := h.runner.SettingsFor(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	activity, err := h.store.Activity(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}

	out := make([]automationResponse, 0, len(catalogue))
	for i, d := range catalogue {
		s := settings[i]
		row := automationResponse{
			Key: d.Key.String(), Name: d.Name, Purpose: d.Purpose,
			Trigger: string(d.Trigger), On: d.On,
			Enabled: s.Enabled, EnabledByDefault: d.EnabledByDefault,
			CeilingMinor: s.CeilingMinor, Overridden: s.Overridden,
		}
		for _, p := range d.Params {
			row.Params = append(row.Params, paramResponse{
				Name: p.Name, Purpose: p.Purpose, Unit: p.Unit,
				Value: s.Param(p.Name), Default: p.Default, Min: p.Min, Max: p.Max,
			})
		}
		if a, ok := activity[d.Key]; ok {
			row.Runs, row.Acted, row.Awaiting, row.Failed = a.Runs, a.Acted, a.Awaiting, a.Failed
			if !a.LastRunAt.IsZero() {
				row.LastRunAt = a.LastRunAt.Format(time.RFC3339)
			}
		}
		out = append(out, row)
	}
	writeJSON(w, http.StatusOK, map[string]any{"automations": out})
}

type setRequest struct {
	Enabled      *bool            `json:"enabled"`
	Params       map[string]int64 `json:"params"`
	CeilingMinor *int64           `json:"approval_ceiling_minor"`
}

// Set writes one override. The story's "switchable off per organisation without a
// release", and it is one row.
func (h *Automations) Set(w http.ResponseWriter, r *http.Request) {
	var req setRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil || (len(body) > 0 && json.Unmarshal(body, &req) != nil) {
		writeError(w, http.StatusBadRequest, "send enabled, params or approval_ceiling_minor")
		return
	}

	key := automation.Key(r.PathValue("key"))
	err = h.runner.Set(r.Context(), key, automation.Override{
		Enabled: req.Enabled, Params: req.Params, CeilingMinor: req.CeilingMinor,
	}, actingParty(r))
	switch {
	case err == nil:
		h.List(w, r) // the whole resolved list back, so a client re-renders from one response
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		// A bad parameter and an unknown automation are both the caller's mistake
		// and both name what is wrong, so they are returned rather than logged.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	}
}

type historyResponse struct {
	ID         string `json:"id"`
	Automation string `json:"automation"`
	Outcome    string `json:"outcome"`
	Action     string `json:"action"`
	Detail     string `json:"detail,omitempty"`
	Amount     int64  `json:"amount_minor,omitempty"`
	OccurredAt string `json:"occurred_at"`
}

// History is what was automated on one record. The story's edge case: "a record
// must show which automation caused an action and when".
func (h *Automations) History(w http.ResponseWriter, r *http.Request) {
	subject := automation.Subject{
		Kind: automation.SubjectKind(r.PathValue("kind")),
		ID:   r.PathValue("id"),
	}
	rows, err := h.store.History(r.Context(), subject, 0)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]historyResponse, 0, len(rows))
	for _, x := range rows {
		out = append(out, historyResponse{
			ID: x.ID, Automation: x.Automation.String(), Outcome: string(x.Outcome),
			Action: x.Action, Detail: x.Detail, Amount: x.Amount,
			OccurredAt: x.OccurredAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"history": out})
}

type approvalResponse struct {
	ID          string `json:"id"`
	Automation  string `json:"automation"`
	SubjectKind string `json:"subject_kind"`
	SubjectID   string `json:"subject_id"`
	Action      string `json:"action"`
	Amount      int64  `json:"amount_minor"`
	Ceiling     int64  `json:"ceiling_minor"`
	RequestedAt string `json:"requested_at"`
}

// Approvals lists what automations stopped for. Oldest first: the queue is a
// queue, and showing the newest at the top buries the one that has waited longest.
func (h *Automations) Approvals(w http.ResponseWriter, r *http.Request) {
	rows, err := h.store.Pending(r.Context(), 0)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	out := make([]approvalResponse, 0, len(rows))
	for _, a := range rows {
		out = append(out, approvalResponse{
			ID: a.ID, Automation: a.Automation.String(),
			SubjectKind: string(a.Subject.Kind), SubjectID: a.Subject.ID,
			Action: a.Action, Amount: a.Amount, Ceiling: a.Ceiling,
			RequestedAt: a.RequestedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"approvals": out})
}

type decideRequest struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

// Decide records an approval or a decline.
//
// It performs nothing. Approving releases the proposal for the next run, which
// goes through the ceiling as it stands at that moment — so approving a request
// and raising a ceiling are the same act with the same trail, rather than two
// paths where one skips the check.
func (h *Automations) Decide(w http.ResponseWriter, r *http.Request) {
	var req decideRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send decision as approved or declined")
		return
	}

	err = h.store.Decide(r.Context(), r.PathValue("id"), req.Decision, req.Reason, actingParty(r))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "state": req.Decision})
	case errors.Is(err, automation.ErrNoApproval):
		writeError(w, http.StatusNotFound, "no such request awaiting a decision")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	}
}

func (h *Automations) fail(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, tenancy.ErrNoTenant) {
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
		return
	}
	h.log.Error("automations request failed", "path", r.URL.Path, "error", err)
	writeError(w, http.StatusInternalServerError, "could not read the automations")
}

// actingParty is who changed the setting, as a party uuid, or empty. Same
// reasoning as the checklist surface: authz.Subject is an authorisation subject
// and not a party id, and the column is nullable until #229 lands.
func actingParty(r *http.Request) string {
	if party, ok := tenancy.ResidentFrom(r.Context()); ok {
		return party.String()
	}
	return ""
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
