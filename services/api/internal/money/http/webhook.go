// Package http is the money module's HTTP surface.
//
// The webhook handler is the one place in the codebase where reading the
// request body the ordinary way is a security bug. Both aggregators sign the
// exact bytes they sent, so the body must be read once, kept, verified, and
// only then decoded. json.NewDecoder(r.Body).Decode(&v) — the idiomatic line,
// the one every other handler in every other service uses — destroys the
// evidence before it is checked, and the failure is invisible in tests that
// build the body themselves.
package http

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
	"strings"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/service"
)

// maxWebhookBody is a ceiling on what will be read into memory. A webhook is a
// few kilobytes; anything approaching this is not a webhook.
const maxWebhookBody = 1 << 20 // 1 MiB

// Ingester is the narrow slice of the collection service this handler needs.
//
// An interface rather than *service.Payments so the test can observe exactly
// what the handler passes on — which, for this handler, is the whole of what it
// must get right. A test that cannot see the bytes cannot prove they survived.
type Ingester interface {
	IngestWebhook(ctx context.Context, providerName string, w provider.Webhook,
		claimed collect.Status, providerRef, eventID string) (collect.Decision, error)
}

var _ Ingester = (*service.Payments)(nil)

// Webhooks handles provider deliveries.
type Webhooks struct {
	payments Ingester
	log      *slog.Logger
}

func NewWebhooks(p Ingester, log *slog.Logger) *Webhooks {
	return &Webhooks{payments: p, log: log}
}

// Cashfree ingests one delivery.
//
// It answers 200 to everything it manages to read, including deliveries it
// parks. That is deliberate: a non-2xx makes Cashfree retry, and there is
// nothing to retry for a delivery with a bad signature or one naming a payment
// we do not have. Both are kept in the inbox for the sweep instead, which is
// what "parked, not dropped" means operationally.
func (h *Webhooks) Cashfree(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, maxWebhookBody))
	if err != nil {
		http.Error(w, "unreadable body", http.StatusBadRequest)
		return
	}

	delivery := provider.Webhook{
		Body:      body, // the exact bytes, kept before anything parses them
		Signature: r.Header.Get("x-webhook-signature"),
		Timestamp: r.Header.Get("x-webhook-timestamp"),
	}

	// Parsed only to find out what the delivery is about. Nothing read here is
	// believed — the signature check above operates on the raw bytes, and the
	// claim below is a claim until the provider is asked.
	var payload struct {
		Type string `json:"type"`
		Data struct {
			Order struct {
				OrderID string `json:"order_id"`
			} `json:"order"`
			Payment struct {
				Status string `json:"payment_status"`
			} `json:"payment"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		// Unparseable, and still signed by somebody. Answering 200 stops a retry
		// loop over something that will never parse.
		h.log.Warn("unparseable cashfree delivery", "error", err)
		w.WriteHeader(http.StatusOK)
		return
	}

	claimed, err := cashfreeClaimedStatus(payload.Data.Payment.Status, payload.Type)
	if err != nil {
		h.log.Warn("cashfree delivery claims a status we have no state for",
			"type", payload.Type, "status", payload.Data.Payment.Status)
		// Left empty on purpose: Decide parks an unknown claim rather than
		// guessing, and the park reason records that it was unsupported.
		claimed = ""
	}

	eventID := r.Header.Get("x-webhook-id")
	if eventID == "" {
		// Cashfree does not always send one. The signature plus the timestamp is
		// what makes a delivery distinct in that case, and the inbox's unique
		// index needs something stable rather than something unique-per-request.
		eventID = delivery.Timestamp + ":" + delivery.Signature
	}

	decision, err := h.payments.IngestWebhook(r.Context(),
		provider.CashfreeName, delivery, claimed, payload.Data.Order.OrderID, eventID)
	if err != nil {
		// Our fault, not theirs — a database that is down, say. This is the one
		// case worth a retry, so it is the one case that is not a 200.
		h.log.Error("ingesting a cashfree delivery", "event", eventID, "error", err)
		http.Error(w, "could not process the delivery", http.StatusInternalServerError)
		return
	}

	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{
		"disposition": string(decision.Disposition),
	})
}

// cashfreeClaimedStatus maps a delivery's claim into our vocabulary. It refuses
// to guess, for the same reason the adapter's translation does: a status this
// system has no state for is parked, and inventing one for it would be the
// system agreeing with a message it does not understand.
func cashfreeClaimedStatus(paymentStatus, eventType string) (collect.Status, error) {
	if s := strings.ToUpper(strings.TrimSpace(paymentStatus)); s != "" {
		return cashfreeStatus(s)
	}
	// Some deliveries carry the claim in the event type instead.
	switch strings.ToUpper(strings.TrimSpace(eventType)) {
	case "PAYMENT_SUCCESS_WEBHOOK":
		return collect.StatusCaptured, nil
	case "PAYMENT_FAILED_WEBHOOK":
		return collect.StatusFailed, nil
	case "PAYMENT_USER_DROPPED_WEBHOOK":
		return collect.StatusExpired, nil
	}
	return "", errUnknownClaim
}

var errUnknownClaim = &unknownClaim{}

type unknownClaim struct{}

func (*unknownClaim) Error() string { return "money: a claim with no state behind it" }

func cashfreeStatus(s string) (collect.Status, error) {
	switch s {
	case "SUCCESS":
		return collect.StatusCaptured, nil
	case "PENDING", "NOT_ATTEMPTED":
		return collect.StatusAttempted, nil
	case "FAILED":
		return collect.StatusFailed, nil
	case "USER_DROPPED":
		return collect.StatusExpired, nil
	case "CANCELLED", "VOID":
		return collect.StatusCancelled, nil
	}
	return "", errUnknownClaim
}

// Routes mounts the module's HTTP surface.
//
// The webhook path is unauthenticated by design — Cashfree has no credential of
// ours to present — and the signature is what authenticates it. That makes this
// the one route where the handler's first action must be to distrust its input.
func (h *Webhooks) Routes(r *authz.Registrar) {
	r.Open("POST /v1/webhooks/cashfree",
		"authenticated by the HMAC signature over the delivery's own bytes (ADR-0011); no user is involved and no tuple could name one",
		h.Cashfree)
}
