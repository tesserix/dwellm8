package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/mandate"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Mandates is the standing-authority repository.
type Mandates struct{ pool tenancy.Pool }

func NewMandates(p tenancy.Pool) *Mandates { return &Mandates{pool: p} }

// ErrAlreadyLive is a second active authority on a unit that already has one.
//
// It is surfaced as its own error rather than a generic constraint failure
// because the caller's correct response is specific: show the tenant the
// mandate they already have, and never register another. Two live authorities
// debit them twice on the first of the month.
var ErrAlreadyLive = errors.New("money: this unit already has a live mandate")

// ErrNoMandate is a lookup that matched nothing, including a row the policy
// hides.
var ErrNoMandate = errors.New("money: no such mandate")

const mandateColumns = `
	m.id, m.tenant_id, m.property_id, m.unit_id, coalesce(m.lease_id::text,''),
	m.payer_kind, m.payer_id, m.rail, m.max_amount_minor, m.provider,
	coalesce(m.provider_mandate_id,''), m.status, coalesce(m.failure_code,''),
	m.created_at`

func (s *Mandates) Register(ctx context.Context, m mandate.Mandate, idempotencyKey, ruleSource string) (mandate.Mandate, error) {
	if err := m.Validate(); err != nil {
		return mandate.Mandate{}, err
	}
	if idempotencyKey == "" {
		return mandate.Mandate{}, errors.New("money: a mandate without an idempotency key could be registered twice")
	}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO mandates (tenant_id, property_id, unit_id, lease_id,
			                      payer_kind, payer_id, rail, max_amount_minor,
			                      provider, provider_mandate_id, status,
			                      rail_rule_source, idempotency_key,
			                      first_debit_on, ends_on)
			VALUES ($1, $2, $3, nullif($4,'')::uuid, $5, $6, $7, $8, $9,
			        nullif($10,''), $11, nullif($12,''),  $13,
			        nullif($14, date 'epoch'), nullif($15, date 'epoch'))
			RETURNING id, created_at`,
			m.TenantID, m.Property, m.Unit, m.LeaseID,
			string(m.PayerKind), m.PayerID, string(m.Rail), int64(m.MaxAmount),
			m.Provider, m.ProviderMandateID, string(m.Status), ruleSource, idempotencyKey,
			m.FirstDebitOn, m.EndsOn,
		).Scan(&m.ID, &m.CreatedAt)
	})
	switch {
	case isUniqueViolation(err, "mandates_one_active_per_unit_idx"):
		return mandate.Mandate{}, fmt.Errorf("%w: unit %s", ErrAlreadyLive, m.Unit)
	case isUniqueViolation(err, "mandates_idempotency_idx"):
		return mandate.Mandate{}, fmt.Errorf("%w: %s", ErrDuplicateKey, idempotencyKey)
	case err != nil:
		return mandate.Mandate{}, fmt.Errorf("money: registering a mandate: %w", err)
	}
	return m, nil
}

func (s *Mandates) ByProviderMandateID(ctx context.Context, provider, id string) (mandate.Mandate, error) {
	return s.one(ctx, `WHERE m.provider = $1 AND m.provider_mandate_id = $2`, provider, id)
}

// LiveForUnit is what the collection path asks before falling back to a collect
// request: is there an authority here that can simply be debited.
func (s *Mandates) LiveForUnit(ctx context.Context, unit string) (mandate.Mandate, error) {
	return s.one(ctx, `WHERE m.unit_id = $1 AND m.status = 'active'`, unit)
}

func (s *Mandates) one(ctx context.Context, where string, args ...any) (mandate.Mandate, error) {
	var m mandate.Mandate
	var kind, rail, status string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT `+mandateColumns+` FROM mandates m `+where, args...,
		).Scan(&m.ID, &m.TenantID, &m.Property, &m.Unit, &m.LeaseID,
			&kind, &m.PayerID, &rail, &m.MaxAmount, &m.Provider,
			&m.ProviderMandateID, &status, &m.FailureCode, &m.CreatedAt)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return mandate.Mandate{}, ErrNoMandate
	}
	if err != nil {
		return mandate.Mandate{}, fmt.Errorf("money: reading a mandate: %w", err)
	}
	m.PayerKind, m.Rail, m.Status = domain.PartyKind(kind), mandate.Rail(rail), mandate.Status(status)
	return m, nil
}

// RecordRegistration stores the provider's id once the authority exists at
// their end. As with payments, the row is written first: a crash between the
// two leaves a `created` mandate with no provider id, which a sweep can finish,
// rather than an authority at Cashfree that nothing here knows about.
func (s *Mandates) RecordRegistration(ctx context.Context, id, providerMandateID string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE mandates SET provider_mandate_id = $2 WHERE id = $1`, id, providerMandateID)
		return err
	})
}

// ApplyConfirmed moves a mandate to a status confirmed against the provider,
// through the domain so the rule is the same one everywhere.
func (s *Mandates) ApplyConfirmed(ctx context.Context, id string, to mandate.Status, at time.Time) (mandate.Mandate, error) {
	var out mandate.Mandate
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var m mandate.Mandate
		var kind, rail, status string
		if err := tx.QueryRow(ctx,
			`SELECT `+mandateColumns+` FROM mandates m WHERE m.id = $1 FOR UPDATE`, id,
		).Scan(&m.ID, &m.TenantID, &m.Property, &m.Unit, &m.LeaseID,
			&kind, &m.PayerID, &rail, &m.MaxAmount, &m.Provider,
			&m.ProviderMandateID, &status, &m.FailureCode, &m.CreatedAt); err != nil {
			return err
		}
		m.PayerKind, m.Rail, m.Status = domain.PartyKind(kind), mandate.Rail(rail), mandate.Status(status)

		if err := m.ApplyConfirmed(to, at); err != nil {
			return err
		}
		out = m
		_, err := tx.Exec(ctx, `
			UPDATE mandates
			   SET status = $2,
			       activated_at = coalesce(activated_at, nullif($3, timestamptz 'epoch')),
			       ended_at     = coalesce(ended_at,     nullif($4, timestamptz 'epoch'))
			 WHERE id = $1`,
			id, string(m.Status), m.ActivatedAt, m.EndedAt)
		return err
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return mandate.Mandate{}, ErrNoMandate
	case isUniqueViolation(err, "mandates_one_active_per_unit_idx"):
		// Activating this one would make two. The index catches it even here,
		// which is the point of it being in the database rather than in a check
		// the activation path performs.
		return mandate.Mandate{}, fmt.Errorf("%w: unit %s", ErrAlreadyLive, out.Unit)
	case err != nil:
		return mandate.Mandate{}, err
	}
	return out, nil
}
