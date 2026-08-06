package ops

import (
	"net/http"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// grantHeader names the mandate an Ops request acts under. ADR-0005's model
// made a wire fact: the session's organisation stays the firm's, and the
// declared grant is what opens the grantor's rows — a bogus or revoked id
// opens nothing, because current_active_grant() checks it against the firm
// and its window on every row.
const grantHeader = "X-Dwellm8-Grant"

// ActingUnderGrant lifts the declared grant into the request context. Wraps
// the whole ops mux, after the resolver — the ctx already names the firm.
func ActingUnderGrant(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g := r.Header.Get(grantHeader); g != "" {
			r = r.WithContext(tenancy.WithGrant(r.Context(), tenancy.GrantID(g)))
		}
		next.ServeHTTP(w, r)
	})
}

// PortfolioRoutes mounts the switcher's read.
func (h *Handler) PortfolioRoutes(r *authz.Registrar) {
	r.Handle("GET /v1/ops/portfolios", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Portfolios)
}

type portfolioResponse struct {
	GrantID       string   `json:"grant_id"`
	OwnerOrgID    string   `json:"owner_org_id"`
	OwnerName     string   `json:"owner_name"`
	Permissions   []string `json:"permissions"`
	Since         string   `json:"since"`
	PropertyCount int      `json:"property_count"`
}

// Portfolios lists the owners this firm manages — every live mandate, named.
// The firm's own books need no entry: no grant declared is the firm itself.
func (h *Handler) Portfolios(w http.ResponseWriter, r *http.Request) {
	if h.owners == nil {
		writeError(w, http.StatusNotFound, "not here yet")
		return
	}
	firm, ok := tenancy.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in again")
		return
	}
	list, err := h.owners.Portfolios(r.Context(), firm.String())
	if err != nil {
		h.log.Error("listing portfolios", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the portfolios")
		return
	}
	out := make([]portfolioResponse, 0, len(list))
	for _, p := range list {
		out = append(out, portfolioResponse{
			GrantID: p.GrantID, OwnerOrgID: p.OwnerOrgID, OwnerName: p.OwnerName,
			Permissions: p.Permissions, Since: p.Since.Format(time.RFC3339),
			PropertyCount: p.PropertyCount,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"portfolios": out})
}
