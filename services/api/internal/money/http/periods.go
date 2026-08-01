package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// Periods is the period-close surface (#190).
type Periods struct {
	periods *store.Periods
	log     *slog.Logger
	now     func() time.Time
}

func NewPeriods(p *store.Periods, log *slog.Logger) *Periods {
	return &Periods{periods: p, log: log, now: time.Now}
}

// Routes mounts the surface. Close and reopen are can_administer — the same
// judgement as the payout-account change: locking the books an owner's
// statements rest on is owner administration, not day-to-day operation. The
// reads are can_view_financials, which is where the accountant lives.
func (h *Periods) Routes(r *authz.Registrar) {
	r.Handle("GET /v1/ledger/periods/{month}", authz.Check{
		Relation: "can_view_financials", Object: authz.Organisation()}, h.State)
	r.Handle("GET /v1/ledger/periods/{month}/audit-pack", authz.Check{
		Relation: "can_view_financials", Object: authz.Organisation()}, h.Pack)
	r.Handle("POST /v1/ledger/periods/{month}/close", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Close)
	r.Handle("POST /v1/ledger/periods/{month}/reopen", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Reopen)
}

// ist: a period is an Indian calendar month, the same zone every date in this
// product renders in.
var istZone = time.FixedZone("IST", 5*3600+1800)

// month parses the path's {month} as 2006-01, refusing anything else.
func (h *Periods) month(w http.ResponseWriter, r *http.Request) (time.Time, bool) {
	m, err := time.ParseInLocation("2006-01", r.PathValue("month"), time.UTC)
	if err != nil {
		writeStatus(w, http.StatusBadRequest, "the period is a month, named like 2026-07")
		return time.Time{}, false
	}
	return m, true
}

type actionRequest struct {
	Reason string `json:"reason,omitempty"`
}

type stateResponse struct {
	Month   string               `json:"month"`
	Closed  bool                 `json:"closed"`
	History []store.PeriodAction `json:"history,omitempty"`
}

// State is one month's standing.
func (h *Periods) State(w http.ResponseWriter, r *http.Request) {
	m, ok := h.month(w, r)
	if !ok {
		return
	}
	s, err := h.periods.State(r.Context(), m)
	if err != nil {
		h.log.Error("reading a period", "month", m.Format("2006-01"), "error", err)
		writeStatus(w, http.StatusInternalServerError, "could not read the period")
		return
	}
	writeBody(w, http.StatusOK, stateResponse{
		Month: m.Format("2006-01"), Closed: s.Closed, History: s.History,
	})
}

// Close locks the month, checklist permitting.
func (h *Periods) Close(w http.ResponseWriter, r *http.Request) {
	m, ok := h.month(w, r)
	if !ok {
		return
	}
	// Only a finished month closes. Closing the month that is still running
	// would refuse the very postings the month has yet to earn, and the mistake
	// would look like an outage of everything.
	if !m.Before(time.Date(h.now().In(istZone).Year(), h.now().In(istZone).Month(), 1, 0, 0, 0, 0, time.UTC)) {
		writeStatus(w, http.StatusUnprocessableEntity, "only a finished month can close")
		return
	}
	var req actionRequest
	_ = json.NewDecoder(r.Body).Decode(&req) // an empty body is a plain close

	err := h.periods.Close(r.Context(), m, actor(r), req.Reason)
	var blocked store.ErrBlocked
	switch {
	case err == nil:
		writeBody(w, http.StatusOK, map[string]any{"month": m.Format("2006-01"), "closed": true})
	case errors.As(err, &blocked):
		// 409 with the list: a close blocked over a known gap names the gap.
		writeBody(w, http.StatusConflict, map[string]any{
			"error": "the period cannot close over outstanding items", "blockers": blocked.Blockers,
		})
	case errors.Is(err, store.ErrAlreadyClosed):
		writeStatus(w, http.StatusConflict, "the period is already closed")
	default:
		h.log.Error("closing a period", "month", m.Format("2006-01"), "error", err)
		writeStatus(w, http.StatusInternalServerError, "could not close the period")
	}
}

// Reopen is the rare, audited exception.
func (h *Periods) Reopen(w http.ResponseWriter, r *http.Request) {
	m, ok := h.month(w, r)
	if !ok {
		return
	}
	var req actionRequest
	_ = json.NewDecoder(r.Body).Decode(&req)
	if len(req.Reason) < 8 {
		writeStatus(w, http.StatusUnprocessableEntity,
			"a reopen carries a reason a reviewer can weigh — eight characters at least")
		return
	}

	err := h.periods.Reopen(r.Context(), m, actor(r), req.Reason)
	switch {
	case err == nil:
		writeBody(w, http.StatusOK, map[string]any{"month": m.Format("2006-01"), "closed": false})
	case errors.Is(err, store.ErrNotClosed):
		writeStatus(w, http.StatusConflict, "the period is not closed")
	default:
		h.log.Error("reopening a period", "month", m.Format("2006-01"), "error", err)
		writeStatus(w, http.StatusInternalServerError, "could not reopen the period")
	}
}

// Pack is the month, exportable.
func (h *Periods) Pack(w http.ResponseWriter, r *http.Request) {
	m, ok := h.month(w, r)
	if !ok {
		return
	}
	pack, err := h.periods.Pack(r.Context(), m)
	if err != nil {
		h.log.Error("assembling an audit pack", "month", m.Format("2006-01"), "error", err)
		writeStatus(w, http.StatusInternalServerError, "could not assemble the audit pack")
		return
	}
	w.Header().Set("Content-Disposition",
		`attachment; filename="audit-pack-`+m.Format("2006-01")+`.json"`)
	writeBody(w, http.StatusOK, pack)
}

// actor is the session's principal — a label until #229 lands party uuids,
// the same trade the outbox actors make.
func actor(r *http.Request) string {
	if p, ok := auth.From(r.Context()); ok && p.UID != "" {
		return p.UID
	}
	return "unknown"
}

func writeBody(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeStatus(w http.ResponseWriter, code int, msg string) {
	writeBody(w, code, map[string]string{"error": msg})
}
