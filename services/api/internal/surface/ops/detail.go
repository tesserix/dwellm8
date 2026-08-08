package ops

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// What a property is like, and what is near it (#354). The vocabularies are
// CHECK constraints in the schema, so an unknown value comes back as the
// manager's typo — 422 — rather than as a server fault.

// DetailRoutes mounts the description of a building, of a flat, and the list
// of what is near it.
func (h *Handler) DetailRoutes(r *authz.Registrar) {
	r.Handle("PUT /v1/ops/properties/{id}/detail", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.DescribeProperty)
	r.Handle("PUT /v1/ops/units/{id}/detail", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.DescribeUnit)
	r.Handle("GET /v1/ops/properties/{id}/places", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Places)
	r.Handle("POST /v1/ops/properties/{id}/places", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.AddPlace)
	r.Handle("PATCH /v1/ops/places/{id}", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.CorrectPlace)
	r.Handle("DELETE /v1/ops/places/{id}", authz.Check{
		Relation: "can_manage", Object: authz.Organisation()}, h.RetirePlace)
}

type propertyDetailRequest struct {
	About     string   `json:"about"`
	Amenities []string `json:"amenities"`
}

// DescribeProperty writes what the building is like.
func (h *Handler) DescribeProperty(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req propertyDetailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a description")
		return
	}
	err := h.properties.Describe(r.Context(), id, propertyservice.PropertyDetail{
		About: strings.TrimSpace(req.About), Amenities: trimmed(req.Amenities),
	})
	switch {
	case errors.Is(err, propertyservice.ErrNoProperty):
		writeError(w, http.StatusNotFound, "no such property")
	case errors.Is(err, propertyservice.ErrNotAllowed):
		writeError(w, http.StatusUnprocessableEntity, "one of those amenities is not one we list")
	case err != nil:
		h.log.Error("describing a property", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the description")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"property": map[string]string{"id": id}})
	}
}

type unitDetailRequest struct {
	About          string   `json:"about"`
	Features       []string `json:"features"`
	Bathrooms      *int     `json:"bathrooms"`
	Balconies      *int     `json:"balconies"`
	CoveredParking *int     `json:"covered_parking"`
	Facing         string   `json:"facing"`
	Furnishing     string   `json:"furnishing"`
}

// DescribeUnit writes what one flat is like.
func (h *Handler) DescribeUnit(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req unitDetailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a description")
		return
	}
	err := h.properties.DescribeUnit(r.Context(), id, propertyservice.UnitDetail{
		About: strings.TrimSpace(req.About), Features: trimmed(req.Features),
		Bathrooms: req.Bathrooms, Balconies: req.Balconies, CoveredParking: req.CoveredParking,
		Facing: strings.TrimSpace(req.Facing), Furnishing: strings.TrimSpace(req.Furnishing),
	})
	switch {
	case errors.Is(err, propertyservice.ErrNoUnit):
		writeError(w, http.StatusNotFound, "no such unit")
	case errors.Is(err, propertyservice.ErrNotAllowed):
		writeError(w, http.StatusUnprocessableEntity,
			"a flat is unfurnished, semi furnished or fully furnished, and faces one of the eight directions")
	case err != nil:
		h.log.Error("describing a unit", "unit", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the description")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"unit": map[string]string{"id": id}})
	}
}

type placeResponse struct {
	ID         string   `json:"id"`
	Category   string   `json:"category"`
	Name       string   `json:"name"`
	DistanceM  int      `json:"distance_m"`
	TravelMode string   `json:"travel_mode"`
	Tags       []string `json:"tags"`
	Note       string   `json:"note,omitempty"`
	Position   int      `json:"position"`
}

func asPlace(p propertyservice.Place) placeResponse {
	tags := p.Tags
	if tags == nil {
		tags = []string{}
	}
	return placeResponse{
		ID: p.ID, Category: p.Category, Name: p.Name, DistanceM: p.DistanceM,
		TravelMode: p.TravelMode, Tags: tags, Note: p.Note, Position: p.Position,
	}
}

// Places lists what is near a property, nearest first.
func (h *Handler) Places(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.propertyInScope(w, r); !ok {
		return
	}
	places, err := h.properties.Places(r.Context(), id)
	if err != nil {
		h.log.Error("reading what is near a property", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read what is nearby")
		return
	}
	out := make([]placeResponse, 0, len(places))
	for _, p := range places {
		out = append(out, asPlace(p))
	}
	writeJSON(w, http.StatusOK, map[string]any{"places": out})
}

type placeRequest struct {
	Category   string   `json:"category"`
	Name       string   `json:"name"`
	DistanceM  int      `json:"distance_m"`
	TravelMode string   `json:"travel_mode"`
	Tags       []string `json:"tags"`
	Note       string   `json:"note"`
	Position   int      `json:"position"`
}

func (req *placeRequest) place() propertyservice.Place {
	return propertyservice.Place{
		Category: strings.TrimSpace(req.Category), Name: strings.TrimSpace(req.Name),
		DistanceM: req.DistanceM, TravelMode: strings.TrimSpace(req.TravelMode),
		Tags: trimmed(req.Tags), Note: strings.TrimSpace(req.Note), Position: req.Position,
	}
}

// read decodes a place and says what is wrong with it in the manager's terms.
func (h *Handler) readPlace(w http.ResponseWriter, r *http.Request) (propertyservice.Place, bool) {
	var req placeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a place")
		return propertyservice.Place{}, false
	}
	p := req.place()
	if p.Name == "" {
		writeError(w, http.StatusUnprocessableEntity, "a place needs a name to be recognised by")
		return propertyservice.Place{}, false
	}
	if p.Category == "" {
		writeError(w, http.StatusUnprocessableEntity, "say what kind of place this is")
		return propertyservice.Place{}, false
	}
	if p.DistanceM <= 0 {
		writeError(w, http.StatusUnprocessableEntity, "say how far it is, in metres")
		return propertyservice.Place{}, false
	}
	return p, true
}

// AddPlace records one place near a property.
func (h *Handler) AddPlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := h.readPlace(w, r)
	if !ok {
		return
	}
	added, err := h.properties.AddPlace(r.Context(), id, p)
	switch {
	case errors.Is(err, propertyservice.ErrNoProperty):
		writeError(w, http.StatusNotFound, "no such property")
	case errors.Is(err, propertyservice.ErrPlaceExists):
		writeError(w, http.StatusConflict, "that place is already on this list")
	case errors.Is(err, propertyservice.ErrNotAllowed):
		writeError(w, http.StatusUnprocessableEntity,
			"that is not a kind of place we list, or it is too far away to count as nearby")
	case err != nil:
		h.log.Error("adding a place near a property", "property", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not add the place")
	default:
		writeJSON(w, http.StatusCreated, map[string]any{"place": asPlace(added)})
	}
}

// CorrectPlace amends a place recorded wrongly — usually the distance.
func (h *Handler) CorrectPlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req placeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "that was not a place")
		return
	}
	p := req.place()
	p.ID = id
	if p.Category == "" && p.Name == "" && p.DistanceM == 0 &&
		p.TravelMode == "" && p.Tags == nil && p.Note == "" && p.Position == 0 {
		writeError(w, http.StatusUnprocessableEntity, "say what about this place changed")
		return
	}
	if p.DistanceM < 0 {
		writeError(w, http.StatusUnprocessableEntity, "say how far it is, in metres")
		return
	}
	corrected, err := h.properties.CorrectPlace(r.Context(), p)
	switch {
	case errors.Is(err, propertyservice.ErrNoPlace):
		writeError(w, http.StatusNotFound, "no such place")
	case errors.Is(err, propertyservice.ErrPlaceExists):
		writeError(w, http.StatusConflict, "that place is already on this list")
	case errors.Is(err, propertyservice.ErrNotAllowed):
		writeError(w, http.StatusUnprocessableEntity,
			"that is not a kind of place we list, or it is too far away to count as nearby")
	case err != nil:
		h.log.Error("correcting a place", "place", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not correct the place")
	default:
		writeJSON(w, http.StatusOK, map[string]any{"place": asPlace(corrected)})
	}
}

// RetirePlace takes a place off the list. The school that closed was still
// advertised once, so the row stays and stops being shown.
func (h *Handler) RetirePlace(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := h.properties.RetirePlace(r.Context(), id)
	switch {
	case errors.Is(err, propertyservice.ErrNoPlace):
		writeError(w, http.StatusNotFound, "no such place")
	case err != nil:
		h.log.Error("retiring a place", "place", id, "error", err)
		writeError(w, http.StatusInternalServerError, "could not remove the place")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// A nil list is one the caller did not mention, which a correction reads as
// "leave the tags alone" — so it stays nil rather than becoming an empty list.
func trimmed(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
