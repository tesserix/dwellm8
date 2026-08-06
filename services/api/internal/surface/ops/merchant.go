package ops

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/merchant"
	moneyprovider "github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// WithMerchants wires the manager's own merchant account (#269).
func (h *Handler) WithMerchants(m *moneyservice.Merchants) *Handler {
	h.merchants = m
	return h
}

// MerchantRoutes mounts the account the manager collects into. money.payout
// rather than can_view: this decides where rent lands.
func (h *Handler) MerchantRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/merchant", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.Merchant)
	r.Handle("POST /v1/ops/merchant", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.ConnectMerchant)
	r.Handle("POST /v1/ops/merchant/{provider}/refresh", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.RefreshMerchant)
}

type connectRequest struct {
	Provider      string `json:"provider"`
	BusinessName  string `json:"business_name"`
	BusinessType  string `json:"business_type"`
	Country       string `json:"country"`
	Email         string `json:"email"`
	Phone         string `json:"phone"`
	PAN           string `json:"pan"`
	GSTIN         string `json:"gstin"`
	AccountNumber string `json:"account_number"`
	AccountHolder string `json:"account_holder"`
	IFSC          string `json:"ifsc"`
	Currency      string `json:"currency"`
}

// merchantResponse is what the screen may know. No account number, ever — it
// went to the provider and nothing on this side kept it.
type merchantResponse struct {
	Provider     string `json:"provider"`
	BusinessName string `json:"business_name"`
	State        string `json:"state"`
	Reason       string `json:"reason,omitempty"`
	Masked       string `json:"settlement_masked"`
	IFSC         string `json:"settlement_ifsc,omitempty"`
	Currency     string `json:"settlement_currency"`
	MayCollect   bool   `json:"may_collect"`
	// NextAction is the sentence the screen shows: a state name is not an
	// instruction, and the manager wants to know what happens now.
	NextAction string `json:"next_action"`
}

func merchantView(a merchant.Account) merchantResponse {
	return merchantResponse{
		Provider: a.Provider, BusinessName: a.BusinessName, State: string(a.State),
		Reason: a.Reason, Masked: a.Settlement.Masked, IFSC: a.Settlement.IFSC,
		Currency: a.Settlement.Currency, MayCollect: a.State.MayCollect(),
		NextAction: nextAction(a),
	}
}

func nextAction(a merchant.Account) string {
	switch a.State {
	case merchant.Verified:
		return "Rent can be collected into this account."
	case merchant.Submitted:
		return "Your provider is verifying the bank account. This usually takes a working day."
	case merchant.Suspended:
		if a.Reason != "" {
			return "Your provider has paused settlements: " + a.Reason
		}
		return "Your provider has paused settlements. Contact them to restore the account."
	default:
		return "Connect a bank account so rent has somewhere to settle."
	}
}

// Merchant lists every provider this manager has connected.
func (h *Handler) Merchant(w http.ResponseWriter, r *http.Request) {
	if h.merchants == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	list, err := h.merchants.List(r.Context())
	if err != nil {
		h.log.Error("listing merchant accounts", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the accounts")
		return
	}
	out := make([]merchantResponse, 0, len(list))
	for _, a := range list {
		out = append(out, merchantView(a))
	}
	writeJSON(w, http.StatusOK, map[string]any{"accounts": out})
}

// ConnectMerchant submits the manager's own account to their provider.
func (h *Handler) ConnectMerchant(w http.ResponseWriter, r *http.Request) {
	if h.merchants == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	var req connectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that request could not be read")
		return
	}
	out, err := h.merchants.Connect(r.Context(), moneyservice.ConnectRequest{
		Provider: req.Provider, BusinessName: req.BusinessName, BusinessType: req.BusinessType,
		Country: req.Country, Email: req.Email, Phone: req.Phone,
		TaxID: pii.NewSecret(req.PAN), GSTIN: req.GSTIN,
		AccountNumber: pii.NewSecret(req.AccountNumber), AccountHolder: req.AccountHolder,
		IFSC: req.IFSC, Currency: req.Currency,
	})
	if err != nil {
		h.merchantError(w, "connecting a merchant account", err)
		return
	}
	writeJSON(w, http.StatusOK, merchantView(out))
}

// RefreshMerchant asks the provider where the account has got to. The manager
// presses this while waiting; the same call is what a webhook triggers.
func (h *Handler) RefreshMerchant(w http.ResponseWriter, r *http.Request) {
	if h.merchants == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	out, err := h.merchants.Refresh(r.Context(), r.PathValue("provider"))
	if err != nil {
		h.merchantError(w, "refreshing a merchant account", err)
		return
	}
	writeJSON(w, http.StatusOK, merchantView(out))
}

// merchantError keeps the provider's own words where they help the manager,
// and says nothing about the account number in any case.
func (h *Handler) merchantError(w http.ResponseWriter, what string, err error) {
	switch {
	case errors.Is(err, moneyservice.ErrNoMerchant):
		writeError(w, http.StatusNotFound, "no account with that provider yet")
	case errors.Is(err, moneyprovider.ErrCapability):
		writeError(w, http.StatusUnprocessableEntity, "that provider cannot open an account from here")
	case errors.Is(err, pii.ErrShape), errors.Is(err, pii.ErrProhibited),
		errors.Is(err, merchant.ErrIncomplete), errors.Is(err, merchant.ErrTransition):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, moneyprovider.ErrUnavailable):
		writeError(w, http.StatusServiceUnavailable, "your provider is not answering — try again shortly")
	default:
		h.log.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "that could not be saved")
	}
}
