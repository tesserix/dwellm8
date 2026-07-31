package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Transport publishes one event and returns once the broker has durably
// acknowledged it. The id must be presented to the broker for deduplication —
// for JetStream that is Nats-Msg-Id — because publishing is at-least-once and
// the duplicate window is what keeps a retry from becoming a second event.
type Transport interface {
	Publish(ctx context.Context, subject, id string, body []byte) error
}

// RelayConfig tunes the drain loop. Zero values are replaced by the ADR's.
type RelayConfig struct {
	BatchSize   int
	Interval    time.Duration
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// MaxAttempts is where automatic retry stops. ADR-0002 §4 makes that a P2
	// alert rather than a discard: the row stays, unpublished and visible.
	MaxAttempts int
	// StuckAfter re-claims rows a dead pod left in flight.
	StuckAfter time.Duration
}

func (c RelayConfig) withDefaults() RelayConfig {
	if c.BatchSize <= 0 {
		c.BatchSize = 100
	}
	if c.Interval <= 0 {
		c.Interval = time.Second
	}
	if c.BaseBackoff <= 0 {
		c.BaseBackoff = time.Second
	}
	if c.MaxBackoff <= 0 {
		c.MaxBackoff = 5 * time.Minute
	}
	if c.MaxAttempts <= 0 {
		c.MaxAttempts = 10
	}
	if c.StuckAfter <= 0 {
		c.StuckAfter = 2 * time.Minute
	}
	return c
}

// Relay drains the outbox into the broker. ADR-0002 §4.
//
// It holds the platform pool because draining is inherently cross-tenant: the
// outbox policy exempts the platform role rather than any unscoped session, so
// a request pool would simply see no rows.
type Relay struct {
	pool      tenancy.PlatformPool
	transport Transport
	log       *slog.Logger
	cfg       RelayConfig
}

func NewRelay(pool tenancy.PlatformPool, transport Transport, log *slog.Logger, cfg RelayConfig) *Relay {
	return &Relay{pool: pool, transport: transport, log: log, cfg: cfg.withDefaults()}
}

// Run drains until the context is cancelled. It is meant to be one goroutine in
// the API process; several replicas running it is safe, because claiming uses
// FOR UPDATE SKIP LOCKED.
func (r *Relay) Run(ctx context.Context) error {
	t := time.NewTicker(r.cfg.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			n, err := r.Drain(ctx)
			if err != nil && !errors.Is(err, context.Canceled) {
				r.log.ErrorContext(ctx, "outbox drain failed", "error", err)
				continue
			}
			// A full batch means there is more waiting; go again rather than
			// idling a whole tick with a backlog building.
			for err == nil && n == r.cfg.BatchSize {
				n, err = r.Drain(ctx)
			}
		}
	}
}

type pending struct {
	env      Envelope
	attempts int
}

// Drain publishes one batch and returns how many rows it claimed.
func (r *Relay) Drain(ctx context.Context) (int, error) {
	rows, err := r.claim(ctx)
	if err != nil {
		return 0, err
	}

	for _, row := range rows {
		body, err := json.Marshal(row.env)
		if err != nil {
			// An envelope that cannot be marshalled will never publish; record
			// the reason rather than retrying it a thousand times.
			r.fail(ctx, row, fmt.Errorf("marshalling: %w", err))
			continue
		}
		if err := r.transport.Publish(ctx, row.env.SubjectName(), row.env.ID, body); err != nil {
			r.fail(ctx, row, err)
			continue
		}
		if err := r.markPublished(ctx, row.env.ID); err != nil {
			// Published but not marked: the sweeper re-claims it and the
			// broker's duplicate window absorbs the second publish.
			r.log.ErrorContext(ctx, "outbox row published but not marked",
				"event_id", row.env.ID, "error", err)
		}
	}
	return len(rows), nil
}

// claim takes a batch and pushes its next attempt out by StuckAfter, so a pod
// that dies mid-publish releases the rows rather than holding them forever.
func (r *Relay) claim(ctx context.Context) ([]pending, error) {
	var out []pending
	err := tenancy.Platform(ctx, r.pool, "outbox relay: claiming a batch to publish",
		func(ctx context.Context, tx pgx.Tx) error {
			var err error
			out, err = claimBatch(ctx, tx, r.cfg)
			return err
		})
	return out, err
}

func claimBatch(ctx context.Context, tx pgx.Tx, cfg RelayConfig) ([]pending, error) {
	rows, err := tx.Query(ctx, `
		WITH claimed AS (
			SELECT id FROM outbox
			WHERE published_at IS NULL
			  AND next_attempt_at <= now()
			  AND attempts < $2
			ORDER BY occurred_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE outbox o
		SET attempts = o.attempts + 1,
		    next_attempt_at = now() + make_interval(secs => $3)
		FROM claimed c
		WHERE o.id = c.id
		RETURNING o.id, o.tenant_id, o.type, o.version, o.subject_kind, o.subject_id,
		          o.correlation_id, coalesce(o.causation_id, ''), o.actor_kind,
		          coalesce(o.actor_id::text, ''), o.occurred_at, o.payload, o.attempts`,
		cfg.BatchSize, cfg.MaxAttempts, cfg.StuckAfter.Seconds())
	if err != nil {
		return nil, fmt.Errorf("events: claiming outbox rows: %w", err)
	}

	var out []pending
	for rows.Next() {
		var p pending
		var payload []byte
		if err := rows.Scan(
			&p.env.ID, &p.env.TenantID, &p.env.Type, &p.env.Version,
			&p.env.Subject.Kind, &p.env.Subject.ID, &p.env.CorrelationID,
			&p.env.CausationID, &p.env.Actor.Kind, &p.env.Actor.ID,
			&p.env.OccurredAt, &payload, &p.attempts,
		); err != nil {
			rows.Close()
			return nil, fmt.Errorf("events: scanning outbox row: %w", err)
		}
		p.env.Data = json.RawMessage(payload)
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("events: reading outbox rows: %w", err)
	}
	return out, nil
}

func (r *Relay) markPublished(ctx context.Context, id string) error {
	return tenancy.Platform(ctx, r.pool, "outbox relay: marking an event published",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE outbox SET published_at = now(), last_error = NULL WHERE id = $1`, id)
			return err
		})
}

// fail records the error and backs the row off: 1s, 2s, 4s, capped.
func (r *Relay) fail(ctx context.Context, p pending, cause error) {
	backoff := r.cfg.BaseBackoff << min(p.attempts, 20)
	if backoff > r.cfg.MaxBackoff || backoff <= 0 {
		backoff = r.cfg.MaxBackoff
	}

	err := tenancy.Platform(ctx, r.pool, "outbox relay: recording a publish failure",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE outbox SET last_error = $2, next_attempt_at = now() + make_interval(secs => $3) WHERE id = $1`,
				p.env.ID, cause.Error(), backoff.Seconds())
			return err
		})
	if err != nil {
		r.log.ErrorContext(ctx, "outbox failure not recorded", "event_id", p.env.ID, "error", err)
		return
	}

	if p.attempts+1 >= r.cfg.MaxAttempts {
		// ADR-0002 §4: retry stops here and an operator is told. The row stays.
		r.log.ErrorContext(ctx, "outbox row exhausted its attempts",
			"event_id", p.env.ID, "type", p.env.Type, "attempts", p.attempts+1, "error", cause)
	}
}

// Lag reports the age of the oldest unpublished row and the size of the backlog,
// which are the two numbers ADR-0002 §4 alerts on.
func (r *Relay) Lag(ctx context.Context) (oldest time.Duration, backlog int, err error) {
	var seconds float64
	err = tenancy.Platform(ctx, r.pool, "outbox relay: reading publication lag",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT coalesce(extract(epoch FROM now() - min(occurred_at)), 0), count(*)
				FROM outbox WHERE published_at IS NULL`).Scan(&seconds, &backlog)
		})
	if err != nil {
		return 0, 0, fmt.Errorf("events: reading outbox lag: %w", err)
	}
	return time.Duration(seconds * float64(time.Second)), backlog, nil
}
