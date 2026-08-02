// Package resident is the tenant's own view of their tenancy. Issue #51,
// ADR-0029.
//
// It is a **surface**, not a ninth module: it owns no table, defines no rule and
// holds no domain. What it does is compose three modules' service interfaces
// into the one screen a renter opens from a reminder — where they live, what
// they owe, the button that pays it, and every receipt since they moved in.
//
// # Why it is not part of the money module
//
// Because the screen is not about money alone. "What do I owe" is money's,
// "where do I live and on what terms" is the lease module's, and "who am I and
// which tenancies are mine" is identity's. Putting the composition inside any
// one of them would make that module depend on the other two, which is exactly
// the coupling ADR-0001 exists to prevent. A surface above all three depends
// downwards only, and the arch test enforces that it reaches each of them
// through its service package and never through its store.
//
// # The rule this package must never break
//
// Every read is scoped by Session.Scope, which sets the landlord's organisation
// and the renter together. There is no path here that scopes one without the
// other: a context carrying the organisation alone would read the landlord's
// whole portfolio, and it would look like working software.
package resident

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"time"

	communityservice "github.com/tesserix/dwellm8/services/api/internal/community/service"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	leasestore "github.com/tesserix/dwellm8/services/api/internal/lease/store"
	maintenanceservice "github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/activity"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ist is the zone a rent due date is in.
//
// effective.DateOf demands a location rather than defaulting to UTC, for the
// reason that package gives: at 11pm on the 4th in Delhi, UTC still says the
// 4th is tomorrow's problem, and a tenant would be told rent falls due a day
// late. Every date this surface renders is an Indian calendar date.
var ist = time.FixedZone("IST", 5*3600+1800)

// maxBody is a ceiling on a request from this surface. Everything a tenant
// sends is a few hundred bytes; anything near this is not a payment.
const maxBody = 16 << 10

// historyLimit is how much history one call returns. A monthly tenancy of six
// months is six charges and six payments, and the phone this renders on is the
// reason it is bounded at all.
const historyLimit = 100

// Handler serves the tenant view.
type Handler struct {
	leases     *leaseservice.Leases
	statements *moneyservice.Statements
	payments   *moneyservice.Payments
	activity   activity.Feeder
	tickets    *maintenanceservice.Tickets
	community  *communityservice.Community
	identity   *identityservice.Residents
	log        *slog.Logger
	now        func() time.Time
}

// New wires the surface to the three services it composes.
func New(l *leaseservice.Leases, s *moneyservice.Statements, p *moneyservice.Payments,
	log *slog.Logger, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{leases: l, statements: s, payments: p, log: log, now: now}
}

// WithActivity adds the feed (#196). Optional the way WithResidents is on the
// lease service: a surface built without it answers the activity route 404.
func (h *Handler) WithActivity(f activity.Feeder) *Handler {
	h.activity = f
	return h
}

// WithTickets adds the maintenance tickets seam (#237). Optional like the feed.
func (h *Handler) WithTickets(t *maintenanceservice.Tickets) *Handler {
	h.tickets = t
	return h
}

// WithCommunity adds the conversation and the gate (#238). Optional like the feed.
func (h *Handler) WithCommunity(c *communityservice.Community) *Handler {
	h.community = c
	return h
}

// WithIdentity adds the identity seam, for the profile route's contact lookup.
func (h *Handler) WithIdentity(r *identityservice.Residents) *Handler {
	h.identity = r
	return h
}

// Routes mounts the surface.
//
// Every route is under one lease except the first, and that is the authorisation
// model made visible: a receipt is reached through the tenancy it belongs to, so
// there is no id in this API that can be incremented into somebody else's.
func (h *Handler) Routes(r *authz.Registrar) {
	r.Open("GET /v1/resident/tenancies",
		"the list is the caller's own residencies, resolved from their sign-in and filtered in SQL — there is no object to check that is not already the subject",
		h.Tenancies)
	r.Handle("GET /v1/resident/tenancies/{lease}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.Tenancy)
	r.Handle("GET /v1/resident/tenancies/{lease}/history", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.History)
	r.Handle("POST /v1/resident/tenancies/{lease}/payments", authz.Check{
		Relation: "tenant", Object: authz.PathObject("agreement", "lease")}, h.Pay)
	r.Handle("GET /v1/resident/tenancies/{lease}/payments/{payment}/receipt", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.Receipt)
	r.Handle("GET /v1/resident/tenancies/{lease}/activity", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.Activity)
	r.Handle("GET /v1/resident/tenancies/{lease}/tickets", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.Tickets)
	r.Handle("POST /v1/resident/tenancies/{lease}/tickets", authz.Check{
		Relation: "tenant", Object: authz.PathObject("agreement", "lease")}, h.RaiseTicket)
	r.Handle("GET /v1/resident/tenancies/{lease}/tickets/{ticket}", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.Ticket)
	r.Handle("GET /v1/resident/tenancies/{lease}/messages", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.Messages)
	r.Handle("POST /v1/resident/tenancies/{lease}/messages", authz.Check{
		Relation: "tenant", Object: authz.PathObject("agreement", "lease")}, h.SendMessage)
	r.Handle("GET /v1/resident/tenancies/{lease}/passes", authz.Check{
		Relation: "can_view", Object: authz.PathObject("agreement", "lease")}, h.Passes)
	r.Handle("POST /v1/resident/tenancies/{lease}/passes", authz.Check{
		Relation: "tenant", Object: authz.PathObject("agreement", "lease")}, h.CreatePass)
	r.Handle("POST /v1/resident/tenancies/{lease}/passes/{pass}/cancel", authz.Check{
		Relation: "tenant", Object: authz.PathObject("agreement", "lease")}, h.CancelPass)
	r.Handle("POST /v1/resident/tenancies/{lease}/notice", authz.Check{
		Relation: "tenant", Object: authz.PathObject("agreement", "lease")}, h.ServeNotice)
	r.Open("GET /v1/resident/me",
		"the answer is the session itself — who signed in and what they may reach, nothing another person's id could widen",
		h.Me)
}

// tenancyResponse is one tenancy as the renter sees it.
//
// Money is amount_minor and an integer here as everywhere — ADR-0007 applies to
// API payloads, because a rupee that becomes a float at the edge is a rupee that
// arrives wrong however carefully the ledger was written.
type tenancyResponse struct {
	LeaseID      string `json:"lease_id"`
	State        string `json:"state"`
	Live         bool   `json:"live"`
	Organisation string `json:"organisation"`

	Property string `json:"property"`
	Unit     string `json:"unit"`
	Locality string `json:"locality"`
	City     string `json:"city"`

	StartOn     string `json:"start_on"`
	EndOn       string `json:"end_on,omitempty"`
	EndedOn     string `json:"ended_on,omitempty"`
	NoticeDays  int    `json:"notice_days"`
	LockInUntil string `json:"lock_in_until,omitempty"`

	// Set while the tenancy is in notice (#239).
	NoticeServedOn  string `json:"notice_served_on,omitempty"`
	NoticeMoveOutOn string `json:"notice_move_out_on,omitempty"`

	RentMinor int64  `json:"rent_amount_minor"`
	DueDay    int    `json:"due_day"`
	Currency  string `json:"currency"`

	Dues *duesResponse `json:"dues,omitempty"`
}

// duesResponse is the breakdown, and it adds up on screen.
//
// Every component is shown rather than only the total, because a tenant who
// cannot see why they owe 27,400 instead of 25,000 raises a ticket, and the
// answer — a late fee and a part month — is already in the ledger.
type duesResponse struct {
	DueMinor        int64  `json:"due_amount_minor"`
	RentMinor       int64  `json:"rent_amount_minor"`
	LateFeeMinor    int64  `json:"late_fee_amount_minor"`
	AdjustmentMinor int64  `json:"adjustment_amount_minor"`
	PaidMinor       int64  `json:"paid_amount_minor"`
	AdvanceMinor    int64  `json:"advance_amount_minor"`
	DepositMinor    int64  `json:"deposit_amount_minor"`
	AsOf            string `json:"as_of"`
}

// Tenancies lists every tenancy this person is on, across landlords.
//
// One scoped read per tenancy rather than one query over all of them. It is more
// round trips and it is the only shape that is safe: each read runs inside the
// one organisation it concerns, so there is no query in this product whose
// result set spans two landlords, and no chance of a join leaking one into the
// other's row.
func (h *Handler) Tenancies(w http.ResponseWriter, r *http.Request) {
	session, ok := identityservice.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}

	today := effective.DateOf(h.now(), ist)
	out := make([]tenancyResponse, 0, len(session.Residencies))
	for _, res := range session.Residencies {
		ctx := session.Scope(r.Context(), res)
		t, err := h.leases.Tenancy(ctx, res.LeaseID, today)
		if errors.Is(err, leasestore.ErrNoLease) {
			// The residency list said this lease is theirs and the policy said
			// otherwise. That is a retired party row landing between the two
			// reads, and the safe reading is the policy's.
			h.log.Warn("a residency resolved to a lease the policy hides",
				"lease", res.LeaseID, "organisation", res.TenantID)
			continue
		}
		if err != nil {
			h.log.Error("reading a tenancy", "lease", res.LeaseID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not read your tenancies")
			return
		}

		item := present(t, res, today)
		dues, err := h.statements.Position(ctx, res.LeaseID, session.PartyID)
		if err != nil {
			h.log.Error("deriving dues", "lease", res.LeaseID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not read what you owe")
			return
		}
		item.Dues = presentDues(dues, today)
		out = append(out, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{"tenancies": out})
}

// Tenancy is one tenancy and what it owes.
func (h *Handler) Tenancy(w http.ResponseWriter, r *http.Request) {
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)
	today := effective.DateOf(h.now(), ist)

	t, err := h.leases.Tenancy(ctx, res.LeaseID, today)
	if err != nil {
		h.fail(w, "reading a tenancy", res.LeaseID, err)
		return
	}
	dues, err := h.statements.Position(ctx, res.LeaseID, session.PartyID)
	if err != nil {
		h.fail(w, "deriving dues", res.LeaseID, err)
		return
	}

	out := present(t, res, today)
	out.Dues = presentDues(dues, today)
	writeJSON(w, http.StatusOK, out)
}

type chargeResponse struct {
	EntryID     string `json:"entry_id"`
	Kind        string `json:"kind"`
	OccurredOn  string `json:"occurred_on"`
	AmountMinor int64  `json:"amount_minor"`
	Memo        string `json:"memo,omitempty"`
	Reversed    bool   `json:"reversed,omitempty"`
}

type paymentResponse struct {
	PaymentID     string `json:"payment_id"`
	AmountMinor   int64  `json:"amount_minor"`
	Currency      string `json:"currency"`
	Method        string `json:"method"`
	Status        string `json:"status"`
	CreatedAt     string `json:"created_at"`
	ReceivedAt    string `json:"received_at,omitempty"`
	FailureCode   string `json:"failure_code,omitempty"`
	ReceiptNumber string `json:"receipt_number,omitempty"`
	// Receipt is the URL of the receipt, present only when there is one to
	// download. A link that 404s is worse than no link on a screen whose whole
	// job is proving something happened.
	Receipt string `json:"receipt,omitempty"`
}

// History is the charges and the payments on one tenancy.
func (h *Handler) History(w http.ResponseWriter, r *http.Request) {
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)

	hist, err := h.statements.History(ctx, res.LeaseID, session.PartyID, historyLimit)
	if err != nil {
		h.fail(w, "reading history", res.LeaseID, err)
		return
	}

	charges := make([]chargeResponse, 0, len(hist.Charges))
	for _, c := range hist.Charges {
		charges = append(charges, chargeResponse{
			EntryID: c.EntryID, Kind: string(c.Kind),
			OccurredOn:  c.OccurredOn.Format(dateLayout),
			AmountMinor: int64(c.AmountMinor), Memo: c.Memo, Reversed: c.Reversed,
		})
	}

	payments := make([]paymentResponse, 0, len(hist.Payments))
	for _, p := range hist.Payments {
		item := paymentResponse{
			PaymentID: p.ID, AmountMinor: int64(p.Amount), Currency: "INR",
			Method: string(p.Method), Status: string(p.Status),
			CreatedAt: p.CreatedAt.Format(time.RFC3339), FailureCode: p.FailureCode,
		}
		received := p.CapturedAt
		if received.IsZero() {
			received = p.SettledAt
		}
		if !received.IsZero() {
			item.ReceivedAt = received.Format(time.RFC3339)
			item.ReceiptNumber = moneyservice.ReceiptNumber(p.ID, received)
			item.Receipt = "/v1/resident/tenancies/" + res.LeaseID + "/payments/" + p.ID + "/receipt"
		}
		payments = append(payments, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"lease_id": res.LeaseID, "charges": charges, "payments": payments,
	})
}

// activityEntry is one feed line as the renter sees it: the fact and its
// time. No payload — event data carries the landlord side's detail, and the
// allowlist admits the type, not its internals. Notes come through only when
// a manager marked them shared.
type activityEntry struct {
	Kind       string `json:"kind"`
	OccurredAt string `json:"occurred_at"`
	Body       string `json:"body,omitempty"`
}

// Activity is the story of one tenancy, renter's cut: lifecycle facts,
// notices, and shared notes — never the owner's fees or payouts, which is
// #196's failure scenario and is enforced by the store's resident audience,
// not by this handler remembering to filter.
func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	if h.activity == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	ctx := session.Scope(r.Context(), res)

	entries, err := h.activity.Feed(ctx, activity.Query{
		SubjectKind: "lease", SubjectID: res.LeaseID,
		Audience: activity.AudienceResident, Limit: historyLimit,
	})
	if err != nil {
		h.fail(w, "reading activity", res.LeaseID, err)
		return
	}
	out := make([]activityEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, activityEntry{
			Kind: e.Kind, OccurredAt: e.OccurredAt.Format(time.RFC3339), Body: e.Body,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"lease_id": res.LeaseID, "entries": out})
}

// payRequest is a tenant paying their own rent.
//
// The amount is the tenant's, not the server's, and that is deliberate: part
// payment is normal in Indian rental and a screen that only offers the full
// balance produces a bank transfer nobody can reconcile. What the server refuses
// is an amount that is not a positive number of paise.
type payRequest struct {
	AmountMinor    int64  `json:"amount_minor"`
	Method         string `json:"method"`
	IdempotencyKey string `json:"idempotency_key"`
}

type payResponse struct {
	PaymentID string `json:"payment_id"`
	Status    string `json:"status"`
	// PayURL and PayToken are whatever the payer's client needs to proceed —
	// which one is populated depends on the method, and this surface passes both
	// through without inspecting either, exactly as the adapter contract says.
	PayURL   string `json:"pay_url,omitempty"`
	PayToken string `json:"pay_token,omitempty"`
	OrderID  string `json:"provider_order_id,omitempty"`
}

// Pay starts a collection for the tenant's own tenancy.
//
// It calls the same service a manager's collection calls. That is the point: one
// idempotency contract, one adapter, one webhook path, and no second way for
// money to enter the ledger that behaves subtly differently because a tenant
// started it.
func (h *Handler) Pay(w http.ResponseWriter, r *http.Request) {
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}

	var req payRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	if req.AmountMinor <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "amount_minor must be a positive number of paise")
		return
	}
	if req.IdempotencyKey == "" {
		writeError(w, http.StatusUnprocessableEntity,
			"idempotency_key is required — without one a retry on a bad connection is a second payment")
		return
	}
	method := req.Method
	if method == "" {
		method = "upi_intent"
	}

	ctx := session.Scope(r.Context(), res)
	t, err := h.leases.Tenancy(ctx, res.LeaseID, effective.DateOf(h.now(), ist))
	if err != nil {
		h.fail(w, "reading a tenancy before paying", res.LeaseID, err)
		return
	}

	payment, order, err := h.payments.Collect(ctx, moneyservice.CollectRequest{
		TenantID: res.TenantID.String(),
		// The place and the tenancy come from the lease the session resolved,
		// never from the request body: a payment that could name its own lease
		// would be one anybody could post against anybody.
		Lease: res.LeaseID,
		// PayerID is the session's, for the same reason.
		PayerID:      session.PartyID,
		PayerName:    t.UnitCode + ", " + t.PropertyName,
		PayerContact: session.Phone,
		Property:     t.PropertyID,
		Unit:         t.UnitID,
		Amount:       domain.Minor(req.AmountMinor),
		Method:       collect.Method(method),
		Reference:    "Rent, " + t.UnitCode,
		// Scoped to the renter so two tenants cannot collide on a key, and to
		// the lease so the same key on two tenancies is two payments.
		IdempotencyKey: "resident:" + session.PartyID + ":" + res.LeaseID + ":" + req.IdempotencyKey,
	})
	switch {
	case err == nil:
	case errors.Is(err, provider.ErrUnavailable):
		// The payment exists and the gateway did not answer. 503 rather than
		// 500, because the honest instruction is "try again in a moment" and the
		// retry with the same key resolves to the same collection.
		h.log.Error("provider unavailable for a tenant payment", "lease", res.LeaseID, "error", err)
		writeError(w, http.StatusServiceUnavailable,
			"the payment provider did not answer — try again in a moment")
		return
	default:
		h.log.Error("starting a tenant payment", "lease", res.LeaseID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not start the payment")
		return
	}

	writeJSON(w, http.StatusCreated, payResponse{
		PaymentID: payment.ID, Status: string(payment.Status),
		PayURL: order.PayURL, PayToken: order.PayToken,
		OrderID: order.ProviderOrderID,
	})
}

type receiptResponse struct {
	Number            string `json:"receipt_number"`
	PaymentID         string `json:"payment_id"`
	LeaseID           string `json:"lease_id"`
	AmountMinor       int64  `json:"amount_minor"`
	Currency          string `json:"currency"`
	Method            string `json:"method"`
	Provider          string `json:"provider"`
	ProviderReference string `json:"provider_reference,omitempty"`
	EntryID           string `json:"entry_id"`
	Status            string `json:"status"`
	ReceivedAt        string `json:"received_at"`

	// Payee, unit and address are on the receipt because a receipt with no payee
	// proves nothing. A tenant produces this for an employer's HRA claim or a
	// landlord's dispute, and both ask who was paid.
	Payee    string `json:"payee"`
	Unit     string `json:"unit"`
	Property string `json:"property"`
	Locality string `json:"locality,omitempty"`
	City     string `json:"city,omitempty"`
}

// Receipt is the proof of one payment.
func (h *Handler) Receipt(w http.ResponseWriter, r *http.Request) {
	session, res, ok := h.residency(w, r)
	if !ok {
		return
	}
	paymentID := r.PathValue("payment")
	if paymentID == "" {
		writeError(w, http.StatusBadRequest, "no payment named")
		return
	}

	ctx := session.Scope(r.Context(), res)
	rec, err := h.statements.Receipt(ctx, paymentID)
	switch {
	case err == nil:
	case errors.Is(err, moneyservice.ErrNoReceipt):
		// 409 rather than 404: the payment is theirs and is not yet money.
		writeError(w, http.StatusConflict, "that payment has not been received yet")
		return
	default:
		// Anything else — including a payment belonging to somebody else, which
		// the policy reports as absent — is "no such receipt".
		h.log.Info("receipt not available", "lease", res.LeaseID, "error", err)
		writeError(w, http.StatusNotFound, "no such receipt")
		return
	}
	// A receipt reached through one tenancy must be a receipt *of* that tenancy.
	// The policy already refuses another renter's payment; this refuses the
	// renter's own payment on their other lease, which the policy would allow
	// and which would put the wrong address on the document.
	if rec.LeaseID != res.LeaseID {
		writeError(w, http.StatusNotFound, "no such receipt")
		return
	}

	t, err := h.leases.Tenancy(ctx, res.LeaseID, effective.DateOf(rec.ReceivedAt, ist))
	if err != nil {
		h.fail(w, "reading a tenancy for a receipt", res.LeaseID, err)
		return
	}

	writeJSON(w, http.StatusOK, receiptResponse{
		Number: rec.Number, PaymentID: rec.PaymentID, LeaseID: rec.LeaseID,
		AmountMinor: int64(rec.AmountMinor), Currency: rec.Currency,
		Method: string(rec.Method), Provider: rec.Provider,
		ProviderReference: rec.ProviderReference, EntryID: rec.EntryID,
		Status: string(rec.Status), ReceivedAt: rec.ReceivedAt.Format(time.RFC3339),
		Payee: res.Organisation, Unit: t.UnitCode, Property: t.PropertyName,
		Locality: t.Locality, City: t.City,
	})
}

// residency resolves the lease in the path against the session, and answers for
// the caller when it is not theirs.
//
// This is the story's failure scenario, and the answer to it is 404 rather than
// 403: 403 confirms the lease exists, and a person changing the identifier in a
// URL is told nothing by "not found". The attempt is logged with the party that
// made it, so the same probe across a hundred ids is visible.
func (h *Handler) residency(w http.ResponseWriter, r *http.Request) (identityservice.Session, identityservice.Residency, bool) {
	session, ok := identityservice.SessionFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return identityservice.Session{}, identityservice.Residency{}, false
	}
	leaseID := r.PathValue("lease")
	res, ok := session.Residency(leaseID)
	if !ok {
		// Logged rather than written to audit_events, and the distinction is not
		// laziness: audit_events.tenant_id is NOT NULL, and a denied attempt
		// belongs to no organisation — attributing it to one would put a false
		// record in some landlord's audit trail.
		h.log.Warn("a renter asked for a tenancy that is not theirs",
			"event", "resident.access_denied", "party", session.PartyID,
			"requested_lease", leaseID, "path", r.URL.Path,
			"tenancies", len(session.Residencies))
		writeError(w, http.StatusNotFound, "no such tenancy")
		return identityservice.Session{}, identityservice.Residency{}, false
	}
	return session, res, true
}

func (h *Handler) fail(w http.ResponseWriter, what, lease string, err error) {
	switch {
	case errors.Is(err, leasestore.ErrNoLease):
		writeError(w, http.StatusNotFound, "no such tenancy")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "sign in again")
	default:
		h.log.Error(what, "lease", lease, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read your tenancy")
	}
}

const dateLayout = "2006-01-02"

func present(t leaseservice.Tenancy, res identityservice.Residency, _ effective.Date) tenancyResponse {
	out := tenancyResponse{
		LeaseID: t.LeaseID, State: string(t.State), Live: t.Live(),
		Organisation: res.Organisation,
		Property:     t.PropertyName, Unit: t.UnitCode,
		Locality: t.Locality, City: t.City,
		StartOn: t.Term.From().String(), NoticeDays: t.NoticeDays,
		RentMinor: t.RentMinor, DueDay: t.DueDay, Currency: "INR",
	}
	if !t.Term.To().Zero() {
		out.EndOn = t.Term.To().String()
	}
	if !t.EndedOn.Zero() {
		out.EndedOn = t.EndedOn.String()
	}
	if !t.LockInUntil.Zero() {
		out.LockInUntil = t.LockInUntil.String()
	}
	if !t.NoticeServedOn.Zero() {
		out.NoticeServedOn = t.NoticeServedOn.String()
	}
	if !t.NoticeMoveOutOn.Zero() {
		out.NoticeMoveOutOn = t.NoticeMoveOutOn.String()
	}
	return out
}

func presentDues(s moneyservice.Statement, asOf effective.Date) *duesResponse {
	return &duesResponse{
		DueMinor: int64(s.Due), RentMinor: int64(s.Rent),
		LateFeeMinor: int64(s.LateFee), AdjustmentMinor: int64(s.Adjustment),
		PaidMinor: int64(s.Paid), AdvanceMinor: int64(s.Advance),
		DepositMinor: int64(s.Deposit), AsOf: asOf.String(),
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	// Unknown fields are refused rather than ignored, as everywhere: a client
	// sending "amount" when the field is "amount_minor" would otherwise pay zero
	// and be told it worked.
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request: "+err.Error())
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	// The tenant view is per-person and must never be held by a shared cache —
	// a CDN or a corporate proxy serving one renter's dues to the next is the
	// cheapest possible way to leak this data.
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
