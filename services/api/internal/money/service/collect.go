// Package service is where the provider and the store meet.
//
// It holds the two orders of operation that matter, and they are the whole
// reason this layer exists rather than a handler calling a store and an adapter
// in whatever sequence reads well:
//
//   - Collecting writes the payment BEFORE calling the provider. A crash
//     between the two leaves a `created` payment with no order, which a sweep
//     can finish. The other order leaves an order at Cashfree that nothing here
//     knows about, which is money a tenant may pay against a screen this system
//     has forgotten.
//   - Ingesting a webhook NEVER writes the status the webhook claims. It parks,
//     ignores, or asks the provider — ADR-0011 §4 — and only the provider's own
//     answer reaches ApplyConfirmed.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/money/provider"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
)

// Payments is the collection service.
type Payments struct {
	store     *store.Payments
	providers *provider.Registry
	log       *slog.Logger
	now       func() time.Time
}

func NewPayments(s *store.Payments, r *provider.Registry, log *slog.Logger) *Payments {
	return &Payments{store: s, providers: r, log: log, now: time.Now}
}

// CollectRequest is one attempt to take money.
type CollectRequest struct {
	TenantID       string
	Property       string
	Unit           string
	PayerID        string
	PayerName      string
	PayerContact   string
	Amount         domain.Minor
	Method         collect.Method
	Reference      string
	IdempotencyKey string
}

// Collect creates the payment, then asks a provider to set up the collection.
//
// A repeated idempotency key returns the payment that already exists rather
// than a second one or an error. That is the contract a retrying client needs:
// the same request twice is the same collection twice, and it says so.
func (s *Payments) Collect(ctx context.Context, req CollectRequest) (collect.Payment, provider.Order, error) {
	adapter, err := s.providers.For(req.Method)
	if err != nil {
		return collect.Payment{}, provider.Order{}, err
	}

	p := collect.Payment{
		TenantID: req.TenantID, Property: req.Property, Unit: req.Unit,
		PayerKind: domain.Tenant, PayerID: req.PayerID,
		Amount: req.Amount, Method: req.Method,
		Provider: adapter.Name(), Status: collect.StatusCreated,
		IdempotencyKey: req.IdempotencyKey,
	}

	created, err := s.store.Create(ctx, p)
	if errors.Is(err, store.ErrDuplicateKey) {
		existing, findErr := s.store.ByIdempotencyKey(ctx, req.IdempotencyKey)
		if findErr != nil {
			return collect.Payment{}, provider.Order{}, findErr
		}
		return existing, provider.Order{ProviderOrderID: existing.ProviderOrderID}, nil
	}
	if err != nil {
		return collect.Payment{}, provider.Order{}, err
	}

	order, err := adapter.CreateOrder(ctx, provider.OrderRequest{
		IdempotencyKey: req.IdempotencyKey,
		Amount:         req.Amount,
		Currency:       domain.Currency,
		Method:         req.Method,
		Reference:      req.Reference,
		PayerName:      req.PayerName,
		PayerContact:   req.PayerContact,
		PayerRef:       req.PayerID,
	})
	if err != nil {
		// The payment stays `created` with no order. That is a recoverable state
		// and it is deliberately not marked failed: the provider being
		// unreachable says nothing about whether this collection can happen, and
		// ErrUnavailable is what lets the layer above offer offline recording to
		// a human rather than deciding for them. ADR-0011 §6.
		s.log.Error("provider did not create an order",
			"payment", created.ID, "provider", adapter.Name(),
			"unavailable", errors.Is(err, provider.ErrUnavailable), "error", err)
		return created, provider.Order{}, err
	}

	if err := s.store.RecordOrder(ctx, created.ID, order.ProviderOrderID); err != nil {
		return created, order, err
	}
	created.ProviderOrderID = order.ProviderOrderID
	return created, order, nil
}

// Confirm asks the provider what is true and applies the answer.
//
// This is the only path that moves a payment's status, and it is reached from
// exactly two places: a webhook that Decide said to confirm, and the
// reconciliation sweep. Resolution is by the payment's own provider — never the
// chain — so a migration cannot make us ask the wrong aggregator about a
// payment it has never heard of.
func (s *Payments) Confirm(ctx context.Context, p collect.Payment) (collect.Payment, error) {
	adapter, err := s.providers.By(p.Provider)
	if err != nil {
		return collect.Payment{}, err
	}
	ref := p.ProviderPaymentID
	if ref == "" {
		ref = p.ProviderOrderID
	}
	conf, err := adapter.Confirm(ctx, ref)
	if err != nil {
		return collect.Payment{}, err
	}

	// A confirmation for a different amount is a reconciliation problem, not a
	// payment. It is refused here rather than posted and argued about later,
	// because once it is captured the ledger has an entry for a number nobody
	// agreed to.
	if conf.AmountMinor != 0 && conf.AmountMinor != p.Amount {
		return collect.Payment{}, fmt.Errorf(
			"money: payment %s is for %s and the provider confirmed %s — reconciliation, not collection",
			p.ID, p.Amount, conf.AmountMinor)
	}

	updated, err := s.store.ApplyConfirmed(ctx, p.ID, conf.Status, s.now())
	if errors.Is(err, collect.ErrStaleTransition) {
		// Normal, and not a fault: the confirmation says what we already knew, or
		// arrived after something later. Nothing is written.
		s.log.Info("confirmation did not move the payment",
			"payment", p.ID, "from", p.Status, "claimed", conf.Status)
		return p, nil
	}
	return updated, err
}

// IngestWebhook is the whole of the delivery path. ADR-0011 §4.
//
// Note the shape of it: every branch either parks, ignores, or confirms, and
// the only writer of a status is Confirm — which asks the provider. There is no
// argument to this function that can cause a status to be written, which is
// what makes "a webhook alone never moves money" a property of the code rather
// than a rule somebody follows.
func (s *Payments) IngestWebhook(ctx context.Context, providerName string, w provider.Webhook, claimed collect.Status, providerRef, eventID string) (collect.Decision, error) {
	adapter, err := s.providers.By(providerName)
	if err != nil {
		return collect.Decision{}, err
	}

	d := collect.Delivery{
		Provider:          providerName,
		EventID:           eventID,
		SignatureVerified: adapter.VerifyWebhook(w),
		ProviderPaymentID: providerRef,
		Claimed:           claimed,
	}

	// The payment is looked up only for a delivery whose signature held. An
	// unverified delivery's contents are an attacker's, so the id it names is not
	// used to probe for which payments exist.
	var current *collect.Payment
	if d.SignatureVerified && providerRef != "" {
		found, err := s.store.ByProviderOrderID(ctx, providerName, providerRef)
		switch {
		case err == nil:
			current = &found
		case errors.Is(err, store.ErrNotFound):
			// Left nil: Decide parks it rather than dropping it.
		default:
			return collect.Decision{}, err
		}
	}

	decision := collect.Decide(d, current)
	switch decision.Disposition {
	case collect.Confirm:
		if _, err := s.Confirm(ctx, *current); err != nil {
			return decision, err
		}
	case collect.Park:
		s.log.Warn("webhook parked", "provider", providerName, "event", eventID,
			"reason", decision.Reason, "verified", d.SignatureVerified)
	case collect.Ignore:
		s.log.Debug("webhook says what is already recorded",
			"provider", providerName, "event", eventID)
	}
	return decision, nil
}
