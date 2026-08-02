package resident

import (
	"errors"
	"net/http"
	"time"

	communityservice "github.com/tesserix/dwellm8/services/api/internal/community/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

type messageResponse struct {
	MessageID string `json:"message_id"`
	Sender    string `json:"sender"`
	Mine      bool   `json:"mine"`
	Body      string `json:"body"`
	SentAt    string `json:"sent_at"`
}

// Messages is the tenancy's conversation, oldest first.
func (h *Handler) Messages(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)

	thread, err := h.community.Thread(ctx, res.LeaseID, historyLimit)
	if err != nil {
		h.fail(w, "reading the conversation", res.LeaseID, err)
		return
	}
	out := make([]messageResponse, 0, len(thread))
	for _, m := range thread {
		out = append(out, messageResponse{
			MessageID: m.ID, Sender: m.Sender,
			Mine: m.Sender == "resident" && m.SenderPartyID == session.PartyID,
			Body: m.Body, SentAt: m.CreatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"lease_id": res.LeaseID, "messages": out})
}

type sendRequest struct {
	Body string `json:"body"`
}

// SendMessage appends the renter's message to the thread.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	var req sendRequest
	if err := decode(w, r, &req); err != nil {
		return
	}

	ctx := session.Scope(r.Context(), res)
	t, err := h.leases.Tenancy(ctx, res.LeaseID, effective.DateOf(h.now(), ist))
	if err != nil {
		h.fail(w, "reading a tenancy before messaging", res.LeaseID, err)
		return
	}

	m, err := h.community.Send(ctx, communityservice.Message{
		TenantID: res.TenantID.String(), LeaseID: res.LeaseID,
		PropertyID: t.PropertyID, UnitID: t.UnitID,
		Sender: "resident", SenderPartyID: session.PartyID, Body: req.Body,
	})
	if errors.Is(err, communityservice.ErrMessage) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		h.fail(w, "sending a message", res.LeaseID, err)
		return
	}
	writeJSON(w, http.StatusCreated, messageResponse{
		MessageID: m.ID, Sender: m.Sender, Mine: true,
		Body: m.Body, SentAt: m.CreatedAt.Format(time.RFC3339),
	})
}

type passResponse struct {
	PassID    string `json:"pass_id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Code      string `json:"code"`
	State     string `json:"state"`
	ValidFrom string `json:"valid_from"`
	ValidTo   string `json:"valid_to,omitempty"`
	CreatedAt string `json:"created_at"`
}

func presentPass(p communityservice.Pass) passResponse {
	out := passResponse{
		PassID: p.ID, Name: p.Name, Kind: string(p.Kind), Code: p.Code,
		State: p.State, ValidFrom: p.ValidFrom.Format(time.RFC3339),
		CreatedAt: p.CreatedAt.Format(time.RFC3339),
	}
	if !p.ValidTo.IsZero() {
		out.ValidTo = p.ValidTo.Format(time.RFC3339)
	}
	return out
}

// Passes lists the tenancy's gate passes, newest first.
func (h *Handler) Passes(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)

	passes, err := h.community.Passes(ctx, res.LeaseID, historyLimit)
	if err != nil {
		h.fail(w, "listing gate passes", res.LeaseID, err)
		return
	}
	out := make([]passResponse, 0, len(passes))
	for _, p := range passes {
		out = append(out, presentPass(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"lease_id": res.LeaseID, "passes": out})
}

type createPassRequest struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
	// ValidHours bounds the pass from now; 0 means the store's open-ended
	// default. The renter's phone knows local intent better than a timestamp
	// field the app would have to build anyway.
	ValidHours int `json:"valid_hours"`
}

// CreatePass records an expected visitor and mints their gate code.
func (h *Handler) CreatePass(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	var req createPassRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if req.ValidHours < 0 || req.ValidHours > 24*14 {
		writeError(w, http.StatusUnprocessableEntity, "valid_hours must be between 0 and 336")
		return
	}

	ctx := session.Scope(r.Context(), res)
	t, err := h.leases.Tenancy(ctx, res.LeaseID, effective.DateOf(h.now(), ist))
	if err != nil {
		h.fail(w, "reading a tenancy before a gate pass", res.LeaseID, err)
		return
	}

	pass := communityservice.Pass{
		TenantID: res.TenantID.String(), LeaseID: res.LeaseID,
		PropertyID: t.PropertyID, UnitID: t.UnitID,
		CreatedBy: session.PartyID,
		Name:      req.Name, Kind: communityservice.PassKind(req.Kind),
	}
	if req.ValidHours > 0 {
		pass.ValidTo = h.now().Add(time.Duration(req.ValidHours) * time.Hour)
	}
	out, err := h.community.CreatePass(ctx, pass)
	if errors.Is(err, communityservice.ErrPass) {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err != nil {
		h.fail(w, "creating a gate pass", res.LeaseID, err)
		return
	}
	writeJSON(w, http.StatusCreated, presentPass(out))
}

// CancelPass withdraws an expected visitor.
func (h *Handler) CancelPass(w http.ResponseWriter, r *http.Request) {
	if h.community == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)

	p, err := h.community.CancelPass(ctx, res.LeaseID, r.PathValue("pass"))
	if errors.Is(err, communityservice.ErrNoPass) {
		writeError(w, http.StatusNotFound, "no such gate pass")
		return
	}
	if err != nil {
		h.fail(w, "cancelling a gate pass", res.LeaseID, err)
		return
	}
	writeJSON(w, http.StatusOK, presentPass(p))
}
