package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Applications (#142), Gin-native: the formal step between enquiry and lease.
// Acceptance mints the lease draft with everything carried over and takes the
// listing off the market; a second acceptance for the unit is blocked naming
// the first; a decline carries its reason and starts the retention clock.

// ApplicationsHandler serves both sides.
type ApplicationsHandler struct {
	apps      *store.Applications
	listings  *store.Listings
	prospects *service.Prospects
	drafter   service.TenancyDrafter
	log       *slog.Logger
}

// NewApplications builds the handler.
func NewApplications(a *store.Applications, l *store.Listings, p *service.Prospects,
	d service.TenancyDrafter, log *slog.Logger) *ApplicationsHandler {
	return &ApplicationsHandler{apps: a, listings: l, prospects: p, drafter: d, log: log}
}

// OwnerRoutes mounts the review side.
func (h *ApplicationsHandler) OwnerRoutes(r *ginx.Registrar) {
	r.Handle(http.MethodGet, "/v1/applications", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.Queue)
	op := authz.Check{Relation: "can_operate", Object: authz.Organisation()}
	r.Handle(http.MethodPost, "/v1/applications/:id/review", op, h.Review)
	r.Handle(http.MethodPost, "/v1/applications/:id/accept", op, h.Accept)
	r.Handle(http.MethodPost, "/v1/applications/:id/decline", op, h.Decline)
}

// PublicRoutes mounts the applicant side.
func (h *ApplicationsHandler) PublicRoutes(r *ginx.Registrar) {
	r.Open(http.MethodPost, "/v1/public/listings/:id/applications", openReason, h.Apply)
	r.Open(http.MethodGet, "/v1/public/applications", openReason, h.Mine)
	r.Open(http.MethodPost, "/v1/public/applications/:id/withdraw", openReason, h.Withdraw)
}

type applyRequest struct {
	MoveIn     string `json:"move_in" binding:"required"`
	TermMonths int    `json:"term_months" binding:"omitempty,min=1,max=60"`
	OfferMinor *int64 `json:"offer_minor" binding:"omitempty,gt=0"`
	Note       string `json:"note" binding:"omitempty,max=2000"`
}

// Apply files the application for the token's verified prospect — everything
// they have already given carries forward from here, typed once.
func (h *ApplicationsHandler) Apply(c *gin.Context) {
	var req applyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	moveIn, err := time.Parse("2006-01-02", req.MoveIn)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "move_in must be a date, as YYYY-MM-DD"})
		return
	}
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	if !p.Verified {
		c.JSON(http.StatusForbidden, gin.H{"error": "verify a phone number before applying"})
		return
	}

	a, err := h.apps.Apply(c.Request.Context(), c.Param("id"), p.ID, moveIn,
		req.TermMonths, req.OfferMinor, req.Note)
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, applicationView(a))
	case errors.Is(err, store.ErrNotVerified):
		c.JSON(http.StatusForbidden, gin.H{"error": "verify a phone number before applying"})
	case errors.Is(err, store.ErrListingNotLive), errors.Is(err, store.ErrNoListing):
		c.JSON(http.StatusConflict, gin.H{
			"error": "this listing is no longer available — search for similar homes in the same locality"})
	default:
		h.fail(c, "filing an application", err)
	}
}

// Mine is the applicant's own timeline.
func (h *ApplicationsHandler) Mine(c *gin.Context) {
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	list, err := h.apps.ForProspect(c.Request.Context(), p.ID)
	if err != nil {
		h.fail(c, "reading applications", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"applications": applicationViews(list)})
}

// Withdraw is the applicant's own exit.
func (h *ApplicationsHandler) Withdraw(c *gin.Context) {
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	if err := h.apps.Withdraw(c.Request.Context(), p.ID, c.Param("id")); err != nil {
		h.fail(c, "withdrawing", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "withdrawn"})
}

// Queue is the owner's review list.
func (h *ApplicationsHandler) Queue(c *gin.Context) {
	list, err := h.apps.ForOwner(c.Request.Context(), c.Query("state"))
	if err != nil {
		h.fail(c, "reading the queue", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"applications": applicationViews(list)})
}

// Review acknowledges an application is being looked at.
func (h *ApplicationsHandler) Review(c *gin.Context) {
	if err := h.apps.Review(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, "moving to review", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "under_review"})
}

type acceptRequest struct {
	TenantName  string `json:"tenant_name" binding:"required"`
	TenantPhone string `json:"tenant_phone" binding:"required"`
	TenantEmail string `json:"tenant_email" binding:"omitempty,email"`
	// RentMinor overrides when negotiation moved the number; zero carries the
	// application's offer, or the listing's asking rent.
	RentMinor int64 `json:"rent_minor" binding:"omitempty,gt=0"`
}

// Accept mints the lease draft with the application's terms carried over and
// records the acceptance — the listing pauses off the market while the
// paperwork runs, and it moves to let when the tenancy actually starts.
func (h *ApplicationsHandler) Accept(c *gin.Context) {
	var req acceptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	a, err := h.apps.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "reading the application", err)
		return
	}
	l, err := h.listings.Get(c.Request.Context(), a.ListingID)
	if err != nil {
		h.fail(c, "reading the listing", err)
		return
	}

	rent := l.Costs.RentMinor
	if a.OfferMinor != nil {
		rent = *a.OfferMinor
	}
	if req.RentMinor > 0 {
		rent = req.RentMinor
	}
	start := effective.DateOf(a.MoveIn, time.UTC)
	end := effective.DateOf(a.MoveIn.AddDate(0, a.TermMonths, 0), time.UTC)

	leaseID, err := h.drafter.DraftTenancy(c.Request.Context(), service.TenancyApplication{
		PropertyID: a.PropertyID, UnitID: a.UnitID,
		Start: start, End: end,
		RentMinor: rent, DepositMinor: l.Costs.DepositMinor,
		TenantName: req.TenantName, TenantPhone: req.TenantPhone, TenantEmail: req.TenantEmail,
	})
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	if err := h.apps.Accept(c.Request.Context(), a.ID, leaseID); err != nil {
		switch {
		case errors.Is(err, store.ErrAlreadyAccepted):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			h.fail(c, "recording the acceptance", err)
		}
		return
	}
	if l.State == domain.StateLive {
		if err := h.listings.Move(c.Request.Context(), a.ListingID, domain.StatePaused,
			events.Actor{Kind: events.ActorSystem}); err != nil {
			h.log.Error("pausing the listing after acceptance", "listing", a.ListingID, "error", err)
		}
	}
	c.JSON(http.StatusCreated, gin.H{
		"id": a.ID, "state": "accepted", "lease_id": leaseID, "lease_state": "draft",
		"rent_minor": rent,
	})
}

// Decline records the decision with its reason and starts the retention clock.
func (h *ApplicationsHandler) Decline(c *gin.Context) {
	var req struct {
		Reason string `json:"reason" binding:"required,min=3,max=500"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "a decline carries its reason"})
		return
	}
	if err := h.apps.Decline(c.Request.Context(), c.Param("id"), req.Reason); err != nil {
		h.fail(c, "declining", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "declined"})
}

func (h *ApplicationsHandler) fail(c *gin.Context, what string, err error) {
	switch {
	case errors.Is(err, store.ErrNoApplication), errors.Is(err, store.ErrNoListing):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, store.ErrNotDecidable):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, tenancy.ErrNoTenant):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no organisation in this request"})
	default:
		h.log.Error(what, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not " + what})
	}
}

func applicationView(a store.Application) gin.H {
	out := gin.H{
		"id": a.ID, "listing_id": a.ListingID, "state": a.State,
		"move_in": a.MoveIn.Format("2006-01-02"), "term_months": a.TermMonths,
		"created_at": a.CreatedAt.UTC().Format(time.RFC3339),
	}
	if a.Headline != "" {
		out["headline"] = a.Headline
	}
	if a.OfferMinor != nil {
		out["offer_minor"] = *a.OfferMinor
	}
	if a.Note != "" {
		out["note"] = a.Note
	}
	if a.DeclineReason != "" {
		out["decline_reason"] = a.DeclineReason
	}
	if a.LeaseID != "" {
		out["lease_id"] = a.LeaseID
	}
	return out
}

func applicationViews(list []store.Application) []gin.H {
	out := make([]gin.H, 0, len(list))
	for _, a := range list {
		out = append(out, applicationView(a))
	}
	return out
}
