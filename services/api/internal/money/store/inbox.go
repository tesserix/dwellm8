package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/money/domain/collect"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The webhook inbox. ADR-0011 §4 and §5.
//
// This is the one repository that takes the platform pool rather than the
// request pool, and the reason is structural rather than convenient: the
// handler runs before it knows whose money a delivery is about, so it cannot
// run inside a tenant-scoped session. `payment_events`'s WITH CHECK is
// `is_platform_session()` alone — no organisation can write to the inbox at
// all, which is asserted from both sides in the isolation harness.
type Inbox struct{ pool tenancy.PlatformPool }

// NewInbox takes the platform pool. Constructing one is deliberate and visible
// in wiring, which is the point of PlatformPool being its own type.
func NewInbox(p tenancy.PlatformPool) *Inbox { return &Inbox{pool: p} }

// ErrAlreadySeen is the fifth delivery of an event. It is not a failure: it is
// the deduplication index doing the work no handler has to remember to do.
var ErrAlreadySeen = errors.New("money: this delivery has already been recorded")

// Delivery is one webhook as it is stored.
type Delivery struct {
	Provider string
	EventID  string
	Type     string
	// Verified records whether the signature held. Stored either way — an
	// unverified delivery is evidence of something, possibly of an attack.
	Verified bool
	Payload  []byte

	// TenantID and PaymentID are empty when the delivery could not be
	// attributed. That is "parked, not dropped" made structural: the column is
	// nullable, and a parked row is visible only to a platform session.
	TenantID  string
	PaymentID string
	MandateID string

	Reason collect.ParkReason
	// Processed marks a delivery that was acted on. A row may be parked or
	// processed and never both — a handler that set both did two things.
	Processed bool
}

// Record stores a delivery. Every delivery, before anything is decided about
// it, so that a crash between arrival and decision loses nothing.
//
// A duplicate event id conflicts on (provider, provider_event_id) and returns
// ErrAlreadySeen rather than an error the caller has to interpret. The fifth
// delivery of an event is discarded by the index, not by a counter.
func (i *Inbox) Record(ctx context.Context, d Delivery) (string, error) {
	if d.EventID == "" {
		return "", errors.New("money: a delivery with no event id cannot be deduplicated")
	}
	// An unverified delivery may never be attributed to anything. The schema's
	// payment_events_unverified_is_parked enforces it; refusing here as well
	// means the error names the mistake rather than the constraint.
	if !d.Verified && (d.PaymentID != "" || d.MandateID != "" || d.TenantID != "") {
		return "", errors.New("money: an unverified delivery was attributed to an organisation")
	}

	var id string
	err := tenancy.Platform(ctx, i.pool, "recording a webhook delivery", func(ctx context.Context, tx pgx.Tx) error {
		payload := d.Payload
		if len(payload) == 0 {
			payload = []byte(`{}`)
		}
		return tx.QueryRow(ctx, `
			INSERT INTO payment_events (tenant_id, provider, provider_event_id, event_type,
			                            signature_verified, payload, payment_id, mandate_id,
			                            park_reason, processed_at)
			VALUES (nullif($1,'')::uuid, $2, $3, $4, $5, $6::jsonb,
			        nullif($7,'')::uuid, nullif($8,'')::uuid, nullif($9,''),
			        CASE WHEN $10 THEN now() END)
			RETURNING id`,
			d.TenantID, d.Provider, d.EventID, d.Type, d.Verified, string(payload),
			d.PaymentID, d.MandateID, string(d.Reason), d.Processed,
		).Scan(&id)
	})
	if isUniqueViolation(err, "payment_events_provider_event_idx") {
		return "", fmt.Errorf("%w: %s/%s", ErrAlreadySeen, d.Provider, d.EventID)
	}
	if err != nil {
		return "", fmt.Errorf("money: recording a delivery: %w", err)
	}
	return id, nil
}

// MarkProcessed records that a delivery was acted on, once the confirmation it
// triggered has been applied.
func (i *Inbox) MarkProcessed(ctx context.Context, id, tenantID, paymentID string) error {
	return tenancy.Platform(ctx, i.pool, "marking a delivery processed", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			UPDATE payment_events
			   SET processed_at = now(),
			       tenant_id  = coalesce(tenant_id,  nullif($2,'')::uuid),
			       payment_id = coalesce(payment_id, nullif($3,'')::uuid)
			 WHERE id = $1 AND signature_verified`,
			id, tenantID, paymentID)
		return err
	})
}
