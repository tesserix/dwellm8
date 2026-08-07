package ops

import (
	"errors"
	"net/http"

	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// WithPayments wires the collection a manager records by hand (#297).
func (h *Handler) WithPayments(p *moneyservice.Payments) *Handler {
	h.payments = p
	return h
}

// CollectionRoutes mounts recording rent that arrived outside the app. Taking
// money is a money decision, not a viewing one.
func (h *Handler) CollectionRoutes(r *authz.Registrar) {
	r.Handle("POST /v1/ops/tenancies/{lease}/collections", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.RecordCollection)
}

type collectionRequest struct {
	AmountMinor    int64  `json:"amount_minor"`
	Method         string `json:"method"`
	Reference      string `json:"reference"`
	IdempotencyKey string `json:"idempotency_key"`
}

type collectionResponse struct {
	PaymentID   string `json:"payment_id"`
	LeaseID     string `json:"lease_id"`
	Status      string `json:"status"`
	AmountMinor int64  `json:"amount_minor"`
	Method      string `json:"method"`
	Reference   string `json:"reference,omitempty"`
	DueMinor    int64  `json:"due_amount_minor"`
	// AdvanceMinor is money received that no charge has claimed. Reported
	// beside the due because a due of zero on its own cannot tell a manager
	// whether the tenant is square or has paid two months ahead.
	AdvanceMinor int64 `json:"advance_amount_minor"`
}

// RecordCollection records rent the manager took in person: cash, a cheque, or
// a transfer they watched land. It goes through the same Collect and Confirm a
// tenant's own payment does, so one ledger posting and one receipt exist for
// every rupee however it arrived.
func (h *Handler) RecordCollection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	firm, ok := tenancy.From(ctx)
	if !ok {
		writeError(w, http.StatusUnauthorized, "no organisation on this session")
		return
	}
	leaseID := r.PathValue("lease")

	var req collectionRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if req.AmountMinor <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "amount_minor must be a positive number of paise")
		return
	}
	if req.IdempotencyKey == "" {
		writeError(w, http.StatusUnprocessableEntity,
			"idempotency_key is required — without one a retry on a bad connection is a second receipt")
		return
	}
	method := collect.Method(req.Method)
	if req.Method == "" {
		method = collect.MethodOfflineCash
	}
	// Offline means a person witnessed the money. A manager asserting a UPI
	// collection by hand would be recording a receipt nobody saw (ADR-0011 §6).
	if !method.IsOffline() {
		writeError(w, http.StatusUnprocessableEntity,
			"only a payment taken offline can be recorded here — cash, a cheque, or a transfer you have seen")
		return
	}

	today := effective.DateOf(h.now(), ist)
	t, err := h.leases.Tenancy(ctx, leaseID, today)
	if errors.Is(err, leasestore.ErrNoLease) {
		writeError(w, http.StatusNotFound, "no such tenancy")
		return
	}
	if err != nil {
		h.log.Error("reading a tenancy before recording a collection", "lease", leaseID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the tenancy")
		return
	}

	// The deposit and the first month change hands at signing, so ask who the
	// parties are as at the term's first day while that is still ahead — the
	// payer is the same person either way (#303).
	on := today
	if start := t.Term.From(); today.Before(start) {
		on = start
	}
	tenantParty, _, err := h.leases.PartiesOf(ctx, leaseID, on)
	if err != nil || tenantParty == "" {
		h.log.Error("no tenant on a tenancy being collected from", "lease", leaseID, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "this tenancy has nobody to credit the payment to")
		return
	}

	reference := req.Reference
	if reference == "" {
		reference = "Rent, " + t.UnitCode
	}

	payment, err := h.payments.Record(ctx, moneyservice.CollectRequest{
		TenantID: firm.String(),
		// The place, the tenancy and the payer come from the lease, never from
		// the body: a receipt that could name its own tenant would be one
		// anybody could post against anybody.
		Lease:     leaseID,
		Property:  t.PropertyID,
		Unit:      t.UnitID,
		PayerID:   tenantParty,
		PayerName: t.UnitCode + ", " + t.PropertyName,
		Amount:    domain.Minor(req.AmountMinor),
		Method:    method,
		Reference: reference,
		// Scoped to the firm and the lease so two managers cannot collide on a
		// key, and the same key on two tenancies is two receipts.
		IdempotencyKey: "ops:" + firm.String() + ":" + leaseID + ":" + req.IdempotencyKey,
	})
	if err != nil {
		h.log.Error("recording an offline collection", "lease", leaseID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not record the payment")
		return
	}

	out := collectionResponse{
		PaymentID: payment.ID, LeaseID: leaseID, Status: string(payment.Status),
		AmountMinor: int64(payment.Amount), Method: string(payment.Method), Reference: reference,
	}
	if position, err := h.statements.Position(ctx, leaseID, tenantParty); err == nil {
		out.DueMinor, out.AdvanceMinor = int64(position.Due), int64(position.Advance)
	}
	writeJSON(w, http.StatusCreated, out)
}
