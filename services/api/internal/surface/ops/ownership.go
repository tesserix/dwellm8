package ops

import (
	"errors"
	"net/http"

	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// OwnershipRoutes mounts the record of who a property is owned by (#302).
// can_administer, like onboarding: this decides whose income the rent is.
func (h *Handler) OwnershipRoutes(r *authz.Registrar) {
	r.Handle("PUT /v1/ops/properties/{id}/ownership", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.RecordOwnership)
}

type ownershipRequest struct {
	// OwnerPartyID names the owner explicitly. Left out, it is the owner of the
	// organisation whose books the property is in — which is the answer for
	// every property registered before ownership was recorded at all.
	OwnerPartyID string `json:"owner_party_id"`
	From         string `json:"from"`
}

type ownershipResponse struct {
	PropertyID   string `json:"property_id"`
	OwnerPartyID string `json:"owner_party_id"`
	From         string `json:"from"`
}

// RecordOwnership names who the rent on a property is credited to. Idempotent,
// so it is both the repair for a property registered without an owner and the
// ordinary record of one.
func (h *Handler) RecordOwnership(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	propertyID := r.PathValue("id")
	var req ownershipRequest
	if err := decode(w, r, &req); err != nil {
		return
	}

	// The books the property is in, which is also the session the write must be
	// made under — a firm acts on the owner's tenancy, never its own.
	ownerOrgID, err := h.properties.TenantOf(r.Context(), propertyID)
	switch {
	case errors.Is(err, propertyservice.ErrNoProperty):
		writeError(w, http.StatusNotFound, "no such property")
		return
	case err != nil:
		h.log.Error("resolving the property's books", "property", propertyID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the property")
		return
	}

	party := req.OwnerPartyID
	if party == "" {
		party, err = h.owners.OwnerParty(r.Context(), ownerOrgID)
		switch {
		case errors.Is(err, identitystore.ErrNoOwnerParty):
			writeError(w, http.StatusUnprocessableEntity,
				"those books have no owner, so name the one the rent is credited to")
			return
		case err != nil:
			h.log.Error("resolving the owner", "owner_org", ownerOrgID, "error", err)
			writeError(w, http.StatusInternalServerError, "could not read the owner")
			return
		}
	}

	from, err := effective.ParseDate(req.From)
	if req.From == "" {
		from = effective.DateOf(h.now(), ist)
	} else if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "from must be a date, as YYYY-MM-DD")
		return
	}

	ownerCtx := tenancy.With(r.Context(), tenancy.ID(ownerOrgID))
	if err := h.properties.RecordOwnership(ownerCtx, propertyID, party, from); err != nil {
		h.log.Error("recording ownership", "property", propertyID, "party", party, "error", err)
		writeError(w, http.StatusUnprocessableEntity, "the ownership was refused: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ownershipResponse{
		PropertyID: propertyID, OwnerPartyID: party, From: from.String()})
}
