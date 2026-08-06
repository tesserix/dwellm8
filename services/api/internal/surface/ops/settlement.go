package ops

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	moneyprovider "github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// WithSettlements wires the division of a collection and its payout (#270).
func (h *Handler) WithSettlements(s *moneyservice.Settlements) *Handler {
	h.settlements = s
	return h
}

// SettlementRoutes mounts the manager's settlement queue. Releasing money is a
// money.payout decision, not a viewing one.
func (h *Handler) SettlementRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/settlements", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.Settlements)
	r.Handle("POST /v1/ops/settlements/{id}/release", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.ReleaseSettlement)
}

// settlementResponse is one collection divided, as the screen shows it.
type settlementResponse struct {
	ID              string `json:"id"`
	PaymentID       string `json:"payment_id"`
	LeaseID         string `json:"lease_id,omitempty"`
	Currency        string `json:"currency"`
	GrossMinor      int64  `json:"gross_amount_minor"`
	PlatformMinor   int64  `json:"platform_amount_minor"`
	ManagementMinor int64  `json:"management_amount_minor"`
	TDSMinor        int64  `json:"tds_amount_minor"`
	OwnerMinor      int64  `json:"owner_amount_minor"`
	State           string `json:"state"`
	Provider        string `json:"provider,omitempty"`
	TransferRef     string `json:"transfer_ref,omitempty"`
	ExpectedOn      string `json:"expected_on,omitempty"`
	SettledOn       string `json:"settled_on,omitempty"`
	Reason          string `json:"reason,omitempty"`
	// Overdue is the manager's exception list: money the schedule promised by a
	// date that has passed.
	Overdue bool `json:"overdue"`
}

func settlementView(s moneyservice.Division, on time.Time) settlementResponse {
	return settlementResponse{
		ID: s.ID, PaymentID: s.PaymentID, LeaseID: s.LeaseID, Currency: s.Currency,
		GrossMinor: int64(s.Split.Gross), PlatformMinor: int64(s.Split.Platform),
		ManagementMinor: int64(s.Split.Management), TDSMinor: int64(s.Split.TDS),
		OwnerMinor: int64(s.Split.Owner), State: s.State, Provider: s.Provider,
		TransferRef: s.TransferRef, ExpectedOn: dayOrEmpty(s.ExpectedOn),
		SettledOn: dayOrEmpty(s.SettledOn), Reason: s.Reason,
		Overdue: s.State != "settled" &&
			!s.ExpectedOn.IsZero() && s.ExpectedOn.Before(on),
	}
}

func dayOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.DateOnly)
}

// Settlements is what the manager is holding on somebody else's behalf.
func (h *Handler) Settlements(w http.ResponseWriter, r *http.Request) {
	if h.settlements == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	now := time.Now().UTC()
	list, err := h.settlements.Queue(r.Context(), now)
	if err != nil {
		h.log.Error("listing settlements", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the settlements")
		return
	}
	out := make([]settlementResponse, 0, len(list))
	for _, s := range list {
		out = append(out, settlementView(s, now))
	}
	writeJSON(w, http.StatusOK, map[string]any{"settlements": out})
}

// ReleaseSettlement sends the owner's leg to their bank through whichever
// provider carried the collection.
func (h *Handler) ReleaseSettlement(w http.ResponseWriter, r *http.Request) {
	if h.settlements == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req struct {
		BeneficiaryRef string `json:"beneficiary_ref"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that request could not be read")
		return
	}
	if strings.TrimSpace(req.BeneficiaryRef) == "" {
		writeError(w, http.StatusUnprocessableEntity, "no beneficiary to pay")
		return
	}

	id := r.PathValue("id")
	if err := h.settlements.Release(r.Context(), id, req.BeneficiaryRef); err != nil {
		h.settlementError(w, "releasing a settlement", err)
		return
	}
	out, err := h.settlements.ByID(r.Context(), id)
	if err != nil {
		h.settlementError(w, "reading a released settlement", err)
		return
	}
	writeJSON(w, http.StatusOK, settlementView(out, time.Now().UTC()))
}

func (h *Handler) settlementError(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, moneyservice.ErrNoInstruction):
		writeError(w, http.StatusNotFound, "no settlement with that id")
	case errors.Is(err, moneyservice.ErrNoMerchant), errors.Is(err, merchant.ErrUnverified):
		writeError(w, http.StatusUnprocessableEntity,
			"connect and verify the account rent settles into first")
	case errors.Is(err, moneyprovider.ErrCapability):
		writeError(w, http.StatusUnprocessableEntity, "that provider cannot pay out from here")
	case errors.Is(err, moneyprovider.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "your provider is not answering — try again shortly")
	default:
		h.log.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "that could not be done")
	}
}
