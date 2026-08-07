package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// RecordOwnership names who the rent on this property is credited to, whole,
// from a date (#302). Without it a tenancy is unbillable: the billing run
// refuses a lease it cannot attribute rather than crediting somebody's income
// to a party it guessed.
//
// Idempotent by the exclusion constraint the schema already carries — an
// onboarding retried after a later step failed must not leave two live rows,
// and must not fail on the second attempt either.
func (s *Properties) RecordOwnership(ctx context.Context, propertyID, ownerPartyID string, from effective.Date) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}

	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, owner_party_id, share_bps, valid_from)
			SELECT $1, $2, $3::uuid, 10000, $4::date
			 WHERE NOT EXISTS (
			       SELECT 1 FROM property_ownership
			        WHERE tenant_id = $1 AND property_id = $2 AND unit_id IS NULL
			          AND owner_party_id = $3::uuid AND retired_at IS NULL)
			RETURNING id::text`,
			tenant.String(), propertyID, ownerPartyID, from.Time()).Scan(&id)
		if err == pgx.ErrNoRows {
			return nil // already recorded, by this call or an earlier attempt
		}
		if err != nil {
			return err
		}
		env, err := events.New("property.ownership.recorded", tenant.String(),
			events.Subject{Kind: "property", ID: propertyID},
			events.Actor{Kind: events.ActorSystem},
			struct {
				OwnerPartyID string `json:"owner_party_id"`
				ShareBps     int    `json:"share_bps"`
				From         string `json:"from"`
			}{ownerPartyID, 10000, from.String()})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
	if isForeignKey(err) {
		return ErrNoProperty
	}
	if err != nil {
		return fmt.Errorf("property: recording ownership of %s: %w", propertyID, err)
	}
	return nil
}
