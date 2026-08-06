package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/identity/domain/verification"
	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"github.com/tesserix/dwellm8/services/api/internal/platform/mail"
)

// Verification proves that the address a manager signed up with reaches them,
// before a firm is minted under it (#282).
type Verification struct {
	principals *store.Principals
	mailer     mail.Sender
	log        *slog.Logger
}

func NewVerification(p *store.Principals, m mail.Sender, log *slog.Logger) *Verification {
	return &Verification{principals: p, mailer: m, log: log}
}

// Routes mounts the pair. Open for the same reason /v1/onboarding is: the
// caller has no organisation yet, and the subject is their own sign-in.
func (h *Verification) Routes(r *authz.Registrar) {
	r.Open("POST /v1/verification/email",
		"the subject is the verified sign-in's own address — nothing another person's id could widen",
		h.Send)
	r.Open("POST /v1/verification/email/confirm",
		"the subject is the verified sign-in's own address, and the code is the authorisation",
		h.Confirm)
	r.Open("GET /v1/verification/email",
		"the subject is the verified sign-in itself — whether their own address is proved",
		h.Status)
}

type verificationStatus struct {
	Email    string `json:"email,omitempty"`
	Verified bool   `json:"verified"`
	// ResendAfter is when the button becomes live again, so the screen counts
	// down rather than refusing at the moment somebody taps.
	ResendAfter int `json:"resend_after_seconds,omitempty"`
}

func (h *Verification) Status(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	verified, err := h.principals.EmailVerified(r.Context(), p)
	if err != nil {
		h.log.Error("reading email verification", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not read the verification")
		return
	}
	out := verificationStatus{Email: p.Email, Verified: verified}
	if pending, err := h.principals.PendingEmailCode(r.Context(), p); err == nil {
		if wait := time.Until(pending.SentAt.Add(verification.ResendAfter)); wait > 0 {
			out.ResendAfter = int(wait.Seconds()) + 1
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// Send mails a code, and refuses to do it twice in a minute.
func (h *Verification) Send(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	if strings.TrimSpace(p.Email) == "" {
		writeError(w, http.StatusUnprocessableEntity, "this sign-in has no email address to verify")
		return
	}

	verified, err := h.principals.EmailVerified(r.Context(), p)
	if err != nil {
		h.log.Error("reading email verification", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not send the code")
		return
	}
	if verified {
		writeJSON(w, http.StatusOK, verificationStatus{Email: p.Email, Verified: true})
		return
	}

	// The hourly ceiling, before the minute cooldown: somebody who has already
	// had five codes is not helped by being told to wait sixty seconds.
	issued, oldest, err := h.principals.EmailCodesIssued(r.Context(), p, verification.IssueWindow)
	if err != nil {
		h.log.Error("counting codes issued", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not send the code")
		return
	}
	if wait, err := verification.NextIssue(issued, oldest, time.Now()); err != nil {
		secs := int(wait.Seconds()) + 1
		w.Header().Set("Retry-After", fmt.Sprint(secs))
		writeJSON(w, http.StatusTooManyRequests, map[string]any{
			"error":                err.Error(),
			"resend_after_seconds": secs,
		})
		return
	}

	pending, err := h.principals.PendingEmailCode(r.Context(), p)
	switch {
	case err == nil:
		if !verification.MayResend(pending, time.Now()) {
			wait := int(time.Until(pending.SentAt.Add(verification.ResendAfter)).Seconds()) + 1
			w.Header().Set("Retry-After", fmt.Sprint(wait))
			writeJSON(w, http.StatusTooManyRequests, map[string]any{
				"error":                "a code was just sent — check your inbox",
				"resend_after_seconds": wait,
			})
			return
		}
	case errors.Is(err, store.ErrNoCodePending):
	default:
		h.log.Error("reading the code in flight", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not send the code")
		return
	}

	code, err := h.principals.IssueEmailCode(r.Context(), p, p.Email)
	if err != nil {
		h.log.Error("issuing an email code", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not send the code")
		return
	}

	if err := h.mailer.Send(r.Context(), mail.Message{
		To:      p.Email,
		Subject: "Your Dwellm8 verification code",
		Body: fmt.Sprintf("%s is your Dwellm8 verification code.\n\n"+
			"It expires in %d minutes. If you did not ask for it, ignore this email —\n"+
			"nobody can use it without your sign-in.\n",
			code, int(verification.TTL.Minutes())),
	}); err != nil {
		// The code is already stored, so this is a delivery failure and must be
		// reported as one: telling the manager to check an inbox nothing was
		// sent to is the ten minutes nobody gets back.
		h.log.Error("mailing an email code", "surface", p.Surface, "error", err)
		writeError(w, http.StatusBadGateway, "the code could not be emailed — try again shortly")
		return
	}

	writeJSON(w, http.StatusAccepted, verificationStatus{
		Email: p.Email, Verified: false, ResendAfter: int(verification.ResendAfter.Seconds()),
	})
}

type confirmRequest struct {
	Code string `json:"code"`
}

// Confirm checks the code. Every refusal reads as itself, because "that did not
// work" sends somebody looking at their typing when the code has expired.
func (h *Verification) Confirm(w http.ResponseWriter, r *http.Request) {
	p, ok := auth.From(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "sign in first")
		return
	}
	var req confirmRequest
	body, err := io.ReadAll(io.LimitReader(r.Body, 1024))
	if err != nil || json.Unmarshal(body, &req) != nil {
		writeError(w, http.StatusBadRequest, "send the code")
		return
	}
	code := strings.TrimSpace(req.Code)
	if code == "" {
		writeError(w, http.StatusBadRequest, "send the code")
		return
	}

	err = h.principals.ConsumeEmailCode(r.Context(), p, code)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, verificationStatus{Email: p.Email, Verified: true})
	case errors.Is(err, verification.ErrWrongCode),
		errors.Is(err, verification.ErrExpired),
		errors.Is(err, verification.ErrAlreadyUsed),
		errors.Is(err, store.ErrNoCodePending):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	case errors.Is(err, verification.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, err.Error())
	default:
		h.log.Error("confirming an email code", "surface", p.Surface, "error", err)
		writeError(w, http.StatusInternalServerError, "could not check the code")
	}
}
