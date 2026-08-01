package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/service"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// tokenHeader carries the browsing token. A header rather than a bearer so the
// public site can also send a signed-in user's Authorization one day without
// the two fighting over a slot.
const tokenHeader = "X-Dwellm8-Prospect"

// openReason is stated once: these routes serve people who by definition have
// no account, and each is limited and shaped for that.
const openReason = "the public listing site serves strangers (ADR-0019): reads are the schema's " +
	"one public branch, writes require a browsing token and the verification point"

// Public handles the anonymous side of the funnel.
type Public struct {
	listings  *service.Listings
	prospects *service.Prospects
	enquiries *service.Enquiries
	log       *slog.Logger
}

// NewPublic builds the handler.
func NewPublic(l *service.Listings, p *service.Prospects, e *service.Enquiries, log *slog.Logger) *Public {
	return &Public{listings: l, prospects: p, enquiries: e, log: log}
}

// Routes mounts the public surface as Open routes: deliberately unguarded,
// with the reason stated at registration (ADR-0020).
func (h *Public) Routes(r *authz.Registrar) {
	r.Open("GET /v1/public/listings", openReason, h.Search)
	r.Open("GET /v1/public/listings/{id}", openReason, h.Detail)
	r.Open("POST /v1/public/prospects", openReason, h.Issue)
	r.Open("POST /v1/public/prospects/verify", openReason, h.Verify)
	r.Open("POST /v1/public/prospects/confirm", openReason, h.Confirm)
	r.Open("GET /v1/public/shortlist", openReason, h.Shortlist)
	r.Open("POST /v1/public/shortlist", openReason, h.ShortlistAdd)
	r.Open("DELETE /v1/public/shortlist/{listing}", openReason, h.ShortlistRemove)
	r.Open("POST /v1/public/enquiries", openReason, h.Enquire)
	r.Open("GET /v1/public/enquiries", openReason, h.Timeline)
}

// cardResponse is what a stranger sees: locality, never an address; every cost
// itemised and totalled (#133, #134).
type cardResponse struct {
	ID             string  `json:"id"`
	Headline       string  `json:"headline"`
	Locality       string  `json:"locality"`
	City           string  `json:"city"`
	StateCode      string  `json:"state_code"`
	Bedrooms       int     `json:"bedrooms,omitempty"`
	CarpetAreaSqft float64 `json:"carpet_area_sqft,omitempty"`
	AvailableFrom  string  `json:"available_from,omitempty"`

	RentMinor         int64  `json:"rent_minor"`
	MaintenanceMinor  int64  `json:"maintenance_minor"`
	ParkingMinor      int64  `json:"parking_minor"`
	OtherMonthlyMinor int64  `json:"other_monthly_minor"`
	DepositMinor      int64  `json:"deposit_minor"`
	OneTimeMinor      int64  `json:"one_time_minor"`
	TotalMonthlyMinor int64  `json:"total_monthly_minor"`
	TotalOneTimeMinor int64  `json:"total_one_time_minor"`
	Currency          string `json:"currency"`

	PublishedAt string `json:"published_at"`
}

func toCard(c store.Card) cardResponse {
	out := cardResponse{
		ID: c.ID, Headline: c.Headline, Locality: c.Locality, City: c.City,
		StateCode: c.StateCode, Bedrooms: c.Bedrooms, CarpetAreaSqft: c.CarpetAreaSqft,
		RentMinor: c.Costs.RentMinor, MaintenanceMinor: c.Costs.MaintenanceMinor,
		ParkingMinor: c.Costs.ParkingMinor, OtherMonthlyMinor: c.Costs.OtherMonthlyMinor,
		DepositMinor: c.Costs.DepositMinor, OneTimeMinor: c.Costs.OneTimeMinor,
		TotalMonthlyMinor: c.TotalMonthlyMinor, TotalOneTimeMinor: c.TotalOneTimeMinor,
		Currency:    c.Currency,
		PublishedAt: c.PublishedAt.UTC().Format(time.RFC3339),
	}
	if c.AvailableFrom != nil {
		out.AvailableFrom = c.AvailableFrom.Format("2006-01-02")
	}
	return out
}

// Search is the front door (#133).
func (h *Public) Search(w http.ResponseWriter, r *http.Request) {
	q := store.Query{
		City:     r.URL.Query().Get("city"),
		Locality: r.URL.Query().Get("locality"),
	}
	if v := r.URL.Query().Get("max_rent_minor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "max_rent_minor must be a non-negative integer")
			return
		}
		q.MaxRentMinor = n
	}
	if v := r.URL.Query().Get("bedrooms"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > 20 {
			writeError(w, http.StatusBadRequest, "bedrooms must be between 0 and 20")
			return
		}
		q.MinBedrooms = n
	}
	if v := r.URL.Query().Get("available_by"); v != "" {
		t, err := time.Parse("2006-01-02", v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "available_by must be a date, as YYYY-MM-DD")
			return
		}
		q.AvailableBy = t
	}
	if v := r.URL.Query().Get("after"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "after is the created_at cursor from the previous page")
			return
		}
		q.After = t
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		q.Limit, _ = strconv.Atoi(v)
	}

	cards, err := h.listings.Search(r.Context(), q)
	if err != nil {
		h.log.Error("public search", "error", err)
		writeError(w, http.StatusInternalServerError, "could not search listings")
		return
	}
	out := make([]cardResponse, 0, len(cards))
	for _, c := range cards {
		out = append(out, toCard(c))
	}
	// The cursor for the next page rides along; a stable sort key means no
	// listing is skipped when one is published mid-scroll.
	resp := map[string]any{"listings": out}
	if len(cards) > 0 {
		resp["next_after"] = cards[len(cards)-1].CreatedAt.UTC().Format(time.RFC3339)
	}
	writeJSON(w, http.StatusOK, resp)
}

// Detail is the listing page (#134). Gone is stated plainly: a page that kept
// advertising a let flat would cost somebody a Saturday.
func (h *Public) Detail(w http.ResponseWriter, r *http.Request) {
	c, err := h.listings.Detail(r.Context(), r.PathValue("id"))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, toCard(c))
	case errors.Is(err, store.ErrNoListing):
		writeJSON(w, http.StatusNotFound, map[string]string{
			"error": "this listing is no longer available"})
	default:
		h.log.Error("public detail", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the listing")
	}
}

// Issue mints a browsing token.
func (h *Public) Issue(w http.ResponseWriter, r *http.Request) {
	token, err := h.prospects.Issue(r.Context())
	if err != nil {
		h.log.Error("issuing a prospect token", "error", err)
		writeError(w, http.StatusInternalServerError, "could not start a session")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"token": token})
}

// Verify sends the OTP — the verification point (#137).
func (h *Public) Verify(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	err := h.prospects.StartVerification(r.Context(), token(r), req.Phone)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "code_sent"})
	case errors.Is(err, service.ErrBadPhone):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	case errors.Is(err, service.ErrNoVerifier):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		h.log.Error("starting verification", "error", err)
		writeError(w, http.StatusInternalServerError, "could not send the code")
	}
}

// Confirm checks the OTP.
func (h *Public) Confirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	err := h.prospects.ConfirmVerification(r.Context(), token(r), req.Phone, req.Code)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "verified"})
	case errors.Is(err, service.ErrBadPhone):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	case errors.Is(err, service.ErrNoVerifier):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		// A wrong code and a provider outage are one refusal: distinguishing
		// them would let a caller brute-force in comfort.
		writeError(w, http.StatusUnprocessableEntity, "the code did not match")
	}
}

// ShortlistAdd saves a listing.
func (h *Public) ShortlistAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListingID string `json:"listing_id"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	h.shortlistMutation(w, r, h.prospects.ShortlistAdd, req.ListingID)
}

// ShortlistRemove takes one off.
func (h *Public) ShortlistRemove(w http.ResponseWriter, r *http.Request) {
	h.shortlistMutation(w, r, h.prospects.ShortlistRemove, r.PathValue("listing"))
}

func (h *Public) shortlistMutation(w http.ResponseWriter, r *http.Request,
	f func(ctx context.Context, token, listingID string) error, listingID string) {
	if listingID == "" {
		writeError(w, http.StatusBadRequest, "no listing named")
		return
	}
	err := f(r.Context(), token(r), listingID)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	default:
		h.log.Error("shortlist update", "error", err)
		writeError(w, http.StatusInternalServerError, "could not update the shortlist")
	}
}

// Shortlist lists the saved listings, honestly — a let one says so.
func (h *Public) Shortlist(w http.ResponseWriter, r *http.Request) {
	items, err := h.prospects.Shortlist(r.Context(), token(r))
	switch {
	case err == nil:
		type item struct {
			ListingID string `json:"listing_id"`
			Headline  string `json:"headline"`
			Locality  string `json:"locality"`
			City      string `json:"city"`
			RentMinor int64  `json:"rent_minor"`
			State     string `json:"state"`
		}
		out := make([]item, 0, len(items))
		for _, it := range items {
			out = append(out, item{it.ListingID, it.Headline, it.Locality, it.City,
				it.RentMinor, it.State})
		}
		writeJSON(w, http.StatusOK, map[string]any{"shortlist": out})
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	default:
		h.log.Error("reading a shortlist", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the shortlist")
	}
}

// Enquire records an enquiry, an inspection request or a callback (#137).
func (h *Public) Enquire(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListingID    string `json:"listing_id"`
		Kind         string `json:"kind"`
		Message      string `json:"message"`
		ScheduledFor string `json:"scheduled_for"`
	}
	if err := decode(w, r, &req); err != nil {
		return
	}
	var scheduled *time.Time
	if req.ScheduledFor != "" {
		t, err := time.Parse(time.RFC3339, req.ScheduledFor)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, "scheduled_for must be RFC 3339")
			return
		}
		scheduled = &t
	}
	if req.Kind == "" {
		req.Kind = "enquiry"
	}

	e, err := h.enquiries.Enquire(r.Context(), token(r), req.ListingID, req.Kind, req.Message, scheduled)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, map[string]string{
			"id": e.ID, "state": e.State,
			"acknowledgement": "your enquiry has reached the manager; expect a response shortly"})
	case errors.Is(err, store.ErrNotVerified):
		writeError(w, http.StatusForbidden, "verify a phone number before making contact")
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	case errors.Is(err, store.ErrListingNotLive), errors.Is(err, store.ErrNoListing):
		// Honest, with a pointer forward rather than silence (#137's edge case).
		writeJSON(w, http.StatusConflict, map[string]string{
			"error": "this listing is no longer available — search for similar homes in the same locality"})
	case errors.Is(err, store.ErrNoEnquiry):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		h.log.Error("recording an enquiry", "error", err)
		writeError(w, http.StatusInternalServerError, "could not record the enquiry")
	}
}

// Timeline is the prospect's own history.
func (h *Public) Timeline(w http.ResponseWriter, r *http.Request) {
	list, err := h.enquiries.Timeline(r.Context(), token(r))
	switch {
	case err == nil:
		out := make([]enquiryResponse, 0, len(list))
		for _, e := range list {
			out = append(out, toEnquiryResponse(e))
		}
		writeJSON(w, http.StatusOK, map[string]any{"enquiries": out})
	case errors.Is(err, service.ErrBadToken):
		writeError(w, http.StatusUnauthorized, "start a session first")
	default:
		h.log.Error("reading a prospect timeline", "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the history")
	}
}

func token(r *http.Request) string { return r.Header.Get(tokenHeader) }
