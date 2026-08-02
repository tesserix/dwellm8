package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Listing moderation (#143), Gin-native. The public reports; the organisation
// admin judges — suspend, reinstate, dismiss — every judgement audited on the
// outbox. Publication guardrails live in the domain and fire on publish.

// ModerationHandler serves the moderation routes.
type ModerationHandler struct {
	mod *store.Moderation
	log *slog.Logger
}

// NewModeration builds the handler.
func NewModeration(m *store.Moderation, log *slog.Logger) *ModerationHandler {
	return &ModerationHandler{mod: m, log: log}
}

// OwnerRoutes mounts the judging side — administrative throughout.
func (h *ModerationHandler) OwnerRoutes(r *ginx.Registrar) {
	admin := authz.Check{Relation: "can_administer", Object: authz.Organisation()}
	r.Handle(http.MethodGet, "/v1/moderation/listings", admin, h.Queue)
	r.Handle(http.MethodPost, "/v1/listings/:id/suspend", admin, h.Suspend)
	r.Handle(http.MethodPost, "/v1/listings/:id/warn", admin, h.Warn)
	r.Handle(http.MethodPost, "/v1/listings/:id/reinstate", admin, h.Reinstate)
	r.Handle(http.MethodPost, "/v1/listings/:id/reports/dismiss", admin, h.Dismiss)
}

// PublicRoutes mounts the report route: anyone may pull the cord.
func (h *ModerationHandler) PublicRoutes(r *ginx.Registrar) {
	r.Open(http.MethodPost, "/v1/public/listings/:id/report", openReason, h.Report)
}

// Report flags a listing from the public side. One answer whatever happened —
// a distinguishable response would be an oracle over moderation state.
func (h *ModerationHandler) Report(c *gin.Context) {
	var req struct {
		Reason string `json:"reason" binding:"required,oneof=fraud discrimination incorrect other"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{
			"error": "reason is one of fraud, discrimination, incorrect, other"})
		return
	}
	if err := h.mod.ReportListing(c.Request.Context(), c.Param("id"), req.Reason); err != nil {
		h.log.Error("recording a listing report", "error", err)
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "thank you — this listing will be reviewed"})
}

// Queue is the listing moderation queue: reported or suspended, oldest first.
func (h *ModerationHandler) Queue(c *gin.Context) {
	list, err := h.mod.Queue(c.Request.Context())
	if err != nil {
		h.fail(c, "reading the moderation queue", err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, f := range list {
		v := gin.H{"id": f.ID, "headline": f.Headline, "state": f.State,
			"report_count": f.ReportCount}
		if f.ReportedAt != nil {
			v["reported_at"] = f.ReportedAt
		}
		if f.SuspendedReason != "" {
			v["suspended_reason"] = f.SuspendedReason
		}
		out = append(out, v)
	}
	c.JSON(http.StatusOK, gin.H{"queue": out, "waiting": len(list)})
}

// Suspend takes a listing off the market by moderation, reason required.
func (h *ModerationHandler) Suspend(c *gin.Context) {
	var req struct {
		Reason string `json:"reason" binding:"required,min=3"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "a suspension carries its reason"})
		return
	}
	if err := h.mod.Suspend(c.Request.Context(), c.Param("id"), req.Reason,
		events.Actor{Kind: events.ActorSystem}); err != nil {
		h.fail(c, "suspending a listing", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "suspended"})
}

// Warn keeps the listing live with the lister on notice, reason required.
func (h *ModerationHandler) Warn(c *gin.Context) {
	var req struct {
		Reason string `json:"reason" binding:"required,min=3"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "a warning carries its reason"})
		return
	}
	if err := h.mod.Warn(c.Request.Context(), c.Param("id"), req.Reason,
		events.Actor{Kind: events.ActorSystem}); err != nil {
		h.fail(c, "warning the lister", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "warned": true})
}

// Reinstate lifts a suspension.
func (h *ModerationHandler) Reinstate(c *gin.Context) {
	if err := h.mod.Reinstate(c.Request.Context(), c.Param("id"),
		events.Actor{Kind: events.ActorSystem}); err != nil {
		h.fail(c, "reinstating a listing", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "live"})
}

// Dismiss clears a report judged wrong.
func (h *ModerationHandler) Dismiss(c *gin.Context) {
	if err := h.mod.DismissReports(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, "dismissing reports", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "reports": "dismissed"})
}

func (h *ModerationHandler) fail(c *gin.Context, what string, err error) {
	switch {
	case errors.Is(err, store.ErrNoListing):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, domain.ErrTransition):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, tenancy.ErrNoTenant):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no organisation in this request"})
	default:
		h.log.Error(what, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not " + what})
	}
}
