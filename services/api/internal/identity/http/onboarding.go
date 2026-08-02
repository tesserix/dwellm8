// Package http is the identity module's HTTP surface: the one call that turns
// a verified first sign-in into an organisation and a membership. Issue #31,
// ADR-0027 §6 — verification is not authorisation, and this is the step that
// mints the authorisation.
package http

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// Onboarding handles the first-sign-in call.
type Onboarding struct {
	principals *store.Principals
	log        *slog.Logger
}

func NewOnboarding(p *store.Principals, log *slog.Logger) *Onboarding {
	return &Onboarding{principals: p, log: log}
}

// Routes mounts the endpoint. It sits inside token verification and outside
// the membership resolver, because its caller by definition has no membership
// to resolve — the chicken this endpoint lays the egg for.
func (h *Onboarding) Routes(r *authz.Registrar) {
	r.Open("POST /v1/onboarding",
		"the caller has no organisation yet — this is the call that creates it; the verified token authenticates them and the surface decides what kind they may create",
		h.Onboard)
	r.Open("GET /v1/me",
		"the subject is the verified sign-in itself — nothing another person's id could widen",
		h.Me)
	r.Open("PATCH /v1/me",
		"the subject is the verified sign-in itself — a person editing their own name and email",
		h.UpdateMe)
}

type meResponse struct {
	PartyID     string `json:"party_id"`
	Phone       string `json:"phone,omitempty"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
}

// Me answers who this verified sign-in is — the Own app's profile (#240).
func (h *Onboarding) Me(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	person, err := h.principals.Lookup(r.Context(), p)
	if errors.Is(err, store.ErrUnknownPrincipal) {
		writeError(w, http.StatusNotFound, "this sign-in has no account yet")
		return
	}
	if err != nil {
		h.log.Error("reading a profile", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the profile")
		return
	}
	prof, err := h.principals.ProfileByParty(r.Context(), person.PartyID)
	if err != nil {
		h.log.Error("reading a profile", "party", person.PartyID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the profile")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		PartyID: prof.PartyID, Phone: prof.Phone, Email: prof.Email, DisplayName: prof.DisplayName,
	})
}

type updateMeRequest struct {
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

// UpdateMe is the person filling in their own PI after onboarding (#240).
// The verified phone never moves here.
func (h *Onboarding) UpdateMe(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	person, err := h.principals.Lookup(r.Context(), p)
	if errors.Is(err, store.ErrUnknownPrincipal) {
		writeError(w, http.StatusNotFound, "this sign-in has no account yet")
		return
	}
	if err != nil {
		h.log.Error("updating a profile", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the details")
		return
	}
	var req updateMeRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send display_name and/or email")
		return
	}
	if req.DisplayName == "" && req.Email == "" {
		writeError(w, http.StatusUnprocessableEntity, "send display_name or email — there is nothing else to change here")
		return
	}
	prof, err := h.principals.UpdateProfileByParty(r.Context(), person.PartyID, req.DisplayName, req.Email)
	if err != nil {
		h.log.Error("updating a profile", "party", person.PartyID, "error", err)
		writeError(w, http.StatusInternalServerError, "could not save the details")
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		PartyID: prof.PartyID, Phone: prof.Phone, Email: prof.Email, DisplayName: prof.DisplayName,
	})
}

type onboardRequest struct {
	OrganisationName string `json:"organisation_name"`
	// Slug is optional; left empty it is derived from the name.
	Slug string `json:"slug"`
}

type onboardResponse struct {
	OrganisationID string `json:"organisation_id"`
	PartyID        string `json:"party_id"`
	Role           string `json:"role"`
	Created        bool   `json:"created"`
}

func (h *Onboarding) Onboard(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}

	var req onboardRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send organisation_name, and optionally slug")
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = slugFrom(req.OrganisationName)
	}

	person, created, err := h.principals.Onboard(r.Context(), store.Onboarding{
		Principal:        p,
		OrganisationName: strings.TrimSpace(req.OrganisationName),
		Slug:             slug,
	}.Fill())
	switch {
	case err == nil:
		org, _ := person.Organisation()
		status := http.StatusOK
		if created {
			status = http.StatusCreated
		}
		writeJSON(w, status, onboardResponse{
			OrganisationID: string(org), PartyID: person.PartyID,
			Role: person.Memberships[0].Role, Created: created})
	case errors.Is(err, store.ErrOnboarding):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case isUniqueViolation(err):
		// The slug is somebody's. Only reachable when the caller chose one —
		// a derived slug carries enough randomness not to collide.
		writeError(w, http.StatusConflict, "that slug is taken — send another")
	default:
		h.log.Error("onboarding", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not create the organisation")
	}
}

// slugFrom turns a name into a handle: lower-cased, dashed, and suffixed with
// enough randomness that two firms called Sharma Estates never collide.
func slugFrom(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if len(base) > 40 {
		base = base[:40]
	}
	if base == "" {
		base = "org"
	}
	var rnd [3]byte
	_, _ = rand.Read(rnd[:])
	return fmt.Sprintf("%s-%x", base, rnd)
}

func isUniqueViolation(err error) bool {
	return strings.Contains(err.Error(), "23505") ||
		strings.Contains(err.Error(), "duplicate key")
}

func writeJSON(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
