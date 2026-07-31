package events

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Append writes one event to the outbox on the caller's transaction. ADR-0002 §3.
//
// The transaction is the caller's on purpose: the state change and the event are
// one commit, so an event that exists describes a fact that happened. Nothing in
// here talks to a broker, and a broker being down cannot fail a user's request.
func Append(ctx context.Context, tx pgx.Tx, e Envelope) error {
	if e.ID == "" {
		e.ID = NewULID(e.OccurredAt)
	}
	if e.CorrelationID == "" {
		e.CorrelationID = e.ID
	}
	if e.Version == 0 {
		e.Version = 1
	}
	if err := e.Validate(); err != nil {
		return err
	}

	_, err := tx.Exec(ctx, `
		INSERT INTO outbox (
			id, tenant_id, type, version, subject_kind, subject_id,
			correlation_id, causation_id, actor_kind, actor_id,
			occurred_at, payload
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (id) DO NOTHING`,
		e.ID, e.TenantID, e.Type, e.Version, e.Subject.Kind, e.Subject.ID,
		e.CorrelationID, nullString(e.CausationID), string(e.Actor.Kind), nullString(e.Actor.ID),
		e.OccurredAt, []byte(e.Data))
	if err != nil {
		return fmt.Errorf("events: appending %s: %w", e.Type, err)
	}
	return nil
}

// AppendAll writes several events on one transaction, preserving their order.
func AppendAll(ctx context.Context, tx pgx.Tx, es ...Envelope) error {
	for _, e := range es {
		if err := Append(ctx, tx, e); err != nil {
			return err
		}
	}
	return nil
}

// New builds an envelope with the fields a caller cannot forget, marshalling the
// payload. It starts a fresh causal chain; use Caused to join an existing one.
func New(typ, tenantID string, subject Subject, actor Actor, data any) (Envelope, error) {
	raw, err := json.Marshal(data)
	if err != nil {
		return Envelope{}, fmt.Errorf("events: marshalling %s: %w", typ, err)
	}
	now := time.Now().UTC()
	id := NewULID(now)
	return Envelope{
		ID:      id,
		Type:    typ,
		Version: 1, TenantID: tenantID,
		OccurredAt: now,
		Actor:      actor,
		Subject:    subject,
		// A root event is its own chain until Caused says otherwise.
		CorrelationID: id,
		Data:          raw,
	}, nil
}

// Caused returns a copy of e carrying the causal chain of parent: the same
// correlation id, and parent's id as the immediate cause.
func (e Envelope) Caused(parent Envelope) Envelope {
	e.CorrelationID = parent.CorrelationID
	if e.CorrelationID == "" {
		e.CorrelationID = parent.ID
	}
	e.CausationID = parent.ID
	return e
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
