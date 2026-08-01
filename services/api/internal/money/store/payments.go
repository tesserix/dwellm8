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
			INSERT INTO payments (tenant_id, property_id, unit_id, mandate_id, lease_id,
			                      payer_kind, payer_id, amount_minor, method,
			                      provider, provider_order_id, status, idempotency_key,
			                      fee_bearer_party_id)
			VALUES ($1, $2, nullif($3,'')::uuid, nullif($4,'')::uuid, nullif($5,'')::uuid,
			        $6, $7, $8, $9, $10, nullif($11,''), $12, $13, nullif($14,'')::uuid)
			RETURNING id, created_at`,
			p.TenantID, p.Property, p.Unit, p.MandateID, p.Lease,
			string(p.PayerKind), p.PayerID, int64(p.Amount), string(p.Method),
			p.Provider, p.ProviderOrderID, string(p.Status), p.IdempotencyKey, p.Bearer,
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
	coalesce(p.mandate_id::text,''), coalesce(p.lease_id::text,''),
	p.payer_kind, p.payer_id, p.amount_minor,
	p.method, p.provider, coalesce(p.provider_order_id,''),
	coalesce(p.provider_payment_id,''), p.status, coalesce(p.failure_code,''),
	p.idempotency_key, coalesce(p.entry_id::text,''), p.created_at,
	coalesce(p.fee_bearer_party_id::text,'')`

func (s *Payments) one(ctx context.Context, where string, args ...any) (collect.Payment, error) {
	var p collect.Payment
	var kind, method, status string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT `+paymentColumns+` FROM payments p `+where, args...,
		).Scan(&p.ID, &p.TenantID, &p.Property, &p.Unit, &p.MandateID, &p.Lease,
			&kind, &p.PayerID, &p.Amount, &method, &p.Provider,
			&p.ProviderOrderID, &p.ProviderPaymentID, &status, &p.FailureCode,
			&p.IdempotencyKey, &p.EntryID, &p.CreatedAt, &p.Bearer)
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

// PostFor builds the entry a payment must post as it reaches its new status.
//
// outstanding is the payer's receivable at that moment, read inside the same
// transaction: a payment is split into principal and advance against what is
// actually owed, and reading it outside would price the split against a balance
// that has since moved.
type PostFor func(p collect.Payment, outstanding domain.Minor) (domain.Entry, error)

// ApplyConfirmedAndPost writes a confirmed status and, when that status is one
// that moves money, the ledger entry for it — in one transaction.
//
// One transaction is the whole point. Posting first and updating after would
// leave an entry behind whenever the transition turns out to be stale, and an
// entry nothing points at is money counted twice. Updating first is worse: the
// schema refuses a captured payment with no entry, so it would simply fail.
//
// build is called only for a status that posts. A stale transition returns
// collect.ErrStaleTransition, having written neither.
func (s *Payments) ApplyConfirmedAndPost(ctx context.Context, id string, to collect.Status, at time.Time, build PostFor) (collect.Payment, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return collect.Payment{}, tenancy.ErrNoTenant
	}

	var out collect.Payment
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		p, err := lockPayment(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := p.ApplyConfirmed(to, at); err != nil {
			return err
		}

		if posts(to) && p.EntryID == "" {
			outstanding, err := receivable(ctx, tx, p)
			if err != nil {
				return fmt.Errorf("reading what is owed: %w", err)
			}
			e, err := build(p, outstanding)
			if err != nil {
				return err
			}
			entryID, err := postEntry(ctx, tx, tenant, e)
			if err != nil {
				return err
			}
			p.EntryID = entryID
		}

		out = p
		_, err = tx.Exec(ctx, `
			UPDATE payments
			   SET status = $2,
			       entry_id = coalesce(entry_id, nullif($3,'')::uuid),
			       provider_payment_id = coalesce(nullif($4,''), provider_payment_id),
			       failure_code = nullif($5,''),
			       authorised_at = coalesce(authorised_at, nullif($6, timestamptz 'epoch')),
			       captured_at   = coalesce(captured_at,   nullif($7, timestamptz 'epoch')),
			       settled_at    = coalesce(settled_at,    nullif($8, timestamptz 'epoch'))
			 WHERE id = $1`,
			id, string(p.Status), p.EntryID, p.ProviderPaymentID, p.FailureCode,
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

// posts reports whether reaching this status means money moved and the ledger
// must say so. The schema's payments_captured_has_entry is the same list.
func posts(s collect.Status) bool {
	return s == collect.StatusCaptured || s == collect.StatusSettled
}

func lockPayment(ctx context.Context, tx pgx.Tx, id string) (collect.Payment, error) {
	var p collect.Payment
	var kind, method, status string
	if err := tx.QueryRow(ctx,
		`SELECT `+paymentColumns+` FROM payments p WHERE p.id = $1 FOR UPDATE`, id,
	).Scan(&p.ID, &p.TenantID, &p.Property, &p.Unit, &p.MandateID, &p.Lease,
		&kind, &p.PayerID, &p.Amount, &method, &p.Provider,
		&p.ProviderOrderID, &p.ProviderPaymentID, &status, &p.FailureCode,
		&p.IdempotencyKey, &p.EntryID, &p.CreatedAt, &p.Bearer); err != nil {
		return collect.Payment{}, err
	}
	p.PayerKind, p.Method, p.Status = domain.PartyKind(kind), collect.Method(method), collect.Status(status)
	return p, nil
}

// receivable is what the payer owes right now, from the postings.
//
// Scoped to the lease when the collection names one and to the payer otherwise,
// which is the difference between "what is owed on this tenancy" and "what this
// person owes anywhere in the organisation".
func receivable(ctx context.Context, tx pgx.Tx, p collect.Payment) (domain.Minor, error) {
	var owed domain.Minor
	err := tx.QueryRow(ctx, `
		SELECT coalesce(sum(l.signed_minor), 0)
		  FROM ledger_postings l
		  JOIN journal_entries e ON e.id = l.entry_id AND e.tenant_id = l.tenant_id
		 WHERE l.account_code = $1
		   AND l.party_kind = $2
		   AND l.party_id = $3::uuid
		   AND ($4 = '' OR e.lease_id = $4::uuid)`,
		domain.TenantReceivable, string(p.PayerKind), p.PayerID, p.Lease).Scan(&owed)
	if owed < 0 {
		// A credit balance is an advance already held, not a debt. The templates
		// treat "owed nothing" and "owed less than nothing" the same way.
		owed = 0
	}
	return owed, err
}

// ApplyConfirmed writes a status that has been confirmed against the provider.
//
// It re-reads the payment inside the transaction and applies the transition
// through the domain, so the rule is the same one the handler used and the
// schema's trigger is the backstop rather than the only check. A stale
// transition returns collect.ErrStaleTransition and writes nothing.
//
// It cannot write a status that posts — the schema refuses a captured payment
// with no entry — so those go through ApplyConfirmedAndPost.
func (s *Payments) ApplyConfirmed(ctx context.Context, id string, to collect.Status, at time.Time) (collect.Payment, error) {
	var out collect.Payment
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var p collect.Payment
		var kind, method, status string
		if err := tx.QueryRow(ctx,
			`SELECT `+paymentColumns+` FROM payments p WHERE p.id = $1 FOR UPDATE`, id,
		).Scan(&p.ID, &p.TenantID, &p.Property, &p.Unit, &p.MandateID, &p.Lease,
			&kind, &p.PayerID, &p.Amount, &method, &p.Provider,
			&p.ProviderOrderID, &p.ProviderPaymentID, &status, &p.FailureCode,
			&p.IdempotencyKey, &p.EntryID, &p.CreatedAt, &p.Bearer); err != nil {
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

// Pending returns payments that have been waiting longer than a cut-off, oldest
// first, for the polling sweep to ask the provider about.
//
// Ordered oldest first so a limit truncates the newest rather than starving the
// ones that have waited longest — a sweep that always asked about the same
// hundred recent payments would leave an old stuck one stuck forever.
//
// Terminal statuses are excluded in SQL rather than in Go: filtering after the
// fact would read every settled payment this organisation has ever taken in
// order to skip it.
func (s *Payments) Pending(ctx context.Context, olderThan time.Time, limit int) ([]collect.Payment, error) {
	var out []collect.Payment
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT `+paymentColumns+`
			  FROM payments p
			 WHERE p.status = ANY($3)
			   AND p.created_at < $1
			 ORDER BY p.created_at
			 LIMIT $2`, olderThan, limit, awaitingPayer())
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var p collect.Payment
			var kind, method, status string
			if err := rows.Scan(&p.ID, &p.TenantID, &p.Property, &p.Unit, &p.MandateID, &p.Lease,
				&kind, &p.PayerID, &p.Amount, &method, &p.Provider,
				&p.ProviderOrderID, &p.ProviderPaymentID, &status, &p.FailureCode,
				&p.IdempotencyKey, &p.EntryID, &p.CreatedAt, &p.Bearer); err != nil {
				return err
			}
			p.PayerKind = domain.PartyKind(kind)
			p.Method = collect.Method(method)
			p.Status = collect.Status(status)
			out = append(out, p)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("money: reading pending payments: %w", err)
	}
	return out, nil
}

// awaitingPayer is collect.AwaitingPayerStatuses as the driver wants it. Derived
// from the domain rather than written out here, so the query cannot drift from
// the Go predicate the sweep uses.
func awaitingPayer() []string {
	statuses := collect.AwaitingPayerStatuses()
	out := make([]string, len(statuses))
	for i, s := range statuses {
		out[i] = string(s)
	}
	return out
}
