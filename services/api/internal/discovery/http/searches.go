package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
)

// Saved searches (#144), Gin-native, public side only: the searcher is a
// prospect with a browsing token, exactly like the shortlist.

// SearchesHandler serves the saved-search routes.
type SearchesHandler struct {
	searches  *store.Searches
	prospects *service.Prospects
	log       *slog.Logger
}

// NewSearches builds the handler.
func NewSearches(s *store.Searches, p *service.Prospects, log *slog.Logger) *SearchesHandler {
	return &SearchesHandler{searches: s, prospects: p, log: log}
}

// PublicRoutes mounts the routes.
func (h *SearchesHandler) PublicRoutes(r *ginx.Registrar) {
	r.Open(http.MethodPost, "/v1/public/searches", openReason, h.Save)
	r.Open(http.MethodGet, "/v1/public/searches", openReason, h.Mine)
	r.Open(http.MethodPost, "/v1/public/searches/:id/seen", openReason, h.Seen)
	r.Open(http.MethodPost, "/v1/public/searches/:id/alerts", openReason, h.Alerts)
	r.Open(http.MethodDelete, "/v1/public/searches/:id", openReason, h.Delete)
}

func (h *SearchesHandler) prospect(c *gin.Context) (string, bool) {
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return "", false
	}
	return p.ID, true
}

// Save records the criteria; the same criteria twice is the same search.
func (h *SearchesHandler) Save(c *gin.Context) {
	var req struct {
		City         string `json:"city" binding:"required"`
		Locality     string `json:"locality"`
		MaxRentMinor int64  `json:"max_rent_minor" binding:"omitempty,gt=0"`
		Bedrooms     int    `json:"bedrooms" binding:"omitempty,min=0,max=20"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	pid, ok := h.prospect(c)
	if !ok {
		return
	}
	id, err := h.searches.Save(c.Request.Context(), pid, req.City, req.Locality,
		req.MaxRentMinor, req.Bedrooms)
	if err != nil {
		h.fail(c, "saving the search", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id})
}

// Mine lists the prospect's searches with their fresh-match counts.
func (h *SearchesHandler) Mine(c *gin.Context) {
	pid, ok := h.prospect(c)
	if !ok {
		return
	}
	list, err := h.searches.Mine(c.Request.Context(), pid)
	if err != nil {
		h.fail(c, "reading saved searches", err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, r := range list {
		v := gin.H{"id": r.ID, "city": r.City, "alerts_enabled": r.AlertsEnabled,
			"new_matches": r.NewMatches, "created_at": r.CreatedAt.UTC().Format(time.RFC3339)}
		if r.Locality != "" {
			v["locality"] = r.Locality
		}
		if r.MaxRentMinor > 0 {
			v["max_rent_minor"] = r.MaxRentMinor
		}
		if r.Bedrooms >= 0 {
			v["bedrooms"] = r.Bedrooms
		}
		out = append(out, v)
	}
	c.JSON(http.StatusOK, gin.H{"searches": out})
}

// Seen advances the watermark — the searcher has looked.
func (h *SearchesHandler) Seen(c *gin.Context) {
	h.act(c, func(pid string) error {
		return h.searches.Seen(c.Request.Context(), pid, c.Param("id"))
	}, "marking the search seen")
}

// Alerts flips alerting; the search is retained either way.
func (h *SearchesHandler) Alerts(c *gin.Context) {
	var req struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "enabled is required, true or false"})
		return
	}
	h.act(c, func(pid string) error {
		return h.searches.SetAlerts(c.Request.Context(), pid, c.Param("id"), *req.Enabled)
	}, "setting alerts")
}

// Delete removes the prospect's own search.
func (h *SearchesHandler) Delete(c *gin.Context) {
	h.act(c, func(pid string) error {
		return h.searches.Delete(c.Request.Context(), pid, c.Param("id"))
	}, "deleting the search")
}

func (h *SearchesHandler) act(c *gin.Context, fn func(pid string) error, what string) {
	pid, ok := h.prospect(c)
	if !ok {
		return
	}
	if err := fn(pid); err != nil {
		h.fail(c, what, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id")})
}

func (h *SearchesHandler) fail(c *gin.Context, what string, err error) {
	if errors.Is(err, store.ErrNoSearch) {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	h.log.Error(what, "error", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "could not " + what})
}
