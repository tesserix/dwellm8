package http

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
)

// Push token registration (#126), public side: the token belongs to the
// prospect's browsing session, exactly like the shortlist and the searches
// the pushes are about.

// PushHandler serves the token routes.
type PushHandler struct {
	tokens    *store.PushTokens
	prospects *service.Prospects
	log       *slog.Logger
}

// NewPush builds the handler.
func NewPush(t *store.PushTokens, p *service.Prospects, log *slog.Logger) *PushHandler {
	return &PushHandler{tokens: t, prospects: p, log: log}
}

// PublicRoutes mounts the routes.
func (h *PushHandler) PublicRoutes(r *ginx.Registrar) {
	r.Open(http.MethodPost, "/v1/public/push/token", openReason, h.Register)
	r.Open(http.MethodPost, "/v1/public/push/token/drop", openReason, h.Drop)
}

type pushTokenRequest struct {
	Token    string `json:"token" binding:"required,min=10,max=200"`
	Platform string `json:"platform" binding:"omitempty,oneof=ios android"`
}

// Register upserts the device's token against the prospect.
func (h *PushHandler) Register(c *gin.Context) {
	var req pushTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	// Expo tokens have one shape; anything else is not a device of ours.
	if !strings.HasPrefix(req.Token, "ExponentPushToken[") {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "token is not an Expo push token"})
		return
	}
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	platform := req.Platform
	if platform == "" {
		platform = "android"
	}
	if err := h.tokens.Register(c.Request.Context(), p.ID, req.Token, platform); err != nil {
		h.log.Error("registering a push token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not register the token"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"status": "registered"})
}

// Drop removes the registration — sign-out, or permission revoked.
func (h *PushHandler) Drop(c *gin.Context) {
	var req pushTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	if err := h.tokens.Drop(c.Request.Context(), p.ID, req.Token); err != nil {
		h.log.Error("dropping a push token", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not drop the token"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "dropped"})
}
