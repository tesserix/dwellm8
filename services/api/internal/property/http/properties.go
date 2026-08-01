// Package http is the property module's HTTP surface: registering the
// building and its units — issue #32, the start of the funnel every other
// module's rows point back to.
package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
	"github.com/tesserix/dwellm8/services/api/internal/property/service"
	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// maxBody bounds a registration. An address is a few hundred bytes.
const maxBody = 64 << 10

// Properties handles the write side of the module.
type Properties struct {
	properties *service.Properties
	log        *slog.Logger
}

// New builds the handler.
func New(p *service.Properties, log *slog.Logger) *Properties {
	return &Properties{properties: p, log: log}
}

// Routes mounts the module. Registering inventory is an administrative act on
// the organisation — an owner or co-owner, not field staff (ADR-0020).
func (h *Properties) Routes(r *authz.Registrar) {
	r.Handle("POST /v1/properties", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Create)
	r.Handle("POST /v1/properties/{id}/units", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.CreateUnit)
}

type createRequest struct {
	Code         string `json:"code"`
	Name         string `json:"name"`
	Kind         string `json:"kind"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2"`
	Locality     string `json:"locality"`
	City         string `json:"city"`
	District     string `json:"district"`
	StateCode    string `json:"state_code"`
	Pin          string `json:"pin"`
}

// Create registers a property.
func (h *Properties) Create(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	id, err := h.properties.Register(r.Context(), domain.PropertyDraft{
		Code: req.Code, Name: req.Name, Kind: req.Kind,
		AddressLine1: req.AddressLine1, AddressLine2: req.AddressLine2,
		Locality: req.Locality, City: req.City, District: req.District,
		StateCode: req.StateCode, Pin: req.Pin,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "code": req.Code})
	case errors.Is(err, domain.ErrDraft):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, store.ErrCodeTaken):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("registering a property", "error", err)
		writeError(w, http.StatusInternalServerError, "could not register the property")
	}
}

type unitRequest struct {
	Code           string  `json:"code"`
	Kind           string  `json:"unit_kind"`
	Floor          *int    `json:"floor"`
	CarpetAreaSqft float64 `json:"carpet_area_sqft"`
	ParentUnitID   string  `json:"parent_unit_id"`
}

// CreateUnit adds a unit to a property.
func (h *Properties) CreateUnit(w http.ResponseWriter, r *http.Request) {
	var req unitRequest
	if err := decode(w, r, &req); err != nil {
		return
	}
	id, err := h.properties.AddUnit(r.Context(), r.PathValue("id"), domain.UnitDraft{
		Code: req.Code, Kind: req.Kind, Floor: req.Floor,
		CarpetAreaSqft: req.CarpetAreaSqft, ParentUnitID: req.ParentUnitID,
	})
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{"id": id, "code": req.Code})
	case errors.Is(err, domain.ErrDraft):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, store.ErrUnitCodeTaken):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNoProperty):
		writeError(w, http.StatusNotFound, "no such property")
	case errors.Is(err, tenancy.ErrNoTenant):
		writeError(w, http.StatusUnauthorized, "no organisation in this request")
	default:
		h.log.Error("adding a unit", "error", err)
		writeError(w, http.StatusInternalServerError, "could not add the unit")
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) error {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
	if err != nil {
		writeError(w, http.StatusBadRequest, "could not read the request")
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
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
