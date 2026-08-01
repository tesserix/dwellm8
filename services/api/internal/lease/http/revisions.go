package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/lease/service"
	"github.com/tesserix/dwellm8/services/api/internal/lease/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The rent-revision route — issue #36, and the first surface on the
// platform's Gin layer (platform/ginx). Same governance as every route since
// ADR-0020: the check is declared at registration or the route does not exist.

// Revisions handles rent changes.
type Revisions struct {
	leases *service.Leases
	log    *slog.Logger
}

// NewRevisions builds the handler.
func NewRevisions(s *service.Leases, log *slog.Logger) *Revisions {
	return &Revisions{leases: s, log: log}
}

// Routes mounts the route on the Gin registrar.
func (h *Revisions) Routes(r *ginx.Registrar) {
	r.Handle(http.MethodPost, "/v1/leases/:id/rent-revisions", authz.Check{
		Relation: "can_edit", Object: authz.PathObject("agreement", "id")}, h.Create)
}

type revisionRequest struct {
	AmountMinor int64  `json:"rent_amount_minor" binding:"required,gt=0"`
	DueDay      int    `json:"due_day" binding:"omitempty,min=1,max=31"`
	EffectiveOn string `json:"effective_on" binding:"required"`
}

// Create revises the rent from a future effective date. ADR-0008: two rows,
// and an as-of query for any earlier day still answers with the old figure.
func (h *Revisions) Create(c *gin.Context) {
	var req revisionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "could not read the request: " + err.Error()})
		return
	}
	from, err := effective.ParseDate(req.EffectiveOn)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "effective_on must be a date, as YYYY-MM-DD"})
		return
	}
	if req.DueDay == 0 {
		req.DueDay = 5
	}

	err = h.leases.ReviseRent(c.Request.Context(), c.Param("id"), store.Revision{
		AmountMinor: req.AmountMinor, DueDay: req.DueDay, From: from,
	})
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, gin.H{
			"lease_id": c.Param("id"), "rent_amount_minor": req.AmountMinor,
			"effective_on": from.String(), "event": "lease.rent.revised",
		})
	case errors.Is(err, store.ErrRevisionExists):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrBackdated):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrNoLease):
		c.JSON(http.StatusNotFound, gin.H{"error": "no such tenancy"})
	case errors.Is(err, tenancy.ErrNoTenant):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no organisation in this request"})
	default:
		h.log.Error("revising rent", "lease", c.Param("id"), "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not revise the rent"})
	}
}
