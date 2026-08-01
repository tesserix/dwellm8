package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// PayoutAccounts is the change endpoint of #227. The read side arrives with
// the payout run (#80); nothing here returns an account number, because
// nothing here ever holds one.
type PayoutAccounts struct {
	accounts *service.PayoutAccounts
	log      *slog.Logger
}

func NewPayoutAccounts(s *service.PayoutAccounts, log *slog.Logger) *PayoutAccounts {
	return &PayoutAccounts{accounts: s, log: log}
}

// Routes mounts the module. Changing where an owner's money goes is owner
// administration, not day-to-day operation - can_administer, deliberately, so
// a manager's mandate alone cannot redirect a payout (threat-model §4).
func (h *PayoutAccounts) Routes(r *authz.Registrar) {
	r.Handle("PUT /v1/payout-accounts/{owner}", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Change)
}

type changeAccountRequest struct {
	AccountNumber string `json:"account_number"`
	IFSC          string `json:"ifsc"`
}

type changeAccountResponse struct {
	ID            string `json:"id"`
	MaskedAccount string `json:"masked_account"`
	// The cool-off made visible: a client that shows this is a client whose
	// owner is not surprised when the next payout holds.
	PayableAt time.Time `json:"payable_at,omitempty"`
	Unchanged bool      `json:"unchanged,omitempty"`
}

func (h *PayoutAccounts) Change(w http.ResponseWriter, r *http.Request) {
	var req changeAccountRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send account_number and ifsc")
		return
	}

	out, err := h.accounts.Change(r.Context(), r.PathValue("owner"),
		pii.NewSecret(req.AccountNumber), req.IFSC)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, changeAccountResponse{
			ID: out.ID, MaskedAccount: out.NewMasked,
			PayableAt: out.PayableAt, Unchanged: out.Unchanged})
	case errors.Is(err, service.ErrUnconfigured):
		writeError(w, http.StatusServiceUnavailable, "payout account changes are not available")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	case errors.Is(err, pii.ErrShape):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		h.log.Error("changing a payout account", "error", err)
		writeError(w, http.StatusInternalServerError, "could not change the payout account")
	}
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
