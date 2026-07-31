// Package store is the money module's persistence. Every query runs inside a
// tenancy-scoped transaction, so row-level security is what enforces isolation
// rather than a WHERE clause somebody has to remember.
//
// The one exception is the webhook inbox, which is a platform-role path for the
// reason ADR-0011 §5 gives: the handler runs before it knows whose money the
// delivery is about, so it cannot run inside a tenant session.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Payments is the payments repository.
type Payments struct{ pool tenancy.Pool }

// NewPayments takes the request pool — the one connected as dwellm8_api. Never
// the platform pool: a collection is always somebody's.
func NewPayments(p tenancy.Pool) *Payments { return &Payments{pool: p} }

// ErrDuplicateKey is a second collection request carrying a key that already
// created one. It is not a failure: it is the idempotency guarantee working,
// and the caller returns the payment that already exists.
var ErrDuplicateKey = errors.New("money: this idempotency key already created a payment")

// ErrNotFound is what a lookup returns when nothing matches — including when a
// row exists and the policy hides it, which is deliberate. "You may not see it"
// and "it does not exist" are the same answer to somebody probing.
var ErrNotFound = errors.New("money: no such payment")

// Create inserts a collection attempt.
//
// The unique index on (tenant_id, idempotency_key) is the guarantee, so this
// does not check first. Two concurrent retries both insert; one wins, the other
// gets ErrDuplicateKey, and neither had to be careful. Checking first is the
// version that is correct in review and wrong under load.
func (s *Payments) Create(ctx context.Context, p collect.Payment) (collect.Payment, error) {
	if err := p.Validate(); err != nil {
		return collect.Payment{}, err
	}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO payments (tenant_id, property_id, unit_id, mandate_id,
			                      payer_kind, payer_id, amount_minor, method,
			                      provider, provider_order_id, status, idempotency_key)
			VALUES ($1, $2, nullif($3,'')::uuid, nullif($4,'')::uuid,
			        $5, $6, $7, $8, $9, nullif($10,''), $11, $12)
			RETURNING id, created_at`,
			p.TenantID, p.Property, p.Unit, p.MandateID,
			string(p.PayerKind), p.PayerID, int64(p.Amount), string(p.Method),
			p.Provider, p.ProviderOrderID, string(p.Status), p.IdempotencyKey,
		).Scan(&p.ID, &p.CreatedAt)
	})
	if err != nil {
		if isUniqueViolation(err, "payments_idempotency_idx") {
			return collect.Payment{}, fmt.Errorf("%w: %s", ErrDuplicateKey, p.IdempotencyKey)
		}
		return collect.Payment{}, fmt.Errorf("money: creating a payment: %w", err)
	}
	return p, nil
}

// ByIdempotencyKey is what a duplicate request returns instead of a second
// payment. Same key, same answer, forever — the key never expires, so a retry a
// month later still resolves to the collection it belongs to.
func (s *Payments) ByIdempotencyKey(ctx context.Context, key string) (collect.Payment, error) {
	return s.one(ctx, `WHERE p.idempotency_key = $1`, key)
}

// ByProviderPaymentID resolves the payment a webhook or a confirmation names.
func (s *Payments) ByProviderPaymentID(ctx context.Context, provider, id string) (collect.Payment, error) {
	return s.one(ctx, `WHERE p.provider = $1 AND p.provider_payment_id = $2`, provider, id)
}

// ByProviderOrderID resolves by the order id, which is what Cashfree's webhooks
// carry rather than a payment id.
func (s *Payments) ByProviderOrderID(ctx context.Context, provider, id string) (collect.Payment, error) {
	return s.one(ctx, `WHERE p.provider = $1 AND p.provider_order_id = $2`, provider, id)
}

const paymentColumns = `
	p.id, p.tenant_id, p.property_id, coalesce(p.unit_id::text,''),
	coalesce(p.mandate_id::text,''), p.payer_kind, p.payer_id, p.amount_minor,
	p.method, p.provider, coalesce(p.provider_order_id,''),
	coalesce(p.provider_payment_id,''), p.status, coalesce(p.failure_code,''),
	p.idempotency_key, coalesce(p.entry_id::text,''), p.created_at`

func (s *Payments) one(ctx context.Context, where string, args ...any) (collect.Payment, error) {
	var p collect.Payment
	var kind, method, status string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT `+paymentColumns+` FROM payments p `+where, args...,
		).Scan(&p.ID, &p.TenantID, &p.Property, &p.Unit, &p.MandateID,
			&kind, &p.PayerID, &p.Amount, &method, &p.Provider,
			&p.ProviderOrderID, &p.ProviderPaymentID, &status, &p.FailureCode,
			&p.IdempotencyKey, &p.EntryID, &p.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return collect.Payment{}, ErrNotFound
	}
	if err != nil {
		return collect.Payment{}, fmt.Errorf("money: reading a payment: %w", err)
	}
	p.PayerKind = domain.PartyKind(kind)
	p.Method = collect.Method(method)
	p.Status = collect.Status(status)
	return p, nil
}

// RecordOrder stores what the provider gave back for a payment we had already
// created. The payment exists first and the provider is called second, so a
// crash between the two leaves a `created` payment with no order — which is
// recoverable — rather than an order nothing in this system knows about.
func (s *Payments) RecordOrder(ctx context.Context, id, providerOrderID string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE payments SET provider_order_id = $2 WHERE id = $1`, id, providerOrderID)
		return err
	})
}

// ApplyConfirmed writes a status that has been confirmed against the provider.
//
// It re-reads the payment inside the transaction and applies the transition
// through the domain, so the rule is the same one the handler used and the
// schema's trigger is the backstop rather than the only check. A stale
// transition returns collect.ErrStaleTransition and writes nothing.
func (s *Payments) ApplyConfirmed(ctx context.Context, id string, to collect.Status, at time.Time) (collect.Payment, error) {
	var out collect.Payment
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var p collect.Payment
		var kind, method, status string
		if err := tx.QueryRow(ctx,
			`SELECT `+paymentColumns+` FROM payments p WHERE p.id = $1 FOR UPDATE`, id,
		).Scan(&p.ID, &p.TenantID, &p.Property, &p.Unit, &p.MandateID,
			&kind, &p.PayerID, &p.Amount, &method, &p.Provider,
			&p.ProviderOrderID, &p.ProviderPaymentID, &status, &p.FailureCode,
			&p.IdempotencyKey, &p.EntryID, &p.CreatedAt); err != nil {
			return err
		}
		p.PayerKind, p.Method, p.Status = domain.PartyKind(kind), collect.Method(method), collect.Status(status)

		if err := p.ApplyConfirmed(to, at); err != nil {
			return err
		}
		out = p
		_, err := tx.Exec(ctx, `
			UPDATE payments
			   SET status = $2,
			       provider_payment_id = coalesce(nullif($3,''), provider_payment_id),
			       failure_code = nullif($4,''),
			       authorised_at = coalesce(authorised_at, nullif($5, timestamptz 'epoch')),
			       captured_at   = coalesce(captured_at,   nullif($6, timestamptz 'epoch')),
			       settled_at    = coalesce(settled_at,    nullif($7, timestamptz 'epoch'))
			 WHERE id = $1`,
			id, string(p.Status), p.ProviderPaymentID, p.FailureCode,
			p.AuthorisedAt, p.CapturedAt, p.SettledAt)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return collect.Payment{}, ErrNotFound
	}
	if err != nil {
		return collect.Payment{}, err
	}
	return out, nil
}

func isUniqueViolation(err error, index string) bool {
	// pgx surfaces the constraint name in the error text; matching on the index
	// name rather than the SQLSTATE alone keeps "this key was reused" distinct
	// from "some other unique constraint was violated", which would otherwise be
	// reported to a caller as a successful duplicate.
	return err != nil && strings.Contains(err.Error(), index)
}
