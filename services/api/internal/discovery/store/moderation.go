package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/discovery/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Listing moderation (#143). Reports come from strangers on the platform pool;
// judgement — suspend, reinstate, dismiss — is the organisation admin's,
// scoped and audited on the outbox like every other moderation act.

// Moderation reads and writes the moderation columns on listings.
type Moderation struct {
	pool     tenancy.Pool
	platform tenancy.PlatformPool
}

// NewModeration builds the store.
func NewModeration(p tenancy.Pool, plat tenancy.PlatformPool) *Moderation {
	return &Moderation{pool: p, platform: plat}
}

// ReportListing flags a listing from the public side — anyone may pull the
// cord on an advert that is actually visible; anything else is a silent yes.
func (s *Moderation) ReportListing(ctx context.Context, id, reason string) error {
	return tenancy.Platform(ctx, s.platform, "reporting a listing (#143)",
		func(ctx context.Context, tx pgx.Tx) error {
			var tenant string
			err := tx.QueryRow(ctx, `
				UPDATE listings
				   SET report_count = report_count + 1,
				       reported_at = coalesce(reported_at, now()), updated_at = now()
				 WHERE id = $1 AND state IN ('live', 'paused', 'suspended')
				 RETURNING tenant_id::text`, id).Scan(&tenant)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil // invisible or gone: the reporter learns nothing either way
			}
			if err != nil {
				return err
			}
			env, err := events.New(string(domain.EventReported), tenant,
				events.Subject{Kind: "listing", ID: id},
				events.Actor{Kind: events.ActorSystem},
				struct {
					Reason string `json:"reason"`
				}{reason})
			if err != nil {
				return err
			}
			return events.Append(ctx, tx, env)
		})
}

// Suspend takes a listing off the market by moderation, the reason on the row
// and the fact on the outbox. The lifecycle owns which states may be suspended.
func (s *Moderation) Suspend(ctx context.Context, id, reason string, actor events.Actor) error {
	return s.judge(ctx, id, domain.StateSuspended, reason, actor)
}

// Reinstate lifts a suspension, clearing the report so the queue empties.
func (s *Moderation) Reinstate(ctx context.Context, id string, actor events.Actor) error {
	return s.judge(ctx, id, domain.StateLive, "", actor)
}

func (s *Moderation) judge(ctx context.Context, id string, to domain.State,
	reason string, actor events.Actor) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var from string
		err := tx.QueryRow(ctx,
			`SELECT state FROM listings WHERE id = $1 FOR UPDATE`, id).Scan(&from)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoListing
		}
		if err != nil {
			return err
		}
		ev, err := domain.Transition(domain.State(from), to)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE listings
			   SET state = $2,
			       suspended_reason = $3,
			       reported_at = CASE WHEN $2 = 'live' THEN NULL ELSE reported_at END,
			       report_count = CASE WHEN $2 = 'live' THEN 0 ELSE report_count END,
			       updated_at = now()
			 WHERE id = $1`, id, string(to), nullText(reason)); err != nil {
			return err
		}
		env, err := events.New(string(ev), tenant.String(),
			events.Subject{Kind: "listing", ID: id}, actor,
			struct {
				Reason string `json:"reason,omitempty"`
			}{reason})
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})
}

// DismissReports clears a report the reviewer judged wrong, leaving the
// listing exactly where it was.
func (s *Moderation) DismissReports(ctx context.Context, id string) error {
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE listings SET reported_at = NULL, report_count = 0, updated_at = now()
			 WHERE id = $1 AND reported_at IS NOT NULL`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoListing
		}
		return nil
	})
}

// FlaggedListing is one row of the listing moderation queue.
type FlaggedListing struct {
	ID              string
	Headline        string
	State           string
	ReportCount     int
	ReportedAt      *time.Time
	SuspendedReason string
}

// Queue is the listing moderation queue: everything reported or suspended in
// the organisation, longest-waiting report first.
func (s *Moderation) Queue(ctx context.Context) ([]FlaggedListing, error) {
	var out []FlaggedListing
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, headline, state, report_count, reported_at,
			       coalesce(suspended_reason, '')
			  FROM listings
			 WHERE tenant_id = current_tenant_id()
			   AND (reported_at IS NOT NULL OR state = 'suspended')
			 ORDER BY reported_at NULLS LAST, updated_at
			 LIMIT 200`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var f FlaggedListing
			if err := rows.Scan(&f.ID, &f.Headline, &f.State, &f.ReportCount,
				&f.ReportedAt, &f.SuspendedReason); err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	})
	return out, err
}
