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
	"encoding/json"
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

// rosterLimit bounds Leases.Live the way historyLimit bounds the resident
// surface — a firm's whole live roster, not paged, because the screen this
// serves is the manager's daily worklist and not an archive.
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
	r.Handle("GET /v1/ops/arrears", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Arrears)
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

// arrearResponse is one live tenancy and what it owes today — the
// collections worklist. Zero DueMinor tenancies are included: a manager
// scanning for who is about to fall behind needs the full roster, not only
// the ones already in arrears.
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

// Arrears reads the firm's live roster and what each tenancy currently
// owes — one Tenancy, one PartiesOf and one Position call per lease, the
// same per-record shape internal/surface/resident.Tenancies uses for its
// own reason: each read is scoped to the one lease it concerns, so no query
// in this product spans two tenancies' money in one round trip.
func (h *Handler) Arrears(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := effective.DateOf(h.now(), ist)

	live, err := h.leases.Live(ctx, rosterLimit)
	if err != nil {
		h.log.Error("reading the live roster", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the roster")
		return
	}

	out := make([]arrearResponse, 0, len(live))
	for _, l := range live {
		t, err := h.leases.Tenancy(ctx, l.ID, today)
		if err != nil {
			h.log.Warn("reading a tenancy on the roster", "lease", l.ID, "error", err)
			continue
		}
		tenantParty, _, err := h.leases.PartiesOf(ctx, l.ID, today)
		if err != nil {
			h.log.Warn("reading a tenancy's parties", "lease", l.ID, "error", err)
			continue
		}
		item := arrearResponse{
			LeaseID: l.ID, Property: t.PropertyName, Unit: t.UnitCode, Locality: t.Locality,
			RentMinor: t.RentMinor, AsOf: today.String(),
		}
		if tenantParty != "" {
			if phone, email, err := h.residents.Contact(ctx, tenantParty); err == nil {
				item.Phone, item.Email = phone, email
			}
			if due, err := h.statements.Position(ctx, l.ID, tenantParty); err == nil {
				item.DueMinor = int64(due.Due)
			}
		}
		out = append(out, item)
	}
	writeJSON(w, http.StatusOK, map[string]any{"arrears": out})
}

// todayResponse is the collection roster's headline numbers.
type todayResponse struct {
	AsOf              string `json:"as_of"`
	ActiveTenancies   int    `json:"active_tenancies"`
	RentRollMinor     int64  `json:"rent_roll_amount_minor"`
	OutstandingMinor  int64  `json:"outstanding_amount_minor"`
	TenanciesInArrear int    `json:"tenancies_in_arrears"`
}

// Today sums the roster Arrears reads into the figures a manager's morning
// screen wants. It is the same reads, not a separate query — rent_roll and
// outstanding are what the roster already carries, not a period-scoped
// ledger question, so they are named for what they are: the position today,
// not "collected this month" (which needs a period boundary this surface
// does not ask about yet).
func (h *Handler) Today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := effective.DateOf(h.now(), ist)

	live, err := h.leases.Live(ctx, rosterLimit)
	if err != nil {
		h.log.Error("reading the live roster", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the roster")
		return
	}

	out := todayResponse{AsOf: today.String(), ActiveTenancies: len(live)}
	for _, l := range live {
		t, err := h.leases.Tenancy(ctx, l.ID, today)
		if err != nil {
			continue
		}
		out.RentRollMinor += t.RentMinor
		tenantParty, _, err := h.leases.PartiesOf(ctx, l.ID, today)
		if err != nil || tenantParty == "" {
			continue
		}
		due, err := h.statements.Position(ctx, l.ID, tenantParty)
		if err != nil {
			continue
		}
		if due.Due > 0 {
			out.OutstandingMinor += int64(due.Due)
			out.TenanciesInArrear++
		}
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
