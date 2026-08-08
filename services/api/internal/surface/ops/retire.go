package ops

import (
	"errors"
	"net/http"
	"strings"

	identitystore "github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	propertyservice "github.com/tesserix/dwellm8/services/api/internal/property/service"
)

// Taking things back off the book (#356). Nothing here deletes: a retired
// building keeps its leases, its ledger and its documents, and leaves the
// lists. The schema refuses to retire anything somebody is living in, and that
// refusal comes back as 422 with the reason so the manager can act on it.

// RetireRoutes mounts the way out of a record.
func (h *Handler) RetireRoutes(r *authz.Registrar) {
	manage := authz.Check{Relation: "can_manage", Object: authz.Organisation()}
	r.Handle("DELETE /v1/ops/properties/{id}", manage, h.RetireProperty)
	r.Handle("DELETE /v1/ops/units/{id}", manage, h.RetireUnit)
	r.Handle("DELETE /v1/ops/beds/{id}", manage, h.RetireBed)
	r.Handle("DELETE /v1/ops/portfolios/{grant}", manage, h.ResignMandate)
	r.Handle("POST /v1/ops/firm/closure", manage, h.CloseFirm)
}

// RetireProperty takes a building off the book.
func (h *Handler) RetireProperty(w http.ResponseWriter, r *http.Request) {
	h.retired(w, r, "property",
		h.properties.Retire(r.Context(), r.PathValue("id"), reasonOf(r)))
}

// RetireUnit takes one home out of the building's list.
func (h *Handler) RetireUnit(w http.ResponseWriter, r *http.Request) {
	h.retired(w, r, "unit",
		h.properties.RetireUnit(r.Context(), r.PathValue("id"), reasonOf(r)))
}

// RetireBed takes a bed off the board.
func (h *Handler) RetireBed(w http.ResponseWriter, r *http.Request) {
	h.retired(w, r, "bed",
		h.properties.RetireBed(r.Context(), r.PathValue("id"), reasonOf(r)))
}

// ResignMandate stops the firm managing for an owner. The owner keeps their
// organisation, their properties and their history; the firm loses its keys.
func (h *Handler) ResignMandate(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	err := h.owners.Resign(r.Context(), firm.String(), r.PathValue("grant"))
	switch {
	case errors.Is(err, identitystore.ErrNoMandate):
		writeError(w, http.StatusNotFound, "you do not act for that owner")
	case err != nil:
		h.log.Error("resigning a mandate", "grant", r.PathValue("grant"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not end that mandate")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// CloseFirm closes the manager's own account. It is refused while a tenancy is
// live or an owner's mandate still stands, because closing then would leave a
// tenant paying rent to nobody.
func (h *Handler) CloseFirm(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	err := h.owners.Close(r.Context(), firm.String(), reasonOf(r))
	switch {
	case errors.Is(err, identitystore.ErrNotAllowed):
		writeError(w, http.StatusUnprocessableEntity, because(err))
	case errors.Is(err, identitystore.ErrNoOrganisation):
		writeError(w, http.StatusNotFound, "no such firm")
	case err != nil:
		h.log.Error("closing a firm", "firm", firm.String(), "error", err)
		writeError(w, http.StatusInternalServerError, "could not close the account")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func (h *Handler) retired(w http.ResponseWriter, r *http.Request, what string, err error) {
	switch {
	case errors.Is(err, propertyservice.ErrNotAllowed):
		writeError(w, http.StatusUnprocessableEntity, because(err))
	case errors.Is(err, propertyservice.ErrNoProperty),
		errors.Is(err, propertyservice.ErrNoUnit),
		errors.Is(err, propertyservice.ErrNoBed):
		writeError(w, http.StatusNotFound, "no such "+what)
	case err != nil:
		h.log.Error("retiring", "what", what, "id", r.PathValue("id"), "error", err)
		writeError(w, http.StatusInternalServerError, "could not take that off the book")
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// because is the guard's own sentence, which is what the manager has to read.
func because(err error) string {
	_, reason, found := strings.Cut(err.Error(), ": ")
	if !found {
		return err.Error()
	}
	return reason
}

// reasonOf is why, from the query string: a DELETE carries no body worth
// parsing and the reason is one short line.
func reasonOf(r *http.Request) string {
	return strings.TrimSpace(r.URL.Query().Get("reason"))
}
