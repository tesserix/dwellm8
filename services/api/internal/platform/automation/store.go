package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// SQLStore is the engine's persistence against PostgreSQL.
//
// Every method runs through tenancy.Scoped, including the ones a background job
// calls: ADR-0028 §3's rule is that the enumeration of organisations is the only
// privileged query in a run, and everything after it happens inside one
// organisation's session with row-level security on.
type SQLStore struct{ pool tenancy.Pool }

// NewStore takes the request pool. An automation always acts for somebody.
func NewStore(p tenancy.Pool) *SQLStore { return &SQLStore{pool: p} }

// Overrides reads what this organisation changed. An empty map is the ordinary
// case and means every automation runs on its defaults.
func (s *SQLStore) Overrides(ctx context.Context) (map[Key]Override, error) {
	out := map[Key]Override{}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT automation, enabled, params, approval_ceiling_minor
			  FROM automation_settings`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var key string
			var enabled *bool
			var params []byte
			var ceiling *int64
			if err := rows.Scan(&key, &enabled, &params, &ceiling); err != nil {
				return err
			}
			o := Override{Enabled: enabled, CeilingMinor: ceiling}
			if len(params) > 0 {
				if err := json.Unmarshal(params, &o.Params); err != nil {
					return fmt.Errorf("reading the parameters of %s: %w", key, err)
				}
			}
			out[Key(key)] = o
		}
		return rows.Err()
	})
	return out, err
}

// Save writes one override, replacing whatever was there.
//
// Upsert rather than insert-or-update in Go: two people on the settings screen at
// once is not exotic, and a read-then-write would have one of them silently
// overwrite the other's parameter with a stale value.
func (s *SQLStore) Save(ctx context.Context, key Key, o Override, by string) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	params, err := json.Marshal(o.Params)
	if err != nil {
		return fmt.Errorf("automation: encoding the parameters of %s: %w", key, err)
	}
	if o.Params == nil {
		params = []byte(`{}`)
	}

	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO automation_settings (tenant_id, automation, enabled, params,
			                                 approval_ceiling_minor, updated_by)
			VALUES ($1, $2, $3, $4::jsonb, $5, $6)
			ON CONFLICT (tenant_id, automation) DO UPDATE
			   SET enabled = EXCLUDED.enabled,
			       params = EXCLUDED.params,
			       approval_ceiling_minor = EXCLUDED.approval_ceiling_minor,
			       updated_at = now(),
			       updated_by = EXCLUDED.updated_by`,
			tenant.String(), key.String(), o.Enabled, params, o.CeilingMinor, nullText(by))
		return err
	})
}

// Recorded reports whether this proposal has already been run.
func (s *SQLStore) Recorded(ctx context.Context, key Key, idempotencyKey string) (bool, error) {
	var found bool
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS (
			    SELECT 1 FROM automation_runs
			     WHERE automation = $1 AND idempotency_key = $2)`,
			key.String(), idempotencyKey).Scan(&found)
	})
	return found, err
}

// Record writes a run, reporting false when the key was already used.
func (s *SQLStore) Record(ctx context.Context, r Record) (bool, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return false, tenancy.ErrNoTenant
	}
	params, err := json.Marshal(r.Params)
	if err != nil {
		return false, fmt.Errorf("automation: encoding the parameters of %s: %w", r.Automation, err)
	}
	if r.Params == nil {
		params = []byte(`{}`)
	}

	var written bool
	err = tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO automation_runs (tenant_id, automation, subject_kind, subject_id,
			                             outcome, action, detail, params, amount_minor,
			                             idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb, $9, $10)
			ON CONFLICT (tenant_id, automation, idempotency_key) DO NOTHING`,
			tenant.String(), r.Automation.String(), string(r.Subject.Kind), r.Subject.ID,
			string(r.Outcome), r.Action, nullText(r.Detail), params,
			nullAmount(r.Amount), r.Key)
		if err != nil {
			return err
		}
		written = tag.RowsAffected() == 1
		return nil
	})
	return written, err
}

// Requested reports whether a live approval already exists for this proposal.
//
// Live means pending or approved. A declined one is deliberately not live: the
// automation may ask again on a later pass, because a decline is an answer about
// today's proposal rather than a permanent rule.
func (s *SQLStore) Requested(ctx context.Context, key Key, idempotencyKey string) (bool, error) {
	var found bool
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT EXISTS (
			    SELECT 1 FROM automation_approvals
			     WHERE automation = $1 AND idempotency_key = $2
			       AND state IN ('pending', 'approved'))`,
			key.String(), idempotencyKey).Scan(&found)
	})
	return found, err
}

// RequestApproval writes the request.
func (s *SQLStore) RequestApproval(ctx context.Context, a ApprovalRequest) (bool, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return false, tenancy.ErrNoTenant
	}
	var written bool
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			INSERT INTO automation_approvals (tenant_id, automation, subject_kind, subject_id,
			                                  action, amount_minor, ceiling_minor, idempotency_key)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (tenant_id, automation, idempotency_key) DO NOTHING`,
			tenant.String(), a.Automation.String(), string(a.Subject.Kind), a.Subject.ID,
			a.Action, a.Amount, a.Ceiling, a.Key)
		if err != nil {
			return err
		}
		written = tag.RowsAffected() == 1
		return nil
	})
	return written, err
}

// RunRecord is one row of the log, as a screen reads it.
type RunRecord struct {
	ID         string
	Automation Key
	Subject    Subject
	Outcome    Outcome
	Action     string
	Detail     string
	Amount     int64
	OccurredAt time.Time
}

// History is what was automated on one record, latest first. ADR-0033 §4 — the
// story's "a record must show which automation caused an action and when".
func (s *SQLStore) History(ctx context.Context, subject Subject, limit int) ([]RunRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []RunRecord
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, automation, subject_kind, subject_id::text, outcome, action,
			       coalesce(detail, ''), coalesce(amount_minor, 0), occurred_at
			  FROM automation_runs
			 WHERE subject_kind = $1 AND subject_id = $2
			 ORDER BY occurred_at DESC
			 LIMIT $3`, string(subject.Kind), subject.ID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var r RunRecord
			var key, kind, outcome string
			if err := rows.Scan(&r.ID, &key, &kind, &r.Subject.ID, &outcome, &r.Action,
				&r.Detail, &r.Amount, &r.OccurredAt); err != nil {
				return err
			}
			r.Automation, r.Subject.Kind, r.Outcome = Key(key), SubjectKind(kind), Outcome(outcome)
			out = append(out, r)
		}
		return rows.Err()
	})
	return out, err
}

// Activity is what the settings screen shows beside each switch.
type Activity struct {
	Automation Key
	Runs       int
	Acted      int
	Awaiting   int
	Failed     int
	LastRunAt  time.Time
	LastActed  time.Time
}

// Activity reads the derived per-automation counts.
func (s *SQLStore) Activity(ctx context.Context) (map[Key]Activity, error) {
	out := map[Key]Activity{}
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT automation, runs, acted, awaiting_approval, failed, last_run_at, last_acted_at
			  FROM automation_activity`)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var a Activity
			var key string
			var lastRun, lastActed *time.Time
			if err := rows.Scan(&key, &a.Runs, &a.Acted, &a.Awaiting, &a.Failed,
				&lastRun, &lastActed); err != nil {
				return err
			}
			a.Automation = Key(key)
			if lastRun != nil {
				a.LastRunAt = *lastRun
			}
			if lastActed != nil {
				a.LastActed = *lastActed
			}
			out[a.Automation] = a
		}
		return rows.Err()
	})
	return out, err
}

// Approval is a request as somebody deciding on it sees it.
type Approval struct {
	ID          string
	Automation  Key
	Subject     Subject
	Action      string
	Amount      int64
	Ceiling     int64
	State       string
	RequestedAt time.Time
}

// ErrNoApproval is no such request — including one hidden by policy.
var ErrNoApproval = errors.New("automation: no such approval")

// Pending lists what is waiting on somebody, oldest first: the queue is a queue,
// and showing the newest request at the top buries the one that has been waiting.
func (s *SQLStore) Pending(ctx context.Context, limit int) ([]Approval, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var out []Approval
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, automation, subject_kind, subject_id::text, action,
			       amount_minor, ceiling_minor, state, requested_at
			  FROM automation_approvals
			 WHERE state = 'pending'
			 ORDER BY requested_at
			 LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var a Approval
			var key, kind string
			if err := rows.Scan(&a.ID, &key, &kind, &a.Subject.ID, &a.Action,
				&a.Amount, &a.Ceiling, &a.State, &a.RequestedAt); err != nil {
				return err
			}
			a.Automation, a.Subject.Kind = Key(key), SubjectKind(kind)
			out = append(out, a)
		}
		return rows.Err()
	})
	return out, err
}

// Decide records an approval or a decline.
//
// It does not perform the action. Granting an approval releases the proposal for
// the next run, which then goes through Propose like any other and is subject to
// the ceiling as it stands at that moment — so raising a ceiling and approving a
// request are the same act with the same audit trail rather than two paths.
func (s *SQLStore) Decide(ctx context.Context, id, state, reason, by string) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	if state != "approved" && state != "declined" {
		return fmt.Errorf("automation: %q is not a decision", state)
	}
	if state == "declined" && reason == "" {
		return errors.New("automation: a declined request must say why — the automation will " +
			"ask again, and the next person needs to know what was decided")
	}

	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE automation_approvals
			   SET state = $3, decided_reason = $4, decided_by = $5
			 WHERE id = $1 AND tenant_id = $2 AND state = 'pending'`,
			id, tenant.String(), state, nullText(reason), nullText(by))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoApproval
		}
		return nil
	})
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullAmount(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
