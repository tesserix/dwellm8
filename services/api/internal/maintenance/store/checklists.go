// Package store writes and reads checklist templates, the checklists fired from
// them and their tasks.
//
// One transaction per firing. A checklist with half its tasks written is a process
// that looks under way and cannot be finished, and a caller that wrote twenty tasks
// in twenty calls would leave exactly that on the eleventh failure.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Checklists is the maintenance module's store.
type Checklists struct{ pool tenancy.Pool }

// New takes the request pool: a checklist is always somebody's, so never the
// platform pool.
func New(p tenancy.Pool) *Checklists { return &Checklists{pool: p} }

// ErrNoChecklist is no such checklist — including one hidden by policy, which is
// the same answer to a caller and deliberately so.
var ErrNoChecklist = errors.New("checklist: no such checklist")

// ErrAlreadyOpen is a process fired twice over one tenancy. The caller's answer is
// to show the one that exists, not to retry, so the id comes back with the error.
type ErrAlreadyOpen struct {
	ExistingID string
	Process    domain.Process
}

func (e ErrAlreadyOpen) Error() string {
	return fmt.Sprintf("checklist: a %s is already open for this tenancy (%s)", e.Process, e.ExistingID)
}

// TemplatesFor reads every candidate template for a process — the organisation's own
// and the platform library — and leaves the choice to domain.Resolve.
//
// Both in one query because the resolution order compares them: fetching the
// organisation's first and falling back would take two round trips to answer a
// question one ORDER BY already answers.
func (s *Checklists) TemplatesFor(ctx context.Context, process domain.Process) ([]domain.Template, error) {
	var out []domain.Template
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, tenant_id::text, is_default, process,
			       coalesce(property_kind, ''), name, version
			  FROM checklist_templates
			 WHERE process = $1
			   AND retired_at IS NULL
			   AND published_at IS NOT NULL
			 ORDER BY is_default, property_kind NULLS LAST, version DESC`, string(process))
		if err != nil {
			return err
		}
		defer rows.Close()

		byID := map[string]int{}
		for rows.Next() {
			var t domain.Template
			var kind, proc string
			if err := rows.Scan(&t.ID, &t.TenantID, &t.Default, &proc, &kind, &t.Name, &t.Version); err != nil {
				return err
			}
			t.Process, t.Kind = domain.Process(proc), domain.PropertyKind(kind)
			byID[t.ID] = len(out)
			out = append(out, t)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(out) == 0 {
			return nil
		}

		ids := make([]string, 0, len(out))
		for id := range byID {
			ids = append(ids, id)
		}
		steps, err := tx.Query(ctx, `
			SELECT template_id::text, code::text, title, coalesce(description, ''), position,
			       blocking, owner_role, due_offset_days, depends_on::text[]
			  FROM checklist_template_steps
			 WHERE template_id = ANY ($1::uuid[])
			 ORDER BY template_id, position`, ids)
		if err != nil {
			return err
		}
		defer steps.Close()

		for steps.Next() {
			var templateID string
			var st domain.Step
			var owner string
			if err := steps.Scan(&templateID, &st.Code, &st.Title, &st.Description, &st.Position,
				&st.Blocking, &owner, &st.DueOffsetDays, &st.DependsOn); err != nil {
				return err
			}
			st.Owner = domain.OwnerRole(owner)
			if i, ok := byID[templateID]; ok {
				out[i].Steps = append(out[i].Steps, st)
			}
		}
		return steps.Err()
	})
	return out, err
}

// Trigger writes the checklist and every one of its tasks in one transaction, and
// appends the event on the same commit.
//
// Idempotent by the schema's partial unique index rather than by a lookup first: two
// managers pressing the button at the same moment is the case a check-then-insert
// gets wrong, and the constraint is the only thing that sees both.
func (s *Checklists) Trigger(ctx context.Context, c domain.Checklist, by string) (domain.Checklist, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return domain.Checklist{}, tenancy.ErrNoTenant
	}

	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO checklists (tenant_id, process, template_id, template_version,
			                        property_id, unit_id, lease_id, anchor_on, started_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9)
			RETURNING id::text`,
			tenant.String(), string(c.Process), c.TemplateID, c.TemplateVersion,
			c.PropertyID, nullText(c.UnitID), nullText(c.LeaseID),
			c.AnchorOn.Time(), nullText(by))
		if err := row.Scan(&c.ID); err != nil {
			return fmt.Errorf("writing the checklist: %w", err)
		}

		for i, t := range c.Tasks {
			var id string
			row := tx.QueryRow(ctx, `
				INSERT INTO checklist_tasks (tenant_id, checklist_id, step_code, title, position,
				                             blocking, owner_role, assignee_party_id, due_on,
				                             depends_on, state)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::date, coalesce($10::citext[], '{}'), $11)
				RETURNING id::text`,
				tenant.String(), c.ID, t.StepCode, t.Title, t.Position,
				t.Blocking, string(t.Owner), nullText(t.Assignee), t.DueOn.Time(),
				t.DependsOn, string(t.State))
			if err := row.Scan(&id); err != nil {
				return fmt.Errorf("writing the step %q: %w", t.StepCode, err)
			}
			c.Tasks[i].ID = id
		}

		env, err := startedEvent(tenant.String(), c)
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})

	if isUniqueViolation(err, "checklists_one_open_per_lease") {
		existing, lookupErr := s.openFor(ctx, c.LeaseID, c.Process)
		if lookupErr != nil {
			return domain.Checklist{}, err
		}
		return domain.Checklist{}, ErrAlreadyOpen{ExistingID: existing, Process: c.Process}
	}
	if err != nil {
		return domain.Checklist{}, err
	}
	return c, nil
}

// Read returns one checklist with its tasks, or ErrNoChecklist.
func (s *Checklists) Read(ctx context.Context, id string) (domain.Checklist, error) {
	var c domain.Checklist
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return readInto(ctx, tx, id, &c)
	})
	return c, err
}

func readInto(ctx context.Context, tx pgx.Tx, id string, c *domain.Checklist) error {
	var process, state, unit, lease string
	var anchor time.Time
	err := tx.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, process, template_id::text, template_version,
		       property_id::text, coalesce(unit_id::text, ''), coalesce(lease_id::text, ''),
		       anchor_on, state
		  FROM checklists WHERE id = $1`, id).
		Scan(&c.ID, &c.TenantID, &process, &c.TemplateID, &c.TemplateVersion,
			&c.PropertyID, &unit, &lease, &anchor, &state)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNoChecklist
	}
	if err != nil {
		return err
	}
	c.Process, c.State = domain.Process(process), domain.State(state)
	c.UnitID, c.LeaseID = unit, lease
	c.AnchorOn = effective.DateOf(anchor, anchor.Location())

	rows, err := tx.Query(ctx, `
		SELECT id::text, step_code::text, title, position, blocking, owner_role,
		       coalesce(assignee_party_id::text, ''), due_on, state, depends_on::text[]
		  FROM checklist_tasks WHERE checklist_id = $1 ORDER BY position`, id)
	if err != nil {
		return err
	}
	defer rows.Close()

	c.Tasks = nil
	for rows.Next() {
		var t domain.Task
		var owner, state string
		var due time.Time
		if err := rows.Scan(&t.ID, &t.StepCode, &t.Title, &t.Position, &t.Blocking, &owner,
			&t.Assignee, &due, &state, &t.DependsOn); err != nil {
			return err
		}
		t.Owner, t.State = domain.OwnerRole(owner), domain.TaskState(state)
		t.DueOn = effective.DateOf(due, due.Location())
		c.Tasks = append(c.Tasks, t)
	}
	return rows.Err()
}

// Settle writes one task's new state and whatever it released, on one transaction.
//
// The whole checklist is read inside the transaction and the rules are evaluated in
// Go, so the refusal names the step. The schema's triggers check the same things
// again on the way out — see ADR-0032 §4 for why both.
func (s *Checklists) Settle(ctx context.Context, id, stepCode string, to domain.TaskState,
	by, reason string) (domain.Checklist, error) {

	tenant, ok := tenancy.From(ctx)
	if !ok {
		return domain.Checklist{}, tenancy.ErrNoTenant
	}

	var out domain.Checklist
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var before domain.Checklist
		if err := readInto(ctx, tx, id, &before); err != nil {
			return err
		}

		var after domain.Checklist
		var err error
		switch to {
		case domain.TaskDone:
			after, err = before.Complete(stepCode)
		case domain.TaskSkipped:
			after, err = before.Skip(stepCode, reason)
		default:
			err = fmt.Errorf("checklist: %q is not something a task can be settled to", to)
		}
		if err != nil {
			return err
		}

		for i, t := range after.Tasks {
			if t.State == before.Tasks[i].State {
				continue
			}
			switch t.State {
			case domain.TaskDone:
				_, err = tx.Exec(ctx, `
					UPDATE checklist_tasks SET state = 'done', completed_at = now(), completed_by = $3
					 WHERE id = $1 AND tenant_id = $2`, t.ID, tenant.String(), nullText(by))
			case domain.TaskSkipped:
				_, err = tx.Exec(ctx, `
					UPDATE checklist_tasks SET state = 'skipped', skipped_reason = $3, completed_by = $4
					 WHERE id = $1 AND tenant_id = $2`, t.ID, tenant.String(), reason, nullText(by))
			case domain.TaskPending:
				// Released by the settlement above. The schema's trigger does this
				// too and lands on the same rows, so the second write is a no-op
				// rather than a conflict.
				_, err = tx.Exec(ctx, `
					UPDATE checklist_tasks SET state = 'pending'
					 WHERE id = $1 AND tenant_id = $2 AND state = 'blocked'`, t.ID, tenant.String())
			}
			if err != nil {
				return fmt.Errorf("settling %s: %w", t.Title, err)
			}
		}

		env, err := taskEvent(tenant.String(), after, stepCode, to, reason)
		if err != nil {
			return err
		}
		if err := events.Append(ctx, tx, env); err != nil {
			return err
		}
		out = after
		return nil
	})
	return out, err
}

// Close moves a checklist to completed or abandoned, refusing a completion while
// blocking steps are outstanding.
func (s *Checklists) Close(ctx context.Context, id string, to domain.State, reason string) (domain.Checklist, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return domain.Checklist{}, tenancy.ErrNoTenant
	}

	var out domain.Checklist
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var before domain.Checklist
		if err := readInto(ctx, tx, id, &before); err != nil {
			return err
		}

		var after domain.Checklist
		var err error
		var ev domain.Event
		switch to {
		case domain.StateCompleted:
			after, err = before.Finish()
			ev = domain.EventCompleted
		case domain.StateAbandoned:
			after, err = before.Abandon(reason)
			ev = domain.EventAbandoned
		default:
			err = fmt.Errorf("checklist: %q is not an ending", to)
		}
		if err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE checklists
			   SET state = $3,
			       completed_at = CASE WHEN $3 = 'completed' THEN now() ELSE completed_at END,
			       abandoned_at = CASE WHEN $3 = 'abandoned' THEN now() ELSE abandoned_at END,
			       abandoned_reason = CASE WHEN $3 = 'abandoned' THEN $4 ELSE abandoned_reason END
			 WHERE id = $1 AND tenant_id = $2 AND state = 'open'`,
			id, tenant.String(), string(to), nullText(reason))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoChecklist
		}

		env, err := events.New(string(ev), tenant.String(),
			events.Subject{Kind: "checklist", ID: id},
			events.Actor{Kind: events.ActorSystem},
			struct {
				Process string `json:"process"`
				LeaseID string `json:"lease_id,omitempty"`
				Reason  string `json:"reason,omitempty"`
			}{Process: string(after.Process), LeaseID: after.LeaseID, Reason: reason})
		if err != nil {
			return err
		}
		if err := events.Append(ctx, tx, env); err != nil {
			return err
		}
		out = after
		return nil
	})
	return out, err
}

// Outstanding returns the blocking steps standing in front of a lease transition.
//
// The lease module's question, answered without it reaching this module's tables:
// one query, ordered, so the refusal names them in the order somebody would do them.
func (s *Checklists) Outstanding(ctx context.Context, leaseID string, process domain.Process) ([]domain.Task, error) {
	var out []domain.Task
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT t.id::text, t.step_code::text, t.title, t.position, t.owner_role, t.due_on, t.state
			  FROM checklist_tasks t
			  JOIN checklists c ON c.id = t.checklist_id
			 WHERE c.lease_id = $1 AND c.process = $2 AND c.state = 'open'
			   AND t.blocking AND t.state NOT IN ('done', 'skipped')
			 ORDER BY t.position`, leaseID, string(process))
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var t domain.Task
			var owner, state string
			var due time.Time
			if err := rows.Scan(&t.ID, &t.StepCode, &t.Title, &t.Position, &owner, &due, &state); err != nil {
				return err
			}
			t.Blocking = true
			t.Owner, t.State = domain.OwnerRole(owner), domain.TaskState(state)
			t.DueOn = effective.DateOf(due, due.Location())
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// Progress is a row of the portfolio view.
type Progress struct {
	ChecklistID         string
	Process             domain.Process
	PropertyID          string
	UnitID              string
	LeaseID             string
	State               domain.State
	Tasks               int
	Settled             int
	Outstanding         int
	BlockingOutstanding int
	NextDueOn           effective.Date
	DaysOverdue         int
}

// Portfolio reads progress across the organisation, newest first, and says which
// rows are late.
//
// Lateness comes from the view rather than from a comparison here: the same
// definition serves this, the Ops screen and anything that alerts on it, and one
// that is computed in three places is one that disagrees in two.
func (s *Checklists) Portfolio(ctx context.Context, state domain.State, limit int) ([]Progress, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var out []Progress
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT p.checklist_id::text, p.process, p.property_id::text,
			       coalesce(p.unit_id::text, ''), coalesce(p.lease_id::text, ''), p.state,
			       p.tasks, p.settled, p.outstanding, p.blocking_outstanding, p.next_due_on,
			       coalesce(s.days_overdue, 0)
			  FROM checklist_progress p
			  LEFT JOIN checklist_stalled s ON s.checklist_id = p.checklist_id
			 WHERE ($1 = '' OR p.state = $1)
			 ORDER BY s.days_overdue DESC NULLS LAST, p.next_due_on NULLS LAST
			 LIMIT $2`, string(state), limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var p Progress
			var process, st string
			var due *time.Time
			if err := rows.Scan(&p.ChecklistID, &process, &p.PropertyID, &p.UnitID, &p.LeaseID, &st,
				&p.Tasks, &p.Settled, &p.Outstanding, &p.BlockingOutstanding, &due,
				&p.DaysOverdue); err != nil {
				return err
			}
			p.Process, p.State = domain.Process(process), domain.State(st)
			if due != nil {
				p.NextDueOn = effective.DateOf(*due, due.Location())
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// openFor finds the checklist that blocked a second firing, so the refusal can name
// it rather than describe it.
func (s *Checklists) openFor(ctx context.Context, leaseID string, process domain.Process) (string, error) {
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id::text FROM checklists
			 WHERE lease_id = $1 AND process = $2 AND state = 'open' LIMIT 1`,
			leaseID, string(process)).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNoChecklist
	}
	return id, err
}

func startedEvent(tenant string, c domain.Checklist) (events.Envelope, error) {
	type step struct {
		Code     string `json:"code"`
		Title    string `json:"title"`
		Blocking bool   `json:"blocking"`
		DueOn    string `json:"due_on"`
	}
	data := struct {
		Process    string `json:"process"`
		TemplateID string `json:"template_id"`
		Version    int    `json:"template_version"`
		PropertyID string `json:"property_id"`
		UnitID     string `json:"unit_id,omitempty"`
		LeaseID    string `json:"lease_id,omitempty"`
		AnchorOn   string `json:"anchor_on"`
		Steps      []step `json:"steps"`
	}{
		Process: string(c.Process), TemplateID: c.TemplateID, Version: c.TemplateVersion,
		PropertyID: c.PropertyID, UnitID: c.UnitID, LeaseID: c.LeaseID,
		AnchorOn: c.AnchorOn.String(),
	}
	// The steps travel with the event, for the reason lease.tenancy.started carries
	// its parties: a consumer must not have to read a table that a later settlement
	// may have changed by the time the event is handled.
	for _, t := range c.Tasks {
		data.Steps = append(data.Steps, step{
			Code: t.StepCode, Title: t.Title, Blocking: t.Blocking, DueOn: t.DueOn.String()})
	}

	return events.New(string(domain.EventStarted), tenant,
		events.Subject{Kind: "checklist", ID: c.ID},
		events.Actor{Kind: events.ActorSystem}, data)
}

func taskEvent(tenant string, c domain.Checklist, stepCode string,
	to domain.TaskState, reason string) (events.Envelope, error) {

	typ := domain.EventTaskCompleted
	if to == domain.TaskSkipped {
		typ = domain.EventTaskSkipped
	}
	return events.New(string(typ), tenant,
		events.Subject{Kind: "checklist", ID: c.ID},
		events.Actor{Kind: events.ActorSystem},
		struct {
			Process  string `json:"process"`
			StepCode string `json:"step_code"`
			LeaseID  string `json:"lease_id,omitempty"`
			Reason   string `json:"reason,omitempty"`
			// What is left, so a consumer can tell the settlement that finished a
			// process from the one that did not without asking.
			BlockingOutstanding int `json:"blocking_outstanding"`
		}{
			Process: string(c.Process), StepCode: stepCode, LeaseID: c.LeaseID,
			Reason: reason, BlockingOutstanding: len(c.Outstanding()),
		})
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == "23505" && pgErr.ConstraintName == constraint
}
