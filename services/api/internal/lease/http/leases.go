// Package http is the lease module's HTTP surface.
//
// Two routes: create a lease, and start the tenancy. They are separate because
// the states are separate (ADR-0010) and because the second is where the
// no-double-let constraint and ADR-0024's tax gate fire — a single
// create-and-activate call would have to report two quite different refusals
// through one response.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	leasedomain "github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// maxBody is a ceiling on a lease request. A lease with parties and terms is a
// few kilobytes; anything near this is not one.
const maxBody = 256 << 10

// Leases handles the lease routes.
type Leases struct {
	leases *service.Leases
	log    *slog.Logger
}

// NewLeases builds the handler.
func NewLeases(s *service.Leases, log *slog.Logger) *Leases {
	return &Leases{leases: s, log: log}
}

// Routes mounts the module.
func (h *Leases) Routes(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/leases", h.Create)
	mux.HandleFunc("POST /v1/leases/{id}/activate", h.Activate)
}

// createRequest is the wire shape.
//
// Money is `amount_minor` and an integer, everywhere, including here — ADR-0007
// applies to API payloads and not only to the ledger, because a rupee that
// becomes a float at the edge is a rupee that arrives wrong.
type createRequest struct {
	PropertyID  string `json:"property_id"`
	UnitID      string `json:"unit_id"`
	StartOn     string `json:"start_on"`
	EndOn       string `json:"end_on"`
	NoticeDays  int    `json:"notice_days"`
	LockInUntil string `json:"lock_in_until"`

	RentMinor    int64  `json:"rent_amount_minor"`
	Cycle        string `json:"cycle"`
	DueDay       int    `json:"due_day"`
	DepositMinor int64  `json:"deposit_amount_minor"`
	DepositHeld  string `json:"deposit_held_by"`

	Parties []partyRequest `json:"parties"`
	Tax     *taxRequest    `json:"tax"`
}

type partyRequest struct {
	PartyID string `json:"party_id"`
	Role    string `json:"role"`
	Name    string `json:"name"`
	Phone   string `json:"phone"`
	Email   string `json:"email"`
}

// taxRequest is ADR-0024's two facts. Optional on a draft, and the tenancy will
// not start without them.
type taxRequest struct {
	DeductorClass     string `json:"deductor_class"`
	LandlordResidency string `json:"landlord_residency"`
	Source            string `json:"source"`
	AcknowledgedOn    string `json:"acknowledged_on"`
	AcknowledgedBy    string `json:"acknowledged_by"`
}

type createResponse struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Event string `json:"event"`
}

// Create writes a lease in draft.
func (h *Leases) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decode(w, r, &req); err != nil {
		return
	}

	draft, err := req.draft()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// The organisation comes from the session, never from the body. A lease that
	// could name its own tenant_id would be one anybody could file against
	// anybody: the isolation policy would still refuse the write, but the refusal
	// would arrive as a policy error rather than as a rejected request.
	tenant, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
		return
	}
	draft.TenantID = tenant.String()

	out, err := h.leases.Create(r.Context(), draft)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, createResponse{
			ID: out.ID, State: string(out.Lease.State), Event: string(out.Event)})
	case errors.Is(err, leasedomain.ErrDraft),
		errors.Is(err, leasedomain.ErrTerms),
		errors.Is(err, tds.ErrFacts):
		// The caller sent something that cannot be a lease. Named back to them,
		// because "invalid request" is not a thing anybody can fix.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("creating a lease", "error", err)
		writeError(w, http.StatusInternalServerError, "could not create the lease")
	}
}

// Activate starts the tenancy.
//
// The two refusals it can give are the interesting part, and they are different
// enough that flattening them would make the API useless: one names another
// tenancy, the other names a missing form.
func (h *Leases) Activate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "no lease named")
		return
	}

	err := h.leases.Activate(r.Context(), id, leasedomain.ActorOwner)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{
			"id": id, "state": string(leasedomain.StateActive),
			"event": string(leasedomain.EventStarted)})
	case errors.Is(err, store.ErrDoubleLet):
		// 409: the request is fine and the world disagrees with it. The message
		// names the conflicting lease.
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNoLease):
		writeError(w, http.StatusNotFound, "no such lease awaiting signature")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	case isTaxRefusal(err):
		// 422 rather than 409: nothing about the world is in the way, something
		// about the lease is missing. The message is the database's, which states
		// what is missing and why it matters.
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		h.log.Error("activating a lease", "lease", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not start the tenancy")
	}
}

// isTaxRefusal recognises ADR-0024's deferred trigger, which arrives as a
// constraint violation with the schema's own sentence in it.
func isTaxRefusal(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "TDS section") || strings.Contains(msg, "acknowledged")
}

// draft turns the wire shape into the domain's, and refuses what cannot be
// converted rather than defaulting it.
func (r createRequest) draft() (leasedomain.Draft, error) {
	start, err := effective.ParseDate(r.StartOn)
	if err != nil {
		return leasedomain.Draft{}, errors.New("start_on must be a date, as YYYY-MM-DD")
	}
	var term effective.Interval
	if r.EndOn == "" {
		term, err = effective.Since(start)
	} else {
		var end effective.Date
		if end, err = effective.ParseDate(r.EndOn); err == nil {
			term, err = effective.Between(start, end)
		}
	}
	if err != nil {
		return leasedomain.Draft{}, errors.New("end_on must be a date after start_on")
	}

	d := leasedomain.Draft{
		Property: r.PropertyID, Unit: r.UnitID,
		Term: term, NoticeDays: r.NoticeDays,
		Terms: leasedomain.Terms{
			RentMinor: r.RentMinor, Cycle: leasedomain.Cycle(r.Cycle),
			DueDay: leasedomain.DueDay(r.DueDay), DepositMinor: r.DepositMinor,
			DepositHeldBy: leasedomain.DepositHolder(r.DepositHeld),
		},
	}
	if r.LockInUntil != "" {
		if d.LockInUntil, err = effective.ParseDate(r.LockInUntil); err != nil {
			return leasedomain.Draft{}, errors.New("lock_in_until must be a date, as YYYY-MM-DD")
		}
	}
	for _, p := range r.Parties {
		d.Parties = append(d.Parties, leasedomain.Party{
			PartyID: p.PartyID, Role: leasedomain.PartyRole(p.Role),
			Name: p.Name, Phone: p.Phone, Email: p.Email,
		})
	}
	if r.Tax != nil {
		if d.Tax, err = r.Tax.history(start); err != nil {
			return leasedomain.Draft{}, err
		}
	}
	return d, nil
}

// history builds the effective-dated facts from the flat wire shape. One set,
// effective from the tenancy's start — a residency that changes later is a
// separate call, not something a create request can express.
func (t taxRequest) history(from effective.Date) (tds.History, error) {
	f := tds.Facts{
		Deductor:       tds.DeductorClass(t.DeductorClass),
		Residency:      tds.Residency(t.LandlordResidency),
		From:           from,
		Source:         t.Source,
		AcknowledgedBy: t.AcknowledgedBy,
	}
	if t.AcknowledgedOn != "" {
		on, err := effective.ParseDate(t.AcknowledgedOn)
		if err != nil {
			return tds.History{}, errors.New("tax.acknowledged_on must be a date, as YYYY-MM-DD")
		}
		f.AcknowledgedOn = on
	}
	iv, err := effective.Since(from)
	if err != nil {
		return tds.History{}, err
	}
	return tds.NewHistory([]effective.Record[tds.Facts]{
		{ID: "1", Range: iv, Kind: effective.KindChange, Value: f},
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	// Unknown fields are refused rather than ignored: a client sending
	// "rentAmount" when the field is "rent_amount_minor" would otherwise create a
	// lease at zero rent and be told it worked.
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request: "+err.Error())
		return err
	}
	return nil
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
