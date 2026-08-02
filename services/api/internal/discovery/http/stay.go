package http

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	moneyservice "github.com/tesserix/dwellm8/services/api/internal/money/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/ginx"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// HomeStay's HTTP surface (#233), Gin-native on platform/ginx. Host routes
// carry ADR-0020 checks; guest routes are Open for the funnel's reason and
// ride the prospect token with its verification gate.

// Stay handles both sides of a short-stay booking.
type Stay struct {
	stay      *store.Stay
	prospects *service.Prospects
	payments  *moneyservice.Payments
	log       *slog.Logger
}

// NewStay builds the handler.
func NewStay(s *store.Stay, p *service.Prospects, log *slog.Logger) *Stay {
	return &Stay{stay: s, prospects: p, log: log}
}

// WithPayments teaches the guest side to take money through the provider
// chain (ADR-0011). Optional: nil answers 503 on the pay route rather than
// the process failing to start, the DocURLKey shape.
func (h *Stay) WithPayments(p *moneyservice.Payments) *Stay {
	h.payments = p
	return h
}

// HostRoutes mounts the authenticated side.
func (h *Stay) HostRoutes(r *ginx.Registrar) {
	org := authz.Check{Relation: "can_operate", Object: authz.Organisation()}
	r.Handle(http.MethodPost, "/v1/stay/listings", org, h.CreateListing)
	r.Handle(http.MethodGet, "/v1/stay/listings", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.HostListings)
	r.Handle(http.MethodPost, "/v1/stay/listings/:id/publish", org, h.move("live"))
	r.Handle(http.MethodPost, "/v1/stay/listings/:id/pause", org, h.move("paused"))
	r.Handle(http.MethodPost, "/v1/stay/listings/:id/withdraw", org, h.move("withdrawn"))
	r.Handle(http.MethodGet, "/v1/stay/bookings", authz.Check{
		Relation: "can_view", Object: authz.Organisation()}, h.HostBookings)
	r.Handle(http.MethodPost, "/v1/stay/bookings/:id/confirm", org, h.Confirm)
	r.Handle(http.MethodPost, "/v1/stay/bookings/:id/cancel", org, h.HostCancel)
	r.Handle(http.MethodPost, "/v1/stay/bookings/:id/complete", org, h.Complete)
}

// PublicRoutes mounts the guest side.
func (h *Stay) PublicRoutes(r *ginx.Registrar) {
	r.Open(http.MethodGet, "/v1/public/stay", openReason, h.Search)
	r.Open(http.MethodGet, "/v1/public/stay/:id", openReason, h.Detail)
	r.Open(http.MethodPost, "/v1/public/stay/:id/bookings", openReason, h.Book)
	r.Open(http.MethodPost, "/v1/public/stay/bookings/:id/pay", openReason, h.Pay)
	r.Open(http.MethodPost, "/v1/public/stay/bookings/:id/cancel", openReason, h.GuestCancel)
}

type stayListingRequest struct {
	PropertyID    string `json:"property_id" binding:"required"`
	UnitID        string `json:"unit_id" binding:"required"`
	Headline      string `json:"headline" binding:"required"`
	Locality      string `json:"locality" binding:"required"`
	City          string `json:"city" binding:"required"`
	StateCode     string `json:"state_code" binding:"required,len=2"`
	NightlyMinor  int64  `json:"nightly_minor" binding:"required,gt=0"`
	CleaningMinor int64  `json:"cleaning_minor" binding:"omitempty,gte=0"`
	DepositMinor  int64  `json:"deposit_minor" binding:"omitempty,gte=0"`
	MaxGuests     int    `json:"max_guests" binding:"omitempty,min=1,max=20"`
	MinNights     int    `json:"min_nights" binding:"omitempty,min=1,max=90"`
	MaxNights     int    `json:"max_nights" binding:"omitempty,min=1,max=365"`
}

// CreateListing drafts a stay listing from live inventory.
func (h *Stay) CreateListing(c *gin.Context) {
	var req stayListingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	id, err := h.stay.CreateListing(c.Request.Context(), store.StayDraft{
		PropertyID: req.PropertyID, UnitID: req.UnitID, Headline: req.Headline,
		Locality: req.Locality, City: req.City, StateCode: req.StateCode,
		NightlyMinor: req.NightlyMinor, CleaningMinor: req.CleaningMinor,
		DepositMinor: req.DepositMinor, MaxGuests: req.MaxGuests,
		MinNights: req.MinNights, MaxNights: req.MaxNights,
	})
	if err != nil {
		h.fail(c, "creating a stay listing", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"id": id, "state": "draft"})
}

func (h *Stay) move(to string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := h.stay.MoveListing(c.Request.Context(), c.Param("id"), to); err != nil {
			h.fail(c, "moving a stay listing", err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": to})
	}
}

// HostListings lists the host's stay inventory.
func (h *Stay) HostListings(c *gin.Context) {
	list, err := h.stay.HostListings(c.Request.Context())
	if err != nil {
		h.fail(c, "listing stay inventory", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"listings": stayViews(list, true)})
}

// HostBookings is the host's calendar.
func (h *Stay) HostBookings(c *gin.Context) {
	list, err := h.stay.HostBookings(c.Request.Context())
	if err != nil {
		h.fail(c, "listing stay bookings", err)
		return
	}
	out := make([]gin.H, 0, len(list))
	for _, b := range list {
		out = append(out, bookingView(b))
	}
	c.JSON(http.StatusOK, gin.H{"bookings": out})
}

// Confirm commits a held booking.
func (h *Stay) Confirm(c *gin.Context) {
	err := h.stay.Confirm(c.Request.Context(), c.Param("id"))
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "confirmed"})
	case errors.Is(err, store.ErrHoldExpired):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	default:
		h.fail(c, "confirming a stay", err)
	}
}

// HostCancel releases the nights from the host's side.
func (h *Stay) HostCancel(c *gin.Context) {
	if err := h.stay.CancelByHost(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, "cancelling a stay", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "cancelled", "by": "host"})
}

// Complete closes a stay after checkout.
func (h *Stay) Complete(c *gin.Context) {
	if err := h.stay.Complete(c.Request.Context(), c.Param("id")); err != nil {
		h.fail(c, "completing a stay", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "completed"})
}

// Search is the guest's browse.
func (h *Stay) Search(c *gin.Context) {
	list, err := h.stay.Search(c.Request.Context(), c.Query("city"))
	if err != nil {
		h.fail(c, "searching stays", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"listings": stayViews(list, false)})
}

// Detail is one stay listing with its whole price.
func (h *Stay) Detail(c *gin.Context) {
	l, err := h.stay.Live(c.Request.Context(), c.Param("id"))
	if err != nil {
		h.fail(c, "reading a stay", err)
		return
	}
	c.JSON(http.StatusOK, stayView(l, false))
}

type bookRequest struct {
	CheckIn  string `json:"check_in" binding:"required"`
	CheckOut string `json:"check_out" binding:"required"`
	Guests   int    `json:"guests" binding:"required,min=1,max=20"`
}

// Book holds the nights for the token's verified prospect: fifteen minutes to
// confirm, the exclusion constraint deciding every race.
func (h *Stay) Book(c *gin.Context) {
	var req bookRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
		return
	}
	in, err1 := time.Parse("2006-01-02", req.CheckIn)
	out, err2 := time.Parse("2006-01-02", req.CheckOut)
	if err1 != nil || err2 != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "check_in and check_out are dates, as YYYY-MM-DD"})
		return
	}
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	if !p.Verified {
		c.JSON(http.StatusForbidden, gin.H{"error": "verify a phone number before booking a stay"})
		return
	}

	b, err := h.stay.Hold(c.Request.Context(), c.Param("id"), p.ID, in, out, req.Guests)
	switch {
	case err == nil:
		c.JSON(http.StatusCreated, bookingView(b))
	case errors.Is(err, store.ErrDatesTaken):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrStayRules):
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
	case errors.Is(err, store.ErrNotVerified):
		c.JSON(http.StatusForbidden, gin.H{"error": "verify a phone number before booking a stay"})
	default:
		h.fail(c, "booking a stay", err)
	}
}

// Pay starts the collection that turns a hold into a stay: UPI through the
// provider chain (ADR-0011), the amount frozen at booking, one payment per
// booking however many times the button is pressed. Confirmation is the
// money.payment.received consumer's — a webhook alone moves the state, this
// route only starts the attempt.
func (h *Stay) Pay(c *gin.Context) {
	if h.payments == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "payments are not configured"})
		return
	}
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	b, err := h.stay.GuestBooking(c.Request.Context(), p.ID, c.Param("id"))
	if err != nil {
		h.fail(c, "reading the booking", err)
		return
	}
	if b.State != "held" {
		c.JSON(http.StatusConflict, gin.H{"error": "only a held booking can be paid for — this one is " + b.State})
		return
	}
	if b.HoldExpiresAt != nil && b.HoldExpiresAt.Before(time.Now()) {
		c.JSON(http.StatusConflict, gin.H{"error": "the hold has expired — book again"})
		return
	}

	// The collection is written into the host's organisation, scoped here the
	// way the resident surface scopes into the landlord's: from the row the
	// server read, never from anything the caller sent.
	ctx := tenancy.With(c.Request.Context(), tenancy.ID(b.TenantID))
	payment, order, err := h.payments.Collect(ctx, moneyservice.CollectRequest{
		TenantID: b.TenantID, Property: b.PropertyID, Unit: b.UnitID,
		PayerID: p.ID, PayerName: "HomeStay guest", PayerContact: p.ContactMasked,
		Amount: moneyservice.Minor(b.TotalMinor), Method: moneyservice.MethodUPIIntent,
		IdempotencyKey: "stay:" + b.ID,
	})
	if err != nil {
		h.log.Error("starting a stay collection", "booking", b.ID, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": "could not start the payment — try again shortly"})
		return
	}
	if err := h.stay.AttachPayment(c.Request.Context(), p.ID, b.ID, payment.ID); err != nil &&
		!errors.Is(err, store.ErrNoStay) {
		// ErrNoStay here means a retry already attached this payment; anything
		// else is real.
		h.fail(c, "attaching the payment", err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{
		"booking_id": b.ID, "payment_id": payment.ID, "status": string(payment.Status),
		"amount_minor": b.TotalMinor, "provider_order_id": order.ProviderOrderID,
		"pay_url": order.PayURL, "pay_token": order.PayToken,
	})
}

// GuestCancel releases the token holder's own booking.
func (h *Stay) GuestCancel(c *gin.Context) {
	p, err := h.prospects.Resolve(c.Request.Context(), c.GetHeader(tokenHeader))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "start a session first"})
		return
	}
	if err := h.stay.CancelByGuest(c.Request.Context(), p.ID, c.Param("id")); err != nil {
		h.fail(c, "cancelling a stay", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": c.Param("id"), "state": "cancelled", "by": "guest"})
}

func (h *Stay) fail(c *gin.Context, what string, err error) {
	switch {
	case errors.Is(err, store.ErrNoStay):
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
	case errors.Is(err, tenancy.ErrNoTenant):
		c.JSON(http.StatusUnauthorized, gin.H{"error": "no organisation in this request"})
	default:
		h.log.Error(what, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not " + what})
	}
}

func stayView(l store.StayListing, host bool) gin.H {
	out := gin.H{
		"id": l.ID, "headline": l.Headline, "locality": l.Locality, "city": l.City,
		"state_code": l.StateCode, "nightly_minor": l.NightlyMinor,
		"cleaning_minor": l.CleaningMinor, "deposit_minor": l.DepositMinor,
		"max_guests": l.MaxGuests, "min_nights": l.MinNights, "max_nights": l.MaxNights,
		"currency": "INR",
	}
	if host {
		out["state"] = l.State
		out["property_id"] = l.PropertyID
		out["unit_id"] = l.UnitID
	}
	return out
}

func stayViews(list []store.StayListing, host bool) []gin.H {
	out := make([]gin.H, 0, len(list))
	for _, l := range list {
		out = append(out, stayView(l, host))
	}
	return out
}

func bookingView(b store.Booking) gin.H {
	out := gin.H{
		"id": b.ID, "listing_id": b.ListingID, "state": b.State, "guests": b.Guests,
		"check_in": b.CheckIn.Format("2006-01-02"), "check_out": b.CheckOut.Format("2006-01-02"),
		"nightly_minor": b.NightlyMinor, "cleaning_minor": b.CleaningMinor,
		"total_minor": b.TotalMinor, "currency": "INR",
	}
	if b.HoldExpiresAt != nil {
		out["hold_expires_at"] = b.HoldExpiresAt.UTC().Format(time.RFC3339)
	}
	return out
}
