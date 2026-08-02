package http

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/blob"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Listing media (#136), Gin-native. Uploads go straight to GCS on signed PUT
// URLs (platform/blob, the find bucket, org-prefixed paths); this surface
// holds membership, order and moderation. Public delivery happens on the
// listing detail: short-lived signed GETs, live media of live listings only.

// MediaHandler serves the media routes.
type MediaHandler struct {
	media *store.MediaStore
	blob  *blob.Store
	log   *slog.Logger
}

// NewMedia builds the handler. blob may be nil: recording, ordering and
// moderation still work, URL minting answers 503 — the DocURLKey shape.
func NewMedia(m *store.MediaStore, b *blob.Store, log *slog.Logger) *MediaHandler {
	return &MediaHandler{media: m, blob: b, log: log}
}

// OwnerRoutes mounts the authenticated side.
func (h *MediaHandler) OwnerRoutes(r *ginx.Registrar) {
	edit := authz.Check{Relation: "can_edit", Object: authz.PathObject("listing", "id")}
	r.Handle(http.MethodPost, "/v1/listings/:id/media/upload-url", edit, h.UploadURL)
	r.Handle(http.MethodPost, "/v1/listings/:id/media", edit, h.Record)
	r.Handle(http.MethodPut, "/v1/listings/:id/media/order", edit, h.Reorder)
	r.Handle(http.MethodGet, "/v1/listings/:id/media", authz.Check{
		Relation: "can_view", Object: authz.PathObject("listing", "id")}, h.List)
	// Takedown is the moderation act — administrative, not day-to-day editing.
	r.Handle(http.MethodPost, "/v1/listings/:id/media/:mid/takedown", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Takedown)
	// The moderation queue (#143): reported photos, oldest first, with the
	// two judgements — takedown above, or clear below.
	r.Handle(http.MethodGet, "/v1/moderation/media", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Queue)
	r.Handle(http.MethodPost, "/v1/listings/:id/media/:mid/clear", authz.Check{
		Relation: "can_administer", Object: authz.Organisation()}, h.Clear)
}

// PublicRoutes mounts the report route: anyone may pull the cord.
func (h *MediaHandler) PublicRoutes(r *ginx.Registrar) {
	r.Open(http.MethodPost, "/v1/public/listings/:id/media/:mid/report", openReason, h.Report)
}

// UploadURL mints a signed PUT for one photo. The object path is chosen
// server-side — a client-supplied path is a client-chosen overwrite target.
func (h *MediaHandler) UploadURL(c *gin.Context) {
	if h.blob == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "media storage is not configured"})
		return
	}
	var req struct {
		Filename    string `json:"filename" binding:"required"`
		ContentType string `json:"content_type" binding:"required,oneof=image/jpeg image/png image/webp"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	org, ok := tenancy.From(c.Request.Context())
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no organisation in this request"})
		return
	}
	url, path, err := h.blob.UploadURL(c.Request.Context(), auth.SurfaceFind, org.String(),
		req.Filename, req.ContentType)
	if err != nil {
		h.log.Error("minting a media upload URL", "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not prepare the upload"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"upload_url": url, "object_path": path,
		"content_type": req.ContentType})
}

// Record confirms an upload landed and places it in the order.
func (h *MediaHandler) Record(c *gin.Context) {
	var req struct {
		ObjectPath  string `json:"object_path" binding:"required"`
		ContentType string `json:"content_type" binding:"required,oneof=image/jpeg image/png image/webp"`
		Position    int    `json:"position" binding:"omitempty,min=0,max=23"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	id, err := h.media.Record(c.Request.Context(), c.Param("id"), req.ObjectPath,
		req.ContentType, req.Position)
	if err != nil {
		h.fail(c, "recording a photo", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "position": req.Position})
}

// Reorder rewrites the order; position zero is the cover.
func (h *MediaHandler) Reorder(c *gin.Context) {
	var req struct {
		IDs []string `json:"ids" binding:"required,min=1,max=24"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if err := h.media.Reorder(c.Request.Context(), c.Param("id"), req.IDs); err != nil {
		h.fail(c, "reordering photos", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"listing_id": c.Param("id"), "cover": req.IDs[0]})
}

// List is the owner's view, with fresh signed GETs when storage is wired.
func (h *MediaHandler) List(c *gin.Context) {
	list, err := h.media.ForListing(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "listing photos", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"media": h.views(c, list)})
}

// Takedown removes a photo from public delivery, audited.
func (h *MediaHandler) Takedown(c *gin.Context) {
	var req struct {
		Reason string `json:"reason" binding:"required,min=3"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "a takedown carries its reason"})
		return
	}
	if err := h.media.Takedown(c.Request.Context(), c.Param("id"), c.Param("mid"), req.Reason); err != nil {
		h.fail(c, "taking a photo down", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("mid"), "state": "removed"})
}

// Queue is the moderation queue: reported photos across the organisation's
// listings, oldest report first, each with a fresh signed URL to look at.
func (h *MediaHandler) Queue(c *gin.Context) {
	list, err := h.media.ReviewQueue(c.Request.Context())
	if err != nil {
		h.fail(c, "reading the moderation queue", err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, it := range list {
		v := gin.H{"id": it.ID, "listing_id": it.ListingID, "headline": it.Headline,
			"reported_at": it.ReportedAt, "content_type": it.ContentType}
		if h.blob != nil {
			if url, err := h.blob.DownloadURL(c.Request.Context(), auth.SurfaceFind, it.ObjectPath); err == nil {
				v["url"] = url
			}
		}
		out = append(out, v)
	}
	c.JSON(http.StatusOK, gin.H{"queue": out, "waiting": len(list)})
}

// Clear returns a reported photo to live — the report judged wrong.
func (h *MediaHandler) Clear(c *gin.Context) {
	if err := h.media.Clear(c.Request.Context(), c.Param("id"), c.Param("mid")); err != nil {
		h.fail(c, "clearing a report", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("mid"), "state": "live"})
}

// Report flags a photo from the public side. One answer whatever happened —
// a distinguishable response would be an oracle over moderation state.
func (h *MediaHandler) Report(c *gin.Context) {
	if err := h.media.Report(c.Request.Context(), c.Param("id"), c.Param("mid")); err != nil {
		h.log.Error("recording a media report", "error", err)
	}
	c.JSON(http.StatusAccepted, gin.H{"status": "thank you — this photograph will be reviewed"})
}

// views attaches signed GET URLs when the blob store is present.
func (h *MediaHandler) views(c *gin.Context, list []store.Media) []gin.H {
	out := make([]gin.H, 0, len(list))
	for _, m := range list {
		v := gin.H{"id": m.ID, "position": m.Position, "state": m.State,
			"content_type": m.ContentType}
		if h.blob != nil {
			if url, err := h.blob.DownloadURL(c.Request.Context(), auth.SurfaceFind, m.ObjectPath); err == nil {
				v["url"] = url
			}
		}
		out = append(out, v)
	}
	return out
}

func (h *MediaHandler) fail(c *gin.Context, what string, err error) {
	switch {
	case errors.Is(err, store.ErrNoMedia), errors.Is(err, store.ErrNoListing):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, tenancy.ErrNoTenant):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no organisation in this request"})
	default:
		h.log.Error(what, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not " + what})
	}
}
