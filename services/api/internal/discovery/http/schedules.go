package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ScheduleRoutes mounts the recurring viewing times (#330). Only the manager's
// side: a prospect sees slots, never the rule that produced them.
func (h *Inspections) ScheduleRoutes(r *authz.Registrar) {
	r.Handle("POST /v1/listings/{id}/schedules", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("listing", "id")}, h.CreateSchedule)
	r.Handle("GET /v1/listings/{id}/schedules", authz.Check{
		Relation: "can_view", Object: authz.PathObject("listing", "id")}, h.Schedules)
	r.Handle("PATCH /v1/schedules/{id}", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.AmendSchedule)
	r.Handle("DELETE /v1/schedules/{id}", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.EndSeries)
	r.Handle("POST /v1/slots/{id}/cancel", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.CancelOccurrence)
	r.Handle("POST /v1/slots/{id}/move", authz.Check{
		Relation: "can_operate", Object: authz.Organisation()}, h.MoveOccurrence)
}

type scheduleRequest struct {
	Weekdays     []int  `json:"weekdays"`
	StartTime    string `json:"start_time"`
	Zone         string `json:"zone"`
	DurationMins int    `json:"duration_mins"`
	Capacity     int    `json:"capacity"`
	AssignedTo   string `json:"assigned_to"`
	MeetingPoint string `json:"meeting_point"`
	StartsOn     string `json:"starts_on"`
	EndsOn       string `json:"ends_on"`
	// From is the date an amendment takes effect: the calendar's "this one and
	// all after it". Ignored on creation.
	From string `json:"from"`
}

func (req scheduleRequest) draft() (store.ScheduleDraft, error) {
	d := store.ScheduleDraft{
		Weekdays: req.Weekdays, StartTime: req.StartTime, Zone: req.Zone,
		DurationMins: req.DurationMins, Capacity: req.Capacity,
		AssignedTo: req.AssignedTo, MeetingPoint: req.MeetingPoint,
	}
	if d.Zone == "" {
		d.Zone = "Asia/Kolkata"
	}
	starts, err := parseDay(req.StartsOn)
	if err != nil {
		return d, err
	}
	d.StartsOn = starts
	if req.EndsOn != "" {
		ends, err := parseDay(req.EndsOn)
		if err != nil {
			return d, err
		}
		d.EndsOn = &ends
	}
	return d, nil
}

func parseDay(v string) (time.Time, error) {
	if v == "" {
		now := time.Now()
		return domain.Day(now.Year(), now.Month(), now.Day()), nil
	}
	return time.Parse("2006-01-02", v)
}

func toSchedule(sc store.Schedule) map[string]any {
	out := map[string]any{
		"id": sc.ID, "listing_id": sc.ListingID, "weekdays": sc.Weekdays,
		"start_time": sc.StartTime, "zone": sc.Zone, "duration_mins": sc.DurationMins,
		"capacity": sc.Capacity, "starts_on": sc.StartsOn.Format("2006-01-02"),
		"state": sc.State, "assigned_to": sc.AssignedTo, "meeting_point": sc.MeetingPoint,
	}
	if sc.EndsOn != nil {
		out["ends_on"] = sc.EndsOn.Format("2006-01-02")
	}
	return out
}

// CreateSchedule publishes a recurring viewing time.
func (h *Inspections) CreateSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	d, err := req.draft()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "starts_on and ends_on must be dates, as YYYY-MM-DD")
		return
	}
	id, err := h.inspections.CreateSchedule(r.Context(), r.PathValue("id"), d)
	if err == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}
	h.respondSchedule(w, err, "publishing a viewing series")
}

// Schedules is the listing's recurring times.
func (h *Inspections) Schedules(w http.ResponseWriter, r *http.Request) {
	list, err := h.inspections.Schedules(r.Context(), r.PathValue("id"))
	if err != nil {
		h.respondListError(w, err, "reading the viewing series")
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, sc := range list {
		out = append(out, toSchedule(sc))
	}
	writeJSON(w, http.StatusOK, map[string]any{"schedules": out})
}

// AmendSchedule changes a series from a date forward.
func (h *Inspections) AmendSchedule(w http.ResponseWriter, r *http.Request) {
	var req scheduleRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	from, err := parseDay(req.From)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "from must be a date, as YYYY-MM-DD")
		return
	}
	d, err := req.draft()
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "starts_on and ends_on must be dates, as YYYY-MM-DD")
		return
	}
	id, err := h.inspections.AmendSchedule(r.Context(), r.PathValue("id"), d, from)
	if err == nil {
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "from": from.Format("2006-01-02")})
		return
	}
	h.respondSchedule(w, err, "amending a viewing series")
}

// EndSeries stops a series from a date forward, or from today.
func (h *Inspections) EndSeries(w http.ResponseWriter, r *http.Request) {
	from, err := parseDay(r.URL.Query().Get("from"))
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "from must be a date, as YYYY-MM-DD")
		return
	}
	if err := h.inspections.EndSeries(r.Context(), r.PathValue("id"), from); err != nil {
		h.respondSchedule(w, err, "ending a viewing series")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "state": "ended"})
}

// CancelOccurrence calls off one viewing without touching the series.
func (h *Inspections) CancelOccurrence(w http.ResponseWriter, r *http.Request) {
	if err := h.inspections.CancelOccurrence(r.Context(), r.PathValue("id")); err != nil {
		h.respondSchedule(w, err, "cancelling one viewing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": r.PathValue("id"), "state": "cancelled"})
}

// MoveOccurrence shifts one viewing to another time.
func (h *Inspections) MoveOccurrence(w http.ResponseWriter, r *http.Request) {
	var req struct {
		StartsAt string `json:"starts_at"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	to, err := time.Parse(time.RFC3339, req.StartsAt)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "starts_at must be RFC 3339")
		return
	}
	if err := h.inspections.MoveOccurrence(r.Context(), r.PathValue("id"), to); err != nil {
		h.respondSchedule(w, err, "moving one viewing")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"id": r.PathValue("id"), "starts_at": to.UTC().Format(time.RFC3339)})
}

func (h *Inspections) respondSchedule(w http.ResponseWriter, err error, what string) {
	switch {
	case errors.Is(err, domain.ErrScheduleUnsound):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, store.ErrNoListing):
		writeError(w, http.StatusNotFound, "no such listing")
	case errors.Is(err, store.ErrNoSchedule):
		writeError(w, http.StatusNotFound, "no such viewing series")
	case errors.Is(err, store.ErrNoSlot):
		writeError(w, http.StatusNotFound, "no such viewing")
	case errors.Is(err, store.ErrListingNotLive):
		writeError(w, http.StatusConflict, "this listing is finished — viewing times cannot be added to it")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error(what, "error", err)
		writeError(w, http.StatusInternalServerError, "could not change the viewing times")
	}
}
