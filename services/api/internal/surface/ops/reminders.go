package ops

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
)

// What is about to happen on the book (#337).
//
// The manager's other lists answer for the past: arrears is who already owes,
// today is what the book is worth this morning. Neither says that the rent on
// eleven flats falls due next Tuesday, or that a term runs out inside its own
// notice period — and chasing money after it is late is the thing the reminder
// ladder exists to avoid.

const (
	defaultHorizon = 30
	maxHorizon     = 365
)

// A reminder is one dated thing a manager should act on before it happens.
type reminderResponse struct {
	Kind       string `json:"kind"`
	LeaseID    string `json:"lease_id"`
	PropertyID string `json:"property_id,omitempty"`
	Property   string `json:"property"`
	Unit       string `json:"unit"`
	Locality   string `json:"locality,omitempty"`
	// On is the day it falls due or the day the term ends. Overdue rent is
	// dated today: it is what to do now, and sorting it anywhere else would put
	// the money already lost below the money not yet owed.
	On          string `json:"on"`
	DaysAway    int    `json:"days_away"`
	AmountMinor int64  `json:"amount_minor,omitempty"`
	// InsideNoticeWindow says notice can still be served in time — the fact a
	// renewal decision turns on.
	InsideNoticeWindow bool `json:"inside_notice_window,omitempty"`
}

// Reminders is the manager's forward view: rent falling due, rent already
// overdue, and tenancies running out, over the whole book, soonest first.
func (h *Handler) Reminders(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	today := effective.DateOf(h.now(), ist)
	days := horizon(r.URL.Query().Get("days"))
	through := today.AddDays(days)

	due, err := h.leases.Upcoming(ctx, today, through, rosterLimit)
	if err != nil {
		h.log.Error("reading what falls due", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the reminders")
		return
	}
	owing, err := h.statements.Outstanding(ctx)
	if err != nil {
		h.log.Error("reading what the firm is owed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the reminders")
		return
	}
	ending, err := h.leases.Expiring(ctx, days)
	if err != nil {
		h.log.Error("reading the tenancies running out", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the reminders")
		return
	}

	out := make([]reminderResponse, 0, len(due)+len(owing)+len(ending))
	for _, d := range due {
		out = append(out, reminderResponse{
			Kind: "rent_due", LeaseID: d.LeaseID, PropertyID: d.PropertyID,
			Property: d.PropertyName, Unit: d.UnitCode, Locality: d.Locality,
			On: d.DueOn.String(), DaysAway: daysBetween(today, d.DueOn), AmountMinor: d.AmountMinor,
		})
	}

	// Where the overdue and the ending tenancies are needs one read each. The
	// debts are already capped by Outstanding's own order, and a tenancy ending
	// is bounded by the horizon, so neither list walks the book.
	if len(owing) > rosterLimit {
		owing = owing[:rosterLimit]
	}
	for _, d := range owing {
		t, err := h.leases.Tenancy(ctx, d.LeaseID, today)
		if err != nil {
			h.log.Warn("describing an overdue tenancy", "lease", d.LeaseID, "error", err)
			continue
		}
		out = append(out, reminderResponse{
			Kind: "rent_overdue", LeaseID: d.LeaseID, PropertyID: t.PropertyID,
			Property: t.PropertyName, Unit: t.UnitCode, Locality: t.Locality,
			On: today.String(), DaysAway: 0, AmountMinor: int64(d.Due),
		})
	}
	for _, e := range ending {
		t, err := h.leases.Tenancy(ctx, e.LeaseID, today)
		if err != nil {
			h.log.Warn("describing a tenancy running out", "lease", e.LeaseID, "error", err)
			continue
		}
		out = append(out, reminderResponse{
			Kind: "tenancy_ending", LeaseID: e.LeaseID, PropertyID: t.PropertyID,
			Property: t.PropertyName, Unit: t.UnitCode, Locality: t.Locality,
			On: e.EndsOn.String(), DaysAway: e.DaysRemaining,
			InsideNoticeWindow: e.InsideNoticeWindow,
		})
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].On < out[j].On })
	writeJSON(w, http.StatusOK, map[string]any{
		"as_of": today.String(), "horizon_days": days, "reminders": out,
	})
}

// daysBetween counts whole days from one date to another.
func daysBetween(from, to effective.Date) int {
	return int(to.Time().Sub(from.Time()).Hours() / 24)
}

// horizon reads how far ahead to look, bounded: a window of a decade is a
// reminder list nobody reads, and one of zero days is an empty screen.
func horizon(q string) int {
	days, err := strconv.Atoi(q)
	if err != nil || days <= 0 {
		return defaultHorizon
	}
	if days > maxHorizon {
		return maxHorizon
	}
	return days
}
