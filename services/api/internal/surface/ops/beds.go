package ops

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// The bed board of a hostel or a PG (#299). A bed is what is let there, so it
// carries its own rent and points at the lease that holds it.

// BedRoutes mounts the board.
func (h *Handler) BedRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/properties/{id}/beds", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Beds)
	r.Handle("POST /v1/ops/units/{id}/beds", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.AddBed)
	r.Handle("POST /v1/ops/beds/{id}/allocation", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.AllocateBed)
}

type bedResponse struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Room      string `json:"room"`
	Floor     int    `json:"floor"`
	Sharing   int    `json:"sharing"`
	RentMinor int64  `json:"rent_amount_minor"`
	State     string `json:"state"`
	LeaseID   string `json:"lease_id,omitempty"`
}

func asBed(b propertyservice.Bed) bedResponse {
	return bedResponse{
		ID: b.ID, Label: b.Label, Room: b.Room, Floor: b.Floor, Sharing: b.Sharing,
		RentMinor: b.RentMinor, State: b.State, LeaseID: b.LeaseID,
	}
}

// Beds lists a property's beds, floor by floor and room by room.
func (h *Handler) Beds(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.propertyInScope(w, r); !ok {
		return
	}
	beds, err := h.properties.Beds(r.Context(), id)
	if err != nil {
		h.log.Error("reading a property's beds", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the beds")
		return
	}
	out := make([]bedResponse, 0, len(beds))
	for _, b := range beds {
		out = append(out, asBed(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"beds": out})
}

type addBedRequest struct {
	Label     string `json:"label"`
	RentMinor int64  `json:"rent_amount_minor"`
}

// AddBed puts a bed in a room.
func (h *Handler) AddBed(w http.ResponseWriter, r *http.Request) {
	unitID := r.PathValue("id")
	var req addBedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a bed")
		return
	}
	req.Label = strings.TrimSpace(req.Label)
	if req.Label == "" {
		writeError(w, http.StatusUnprocessableEntity, "a bed needs a label to be allocated by")
		return
	}
	if req.RentMinor < 0 {
		writeError(w, http.StatusUnprocessableEntity, "a bed cannot be let at less than nothing")
		return
	}
	if _, err := h.properties.Unit(r.Context(), unitID); err != nil {
		if errors.Is(err, propertyservice.ErrNoUnit) {
			writeError(w, http.StatusNotFound, "no such room")
			return
		}
		h.log.Error("reading the room a bed goes in", "unit", unitID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the room")
		return
	}
	bed, err := h.properties.AddBed(r.Context(), unitID, req.Label, req.RentMinor)
	if err != nil {
		if errors.Is(err, propertyservice.ErrBedExists) {
			writeError(w, http.StatusConflict, "that bed is already in this room")
			return
		}
		h.log.Error("adding a bed", "unit", unitID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not add the bed")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"bed": asBed(bed)})
}

type allocateBedRequest struct {
	State   string `json:"state"`
	LeaseID string `json:"lease_id"`
}

var bedStates = map[string]bool{"vacant": true, "reserved": true, "occupied": true, "notice": true}

// AllocateBed moves a bed between states, and holds it against the lease that
// carries the rent where the state calls for one.
func (h *Handler) AllocateBed(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req allocateBedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not an allocation")
		return
	}
	req.State, req.LeaseID = strings.TrimSpace(req.State), strings.TrimSpace(req.LeaseID)
	if !bedStates[req.State] {
		writeError(w, http.StatusUnprocessableEntity, "a bed is vacant, reserved, occupied or on notice")
		return
	}
	// The database says the same thing, but a 500 out of a constraint tells the
	// manager nothing about the tenancy they have not created yet.
	if (req.State == "occupied" || req.State == "notice") && req.LeaseID == "" {
		writeError(w, http.StatusUnprocessableEntity,
			"an occupied bed names the tenancy it is let under, or nobody is billed for it")
		return
	}
	if req.State == "vacant" || req.State == "reserved" {
		req.LeaseID = ""
	}
	if err := h.properties.AllocateBed(r.Context(), id, req.State, req.LeaseID); err != nil {
		if errors.Is(err, propertyservice.ErrNoBed) {
			writeError(w, http.StatusNotFound, "no such bed")
			return
		}
		h.log.Error("allocating a bed", "bed", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not allocate the bed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"bed": map[string]string{"id": id, "state": req.State}})
}
