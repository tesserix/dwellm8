// Package ops is the management firm's view of the portfolios it runs — the
// `ops` app's backend. Sibling to internal/surface/owner: same composition
// seam (ADR-0001 §3), the manager's audience instead of the owner's.
//
// Phase 1 on purpose: the portfolio, an org-wide arrears list and the
// collection roster are real reads over the property, lease and money
// services. Tickets, vendors, inspections, gate passes, payouts and leads —
// everything apps/pm/src/data/mock.ts calls a ticket, a vendor or an
// inspection — have no schema behind them yet (no maintenance-ticket or
// vendor-panel tables exist as of this surface), so this package does not
// serve them. Building a shallow version of any of those would be a fake
// API standing in front of real ones, which is worse than the mobile app's
// existing demonstration data saying plainly that it is demonstration data.
package ops

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	communityservice "github.com/tesserix/dwellm8/services/api/internal/community/service"
	identityservice "github.com/tesserix/dwellm8/services/api/internal/identity/service"
	leaseservice "github.com/tesserix/dwellm8/services/api/internal/lease/service"
	maintenanceservice "github.com/tesserix/dwellm8/services/api/internal/maintenance/service"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/activity"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

var ist = time.FixedZone("IST", 5*3600+1800)

// rosterLimit bounds the arrears list, which is now ordered by what is owed:
// the cut falls on the smallest debts rather than on an arbitrary slice of the
// roster, and the tiles above it are aggregates over every tenancy (#306).
const rosterLimit = 500
const activityLimit = 100

// Handler serves the ops surface.
type Handler struct {
	properties  *propertyservice.Properties
	leases      *leaseservice.Leases
	statements  *moneyservice.Statements
	residents   *identityservice.Residents
	activity    activity.Feeder
	tickets     *maintenanceservice.Tickets
	community   *communityservice.Community
	owners      *identityservice.Owners
	merchants   *moneyservice.Merchants
	settlements *moneyservice.Settlements
	payments    *moneyservice.Payments

	registrations *identityservice.Registrations
	log           *slog.Logger
	now           func() time.Time
}

// New wires the surface to the services it composes.
func New(p *propertyservice.Properties, l *leaseservice.Leases, s *moneyservice.Statements,
	residents *identityservice.Residents, feed activity.Feeder, log *slog.Logger, now func() time.Time) *Handler {
	if now == nil {
		now = time.Now
	}
	return &Handler{properties: p, leases: l, statements: s, residents: residents, activity: feed, log: log, now: now}
}

// Routes mounts the surface.
func (h *Handler) Routes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/properties", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Properties)
	r.Handle("GET /v1/ops/properties/{id}", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Property)
	r.Handle("GET /v1/ops/arrears", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Arrears)
	r.Handle("GET /v1/ops/tenancies", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Tenancies)
	r.Handle("GET /v1/ops/tenancies/{lease}/position", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Position)
	r.Handle("GET /v1/ops/today", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Today)
	r.Handle("GET /v1/ops/activity", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Activity)
}

// propertyResponse is one property on the firm's portfolio.
type propertyResponse struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	AddressLine1 string `json:"address_line1"`
	Locality     string `json:"locality"`
	City         string `json:"city"`
	UnitCount    int    `json:"unit_count"`
}

// Properties lists every property this session's organisation holds —
// everything under management, the same RLS-scoped read the Own app's
// portfolio uses, asked by the firm's own session instead of an owner's.
func (h *Handler) Properties(w http.ResponseWriter, r *http.Request) {
	props, err := h.properties.List(r.Context())
	if err != nil {
		h.log.Error("listing properties", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the portfolio")
		return
	}
	out := make([]propertyResponse, 0, len(props))
	for _, p := range props {
		out = append(out, propertyResponse{
			ID: p.ID, Code: p.Code, Name: p.Name, Kind: p.Kind,
			AddressLine1: p.AddressLine1, Locality: p.Locality, City: p.City, UnitCount: p.UnitCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"properties": out})
}

// unitResponse is one lettable unit and, when it is let, the tenancy in it.
// The tenancy fields are empty on a vacant unit rather than absent, so the
// screen renders one row shape either way.
type unitResponse struct {
	ID        string `json:"id"`
	Code      string `json:"code"`
	Kind      string `json:"kind"`
	Floor     int    `json:"floor"`
	Occupancy string `json:"occupancy"`
	LeaseID   string `json:"lease_id,omitempty"`
	Tenant    string `json:"tenant,omitempty"`
	RentMinor int64  `json:"rent_amount_minor,omitempty"`
	LeaseEnds string `json:"lease_ends,omitempty"`
	DueMinor  int64  `json:"due_amount_minor,omitempty"`
	// LetFrom is set on a unit nobody lives in yet whose tenancy is signed and
	// starts later. Vacant with a date on it is not a unit to re-let (#304).
	LetFrom string `json:"let_from,omitempty"`
}

// Property is the record a manager opens on site: the building, its lettable
// units, and per let unit who is in it and on what terms. One lease read per
// unit, the same per-record shape as Arrears and for the same reason.
func (h *Handler) Property(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := r.PathValue("id")

	p, err := h.properties.Get(ctx, id)
	if errors.Is(err, propertyservice.ErrNoProperty) {
		writeError(w, http.StatusNotFound, "no such property")
		return
	}
	if err != nil {
		h.log.Error("reading a property", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the property")
		return
	}

	units, err := h.properties.Units(ctx, id)
	if err != nil {
		h.log.Error("reading a property's units", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the units")
		return
	}

	today := effective.DateOf(h.now(), ist)
	out := make([]unitResponse, 0, len(units))
	for _, u := range units {
		item := unitResponse{ID: u.ID, Code: u.Code, Kind: u.Kind, Floor: u.Floor, Occupancy: u.Occupancy}
		leaseID, err := h.leases.ActiveOnUnit(ctx, u.ID, today)
		if err != nil {
			h.log.Warn("reading the tenancy on a unit", "unit", u.ID, "error", err)
		}
		if leaseID != "" {
			// A live tenancy outranks the register's own column, which nothing
			// updates when a lease starts and which is stale wherever it differs.
			item.LeaseID, item.Occupancy = leaseID, "occupied"
			if t, err := h.leases.Tenancy(ctx, leaseID, today); err == nil {
				item.RentMinor = t.RentMinor
				if !t.Term.To().Zero() {
					item.LeaseEnds = t.Term.To().String()
				}
			}
			if tenantParty, _, err := h.leases.PartiesOf(ctx, leaseID, today); err == nil && tenantParty != "" {
				if profile, err := h.residents.Profile(ctx, tenantParty); err == nil {
					item.Tenant = profile.DisplayName
				}
				if due, err := h.statements.Position(ctx, leaseID, tenantParty); err == nil {
					item.DueMinor = int64(due.Due)
				}
			}
		} else if next, from, err := h.leases.NextOnUnit(ctx, u.ID, today); err != nil {
			h.log.Warn("reading the next tenancy on a unit", "unit", u.ID, "error", err)
		} else if next != "" {
			item.LeaseID, item.LetFrom = next, from.String()
			if t, err := h.leases.Tenancy(ctx, next, from); err == nil {
				item.RentMinor = t.RentMinor
				if !t.Term.To().Zero() {
					item.LeaseEnds = t.Term.To().String()
				}
			}
			if tenantParty, _, err := h.leases.PartiesOf(ctx, next, from); err == nil && tenantParty != "" {
				if profile, err := h.residents.Profile(ctx, tenantParty); err == nil {
					item.Tenant = profile.DisplayName
				}
			}
		}
		out = append(out, item)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"property": propertyResponse{
			ID: p.ID, Code: p.Code, Name: p.Name, Kind: p.Kind,
			AddressLine1: p.AddressLine1, Locality: p.Locality, City: p.City, UnitCount: p.UnitCount,
		},
		"units": out,
	})
}

// arrearResponse is one live tenancy and what it owes today. Arrears carries
// only the tenancies that owe something; Tenancies carries the whole roster in
// the same shape, including the ones that are square.
type arrearResponse struct {
	LeaseID   string `json:"lease_id"`
	Property  string `json:"property"`
	Unit      string `json:"unit"`
	Locality  string `json:"locality"`
	Phone     string `json:"phone,omitempty"`
	Email     string `json:"email,omitempty"`
	RentMinor int64  `json:"rent_amount_minor"`
	DueMinor  int64  `json:"due_amount_minor"`
	AsOf      string `json:"as_of"`
}

// Arrears is who owes money, most owed first.
//
// The debts come from one ledger query over the organisation, not from walking
// the roster lease by lease: read that way the list had to be cut at a page,
// and the page was in identifier order, so a firm past five hundred tenancies
// got five hundred arbitrary ones and its real arrears were not among them
// (#306). Only the tenancies on the list are then described — a tenancy that
// owes nothing costs no query at all.
func (h *Handler) Arrears(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := effective.DateOf(h.now(), ist)

	owing, err := h.statements.Outstanding(ctx)
	if err != nil {
		h.log.Error("reading what the firm is owed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the roster")
		return
	}
	if len(owing) > rosterLimit {
		owing = owing[:rosterLimit]
	}

	out := make([]arrearResponse, 0, len(owing))
	for _, d := range owing {
		item, err := h.arrear(ctx, d.LeaseID, today)
		if err != nil {
			h.log.Warn("describing a tenancy in arrears", "lease", d.LeaseID, "error", err)
			continue
		}
		item.DueMinor = int64(d.Due)
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"arrears": out})
}

// Tenancies is the whole live roster with each tenancy's position, for the
// manager scanning for who is about to fall behind rather than who already has.
//
// Arrears cannot serve this: it lists only what is owed, so a firm whose
// tenancies are all square read as a firm managing nothing (#313).
func (h *Handler) Tenancies(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := effective.DateOf(h.now(), ist)

	live, err := h.leases.Live(ctx, rosterLimit)
	if err != nil {
		h.log.Error("reading the live roster", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the roster")
		return
	}
	owing, err := h.statements.Outstanding(ctx)
	if err != nil {
		h.log.Error("reading what the firm is owed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the roster")
		return
	}
	due := make(map[string]int64, len(owing))
	for _, d := range owing {
		due[d.LeaseID] = int64(d.Due)
	}

	out := make([]arrearResponse, 0, len(live))
	for _, l := range live {
		item, err := h.arrear(ctx, l.ID, today)
		if err != nil {
			h.log.Warn("describing a tenancy on the roster", "lease", l.ID, "error", err)
			continue
		}
		item.DueMinor = due[l.ID]
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenancies": out})
}

// Position is one tenancy's rent and what it owes, so a receipt or a collection
// screen can read the tenancy in front of it without downloading the firm's
// whole arrears list to find one row (#306).
func (h *Handler) Position(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := effective.DateOf(h.now(), ist)
	leaseID := r.PathValue("lease")

	item, err := h.arrear(ctx, leaseID, today)
	if err != nil {
		// Not theirs and not there are the same answer, as everywhere else.
		writeError(w, http.StatusNotFound, "no such tenancy")
		return
	}
	tenantParty, _, err := h.leases.PartiesOf(ctx, leaseID, today)
	if err == nil && tenantParty != "" {
		if due, err := h.statements.Position(ctx, leaseID, tenantParty); err == nil {
			item.DueMinor = int64(due.Due)
		}
	}
	writeJSON(w, http.StatusOK, item)
}

// arrear describes one tenancy for the arrears surface, without its debt: who
// lives there, where, and on what rent.
func (h *Handler) arrear(ctx context.Context, leaseID string, today effective.Date) (arrearResponse, error) {
	t, err := h.leases.Tenancy(ctx, leaseID, today)
	if err != nil {
		return arrearResponse{}, err
	}
	item := arrearResponse{
		LeaseID: leaseID, Property: t.PropertyName, Unit: t.UnitCode, Locality: t.Locality,
		RentMinor: t.RentMinor, AsOf: today.String(),
	}
	if tenantParty, _, err := h.leases.PartiesOf(ctx, leaseID, today); err == nil && tenantParty != "" {
		if phone, email, err := h.residents.Contact(ctx, tenantParty); err == nil {
			item.Phone, item.Email = phone, email
		}
	}
	return item, nil
}

// todayResponse is the collection roster's headline numbers.
type todayResponse struct {
	AsOf            string `json:"as_of"`
	ActiveTenancies int    `json:"active_tenancies"`
	// StartingTenancies is signed but not begun. Counted apart from the active
	// ones, whose rent is in force and whose money the tile is about (#305).
	StartingTenancies int   `json:"starting_tenancies"`
	RentRollMinor     int64 `json:"rent_roll_amount_minor"`
	OutstandingMinor  int64 `json:"outstanding_amount_minor"`
	TenanciesInArrear int   `json:"tenancies_in_arrears"`
}

// Today is the manager's morning screen: how many tenancies are live, what
// rent they carry, and what is unpaid.
//
// Two aggregate queries over the whole book, not a walk of it. The tile is a
// total, and a total assembled from a page of the roster was quietly the first
// five hundred tenancies by identifier (#306). Named for what they are — the
// position today, not "collected this month", which needs a period boundary
// this surface does not ask about yet.
func (h *Handler) Today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := effective.DateOf(h.now(), ist)

	roster, err := h.leases.Roster(ctx, today)
	if err != nil {
		h.log.Error("counting the live roster", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the roster")
		return
	}
	owing, err := h.statements.Outstanding(ctx)
	if err != nil {
		h.log.Error("reading what the firm is owed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the roster")
		return
	}

	out := todayResponse{
		AsOf:            today.String(),
		ActiveTenancies: roster.Active, StartingTenancies: roster.Starting,
		RentRollMinor: roster.RentRollMinor,
	}
	for _, d := range owing {
		out.OutstandingMinor += int64(d.Due)
		out.TenanciesInArrear++
	}
	writeJSON(w, http.StatusOK, out)
}

// activityEntry mirrors the owner surface's — same audience shape, the
// firm's own organisation feed.
type activityEntry struct {
	Kind       string `json:"kind"`
	OccurredAt string `json:"occurred_at"`
	Body       string `json:"body,omitempty"`
}

// Activity is the whole-organisation feed.
func (h *Handler) Activity(w http.ResponseWriter, r *http.Request) {
	if h.activity == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	entries, err := h.activity.Feed(r.Context(), activity.Query{
		Audience: activity.AudienceOrg, Limit: activityLimit,
	})
	if err != nil {
		h.log.Error("reading activity", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read activity")
		return
	}
	out := make([]activityEntry, 0, len(entries))
	for _, e := range entries {
		out = append(out, activityEntry{Kind: e.Kind, OccurredAt: e.OccurredAt.Format(time.RFC3339), Body: e.Body})
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": out})
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store, private")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

// writeFieldError refuses one field by name, so a form can put the reason where
// the value was typed instead of at the bottom of the screen (#287).
func writeFieldError(w http.ResponseWriter, code int, field, msg string) {
	writeJSON(w, code, map[string]string{"error": msg, "field": field})
}
