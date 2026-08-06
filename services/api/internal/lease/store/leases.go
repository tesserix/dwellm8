// Package store writes and reads leases, their parties, their rent schedule and
// the tax facts a tenancy cannot start without.
//
// One transaction per lease. A lease with no tenant on it, or a tenancy that
// went live with no TDS section, is not a state this table may be left in for
// even a moment — and a caller that wrote them in four calls would leave exactly
// that state on the fourth failure.
package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Leases is the lease module's store.
type Leases struct{ pool tenancy.Pool }

// New takes the request pool: a lease is always somebody's, so never the
// platform pool.
func New(p tenancy.Pool) *Leases { return &Leases{pool: p} }

// ErrDoubleLet is a lease that would let a flat already let over the same days.
// Distinguishable because the caller's answer is to show the conflicting
// tenancy, not to retry.
var ErrDoubleLet = errors.New("lease: the unit is already let over those days")

// ErrNoLease is no such lease — including one hidden by policy, which is the
// same answer to a caller and deliberately so.
var ErrNoLease = errors.New("lease: no such lease")

// Created is what Create wrote.
type Created struct {
	ID    string
	Lease domain.Lease
	Terms domain.Terms
	Event domain.Event
}

// Create writes the lease, its parties, its opening rent schedule and its tax
// facts in one transaction.
//
// The lease is written in draft. Activation is a separate call, because that is
// where the no-double-let constraint and the tax gate bite and where an event
// with consequences is published.
func (s *Leases) Create(ctx context.Context, d domain.Draft) (Created, error) {
	l, terms, ev, err := d.Create()
	if err != nil {
		return Created{}, err
	}
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return Created{}, tenancy.ErrNoTenant
	}

	var id string
	err = tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO leases (tenant_id, property_id, unit_id, state, valid_from, valid_to,
			                    notice_days, lock_in_until, renews_lease_id)
			VALUES ($1, $2, $3, 'draft', $4::date, $5, $6, $7, $8)
			RETURNING id`,
			tenant.String(), d.Property, d.Unit,
			d.Term.From().Time(), nullDate(d.Term.To()),
			d.NoticeDays, nullDate(d.LockInUntil), nullText(d.Renews))
		if err := row.Scan(&id); err != nil {
			return fmt.Errorf("writing the lease: %w", err)
		}

		for _, p := range d.Parties {
			if _, err := tx.Exec(ctx, `
				INSERT INTO lease_parties (tenant_id, lease_id, party_id, role, valid_from)
				VALUES ($1, $2, $3, $4, $5::date)`,
				tenant.String(), id, p.PartyID, string(p.Role), d.Term.From().Time()); err != nil {
				return fmt.Errorf("writing %s as %s: %w", p.Name, p.Role, err)
			}
		}

		// The opening rent. A revision later is a new row that closes this one
		// (ADR-0008), never an update — which is why this is an insert of a
		// schedule rather than a column on the lease.
		if _, err := tx.Exec(ctx, `
			INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, due_day, valid_from, valid_to)
			VALUES ($1, $2, $3, $4, $5::date, $6)`,
			tenant.String(), id, terms.RentMinor, int(terms.DueDay),
			d.Term.From().Time(), nullDate(d.Term.To())); err != nil {
			return fmt.Errorf("writing the rent schedule: %w", err)
		}

		return writeTaxFacts(ctx, tx, tenant.String(), id, d.Tax)
	})
	if err != nil {
		return Created{}, err
	}

	l.ID = id
	return Created{ID: id, Lease: l, Terms: terms, Event: ev}, nil
}

// writeTaxFacts writes each set of facts as its own effective-dated row.
func writeTaxFacts(ctx context.Context, tx pgx.Tx, tenant, lease string, h tds.History) error {
	for _, rec := range h.Timeline.Live() {
		f := rec.Value
		if _, err := tx.Exec(ctx, `
			INSERT INTO lease_tax_facts (tenant_id, lease_id, deductor_class, landlord_residency,
			                             source, acknowledged_on, acknowledged_by,
			                             valid_from, valid_to)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9)`,
			tenant, lease, string(f.Deductor), string(f.Residency), f.Source,
			nullDate(f.AcknowledgedOn), nullText(f.AcknowledgedBy),
			rec.Range.From().Time(), nullDate(rec.Range.To())); err != nil {
			return fmt.Errorf("writing the tax facts effective %s: %w", f.From, err)
		}
	}
	return nil
}

// Offer sends a draft out for signature. Nothing is published: ADR-0010 §3
// gives the edge no event because nobody outside the lease acts on it.
//
// A lease already out is left where it is rather than refused — a manager who
// presses send twice has not made a mistake.
func (s *Leases) Offer(ctx context.Context, id string, by domain.Actor) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}
	if !domain.MayTransition(domain.StateDraft, domain.StatePendingSignature, by) {
		return fmt.Errorf("%w: %s may not send a lease for signature", domain.ErrTransition, by)
	}
	return tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'pending_signature'
			 WHERE id = $1 AND tenant_id = $2 AND state IN ('draft', 'pending_signature')`,
			id, tenant.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoLease
		}
		return nil
	})
}

// Activate moves a lease to active, and is where the two guards the database
// owns actually fire: the no-double-let exclusion and the deferred tax-path
// trigger.
//
// Both are refusals the caller must be able to tell apart — one names another
// tenancy, the other names a missing form — so the constraint violations are
// mapped rather than passed up as an opaque 23P01.
func (s *Leases) Activate(ctx context.Context, id string, by domain.Actor) error {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return tenancy.ErrNoTenant
	}

	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `
			UPDATE leases SET state = 'active'
			 WHERE id = $1 AND tenant_id = $2 AND state = 'pending_signature'`,
			id, tenant.String())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoLease
		}
		// The fact, in the same transaction as the state it reports (ADR-0002).
		// It carries the parties because its consumers — the authz projector
		// first (#151) — must not have to read a table that a later revision
		// may have re-dated by the time the event is handled.
		env, err := startedEvent(ctx, tx, tenant.String(), id, by)
		if err != nil {
			return err
		}
		return events.Append(ctx, tx, env)
	})

	switch {
	case err == nil:
		return nil
	case isExclusionViolation(err, "leases_no_double_let"):
		conflict, lookupErr := s.conflicting(ctx, id)
		if lookupErr != nil || conflict == "" {
			return fmt.Errorf("%w: %v", ErrDoubleLet, err)
		}
		return fmt.Errorf("%w: lease %s already lets that unit over those days",
			ErrDoubleLet, conflict)
	default:
		return err
	}
}

// ActiveTenancy is what the authz reconciliation compares against the graph:
// the lease, its unit, and the resident-side parties still on it. dwellm8#151.
type ActiveTenancy struct {
	ID      string
	UnitID  string
	Parties []struct{ PartyID, Role string }
}

// ActiveTenancies lists every active lease with its live parties, scoped to
// the organisation in the context like every other read here.
func (s *Leases) ActiveTenancies(ctx context.Context, limit int) ([]ActiveTenancy, error) {
	if limit <= 0 {
		limit = 5000
	}
	var out []ActiveTenancy
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT l.id::text, l.unit_id::text, p.party_id::text, p.role
			  FROM leases l
			  JOIN lease_parties p ON p.lease_id = l.id AND p.retired_at IS NULL
			 WHERE l.state = 'active'
			 ORDER BY l.id
			 LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		byLease := map[string]int{}
		for rows.Next() {
			var leaseID, unit, party, role string
			if err := rows.Scan(&leaseID, &unit, &party, &role); err != nil {
				return err
			}
			i, ok := byLease[leaseID]
			if !ok {
				i = len(out)
				byLease[leaseID] = i
				out = append(out, ActiveTenancy{ID: leaseID, UnitID: unit})
			}
			out[i].Parties = append(out[i].Parties, struct{ PartyID, Role string }{party, role})
		}
		return rows.Err()
	})
	return out, err
}

// startedEvent builds lease.tenancy.started with the unit and every party on
// the lease, read inside the activating transaction.
func startedEvent(ctx context.Context, tx pgx.Tx, tenant, id string, by domain.Actor) (events.Envelope, error) {
	var unit string
	if err := tx.QueryRow(ctx, `SELECT unit_id FROM leases WHERE id = $1`, id).Scan(&unit); err != nil {
		return events.Envelope{}, fmt.Errorf("reading the unit for the event: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT party_id, role FROM lease_parties WHERE lease_id = $1`, id)
	if err != nil {
		return events.Envelope{}, fmt.Errorf("reading the parties for the event: %w", err)
	}
	defer rows.Close()

	type party struct {
		PartyID string `json:"party_id"`
		Role    string `json:"role"`
	}
	data := struct {
		UnitID string `json:"unit_id"`
		// Which side started the tenancy. The actor below is system until the
		// request principal carries a party uuid (#229): a role label is not an
		// id, and the outbox's actor_id column rightly refused one.
		ActivatedBy string  `json:"activated_by"`
		Parties     []party `json:"parties"`
	}{UnitID: unit, ActivatedBy: string(by)}
	for rows.Next() {
		var p party
		if err := rows.Scan(&p.PartyID, &p.Role); err != nil {
			return events.Envelope{}, err
		}
		data.Parties = append(data.Parties, p)
	}
	if err := rows.Err(); err != nil {
		return events.Envelope{}, err
	}

	return events.New(string(domain.EventStarted), tenant,
		events.Subject{Kind: "lease", ID: id},
		events.Actor{Kind: events.ActorSystem}, data)
}

// ActiveOnUnit returns the tenancy occupying a unit on a date, "" when none.
// Active and in-notice count: a tenancy under notice still occupies the flat.
func (s *Leases) ActiveOnUnit(ctx context.Context, unitID string, on effective.Date) (string, error) {
	var id string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT id::text
			  FROM leases
			 WHERE unit_id = $1 AND state IN ('active', 'in_notice')
			   AND validity @> $2::date
			 LIMIT 1`, unitID, on.Time()).Scan(&id)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return id, err
}

// conflicting finds the tenancy that blocked an activation, so the refusal can
// name it rather than describe it.
func (s *Leases) conflicting(ctx context.Context, id string) (string, error) {
	var other string
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT other.id
			  FROM leases mine
			  JOIN leases other
			    ON other.unit_id = mine.unit_id
			   AND other.id <> mine.id
			   AND other.validity && mine.validity
			   AND other.state IN ('active', 'in_notice', 'renewed', 'terminated', 'settled')
			 WHERE mine.id = $1
			 LIMIT 1`, id).Scan(&other)
	})
	if err != nil {
		return "", err
	}
	return other, nil
}

// Terms reads the rent in force for a lease on a date.
func (s *Leases) Terms(ctx context.Context, id string, on effective.Date) (domain.Terms, error) {
	var (
		amount int64
		dueDay int
	)
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT amount_minor, due_day
			  FROM rent_schedule
			 WHERE lease_id = $1 AND retired_at IS NULL AND validity @> $2::date`,
			id, on.Time()).Scan(&amount, &dueDay)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Terms{}, fmt.Errorf("%w: no rent in force on %s", ErrNoLease, on)
	}
	if err != nil {
		return domain.Terms{}, err
	}
	return domain.Terms{
		RentMinor: amount, DueDay: domain.DueDay(dueDay), Cycle: domain.Monthly,
	}, nil
}

func nullDate(d effective.Date) any {
	if d.Zero() {
		return nil
	}
	return d.Time()
}

func nullText(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func isExclusionViolation(err error, constraint string) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return false
	}
	// 23P01 is exclusion_violation. The name is checked as well as the code
	// because a schema with two exclusion constraints on one table would
	// otherwise report the wrong one.
	return pg.Code == "23P01" && strings.Contains(pg.ConstraintName, constraint)
}

// Parties returns the tenant and the owner a posting is kept per.
//
// The tenant comes from lease_parties — the first one liable, which for a joint
// tenancy is a simplification this returns honestly rather than hides: a
// receivable is kept per party, and splitting one across co-tenants is a
// decision nobody has made yet.
//
// Both are read **as at a date** rather than as at today, and that is not a
// nicety: a tenancy starting next month has no party today, so a billing run
// preparing its first invoice would find nobody and refuse a lease that is
// perfectly well formed. Ownership is effective-dated for the same reason in the
// other direction — the owner who was paid last March is the one who owned it
// then, not whoever owns it now.
func (s *Leases) Parties(ctx context.Context, id string, on effective.Date) (tenant, owner string, err error) {
	err = tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			SELECT party_id::text
			  FROM lease_parties
			 WHERE lease_id = $1 AND role = 'tenant' AND retired_at IS NULL
			   AND validity @> $2::date
			 ORDER BY valid_from
			 LIMIT 1`, id, on.Time()).Scan(&tenant); err != nil {
			return fmt.Errorf("no tenant on lease %s: %w", id, err)
		}
		err := tx.QueryRow(ctx, `
			SELECT o.owner_party_id::text
			  FROM property_ownership o
			  JOIN leases l ON l.unit_id = o.unit_id OR (o.unit_id IS NULL AND l.property_id = o.property_id)
			 WHERE l.id = $1 AND o.retired_at IS NULL AND o.validity @> l.valid_from
			 ORDER BY o.unit_id NULLS LAST, o.share_bps DESC
			 LIMIT 1`, id).Scan(&owner)
		if errors.Is(err, pgx.ErrNoRows) {
			// A tenancy on a unit whose ownership nobody has recorded. Refused
			// rather than defaulted: an invoice credits rent income to an owner,
			// and inventing one puts somebody else's money on a statement.
			return fmt.Errorf("%w: lease %s has no recorded owner, so there is nobody to credit "+
				"the rent to", ErrNoLease, id)
		}
		return err
	})
	return tenant, owner, err
}

// Billable returns the tenancies a billing run should consider.
//
// Active and in-notice only: notice announces an ending and does not perform it,
// so a tenancy under notice still owes rent — stopping the invoices when notice
// is served is a month of rent the owner never sees and the tenant never
// disputes (ADR-0010 §4).
//
// Ordered by id so a run that is interrupted and restarted covers the same
// leases in the same order, which makes a partial run's effect describable.
func (s *Leases) Billable(ctx context.Context, limit int) ([]domain.Lease, error) {
	if limit <= 0 {
		limit = 500
	}
	var out []domain.Lease
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text, tenant_id::text, property_id::text, unit_id::text, state,
			       valid_from, valid_to, ended_on, notice_days
			  FROM leases
			 WHERE state IN ('active', 'in_notice')
			 ORDER BY id
			 LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var (
				l         domain.Lease
				state     string
				from      time.Time
				to, ended *time.Time
			)
			if err := rows.Scan(&l.ID, &l.TenantID, &l.Property, &l.Unit, &state,
				&from, &to, &ended, &l.NoticeDays); err != nil {
				return err
			}
			l.State = domain.State(state)
			if l.Term, err = interval(from, to); err != nil {
				return fmt.Errorf("lease %s: %w", l.ID, err)
			}
			if ended != nil {
				l.EndedOn = effective.Day(ended.Year(), ended.Month(), ended.Day())
			}
			out = append(out, l)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, fmt.Errorf("lease: reading billable tenancies: %w", err)
	}
	return out, nil
}

// interval rebuilds the agreed term from its two columns.
func interval(from time.Time, to *time.Time) (effective.Interval, error) {
	f := effective.Day(from.Year(), from.Month(), from.Day())
	if to == nil {
		return effective.Since(f)
	}
	return effective.Between(f, effective.Day(to.Year(), to.Month(), to.Day()))
}

// Expiring is a tenancy running out, as the renewal reminder reads it.
type Expiring struct {
	LeaseID       string
	PropertyID    string
	UnitID        string
	State         string
	EndsOn        effective.Date
	DaysRemaining int
	NoticeDays    int
	// InsideNoticeWindow is whether notice can still be given in time — the thing
	// an owner actually needs to know, and one subtraction away from being got
	// wrong in a dashboard.
	InsideNoticeWindow bool
}

// Expiring lists tenancies ending within `within` days.
//
// It reads the lease_expiring view rather than repeating its arithmetic, which is
// why ADR-0010 §6 made it a view: the renewal reminder and the owner's dashboard
// read one definition, and a second copy here would be the one that drifts.
func (s *Leases) Expiring(ctx context.Context, within int) ([]Expiring, error) {
	if within <= 0 {
		within = 60
	}
	var out []Expiring
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT lease_id::text, property_id::text, unit_id::text, state, ends_on,
			       days_remaining, notice_days, inside_notice_window
			  FROM lease_expiring
			 WHERE days_remaining <= $1
			 ORDER BY ends_on`, within)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var e Expiring
			var ends time.Time
			if err := rows.Scan(&e.LeaseID, &e.PropertyID, &e.UnitID, &e.State, &ends,
				&e.DaysRemaining, &e.NoticeDays, &e.InsideNoticeWindow); err != nil {
				return err
			}
			e.EndsOn = effective.DateOf(ends, ends.Location())
			out = append(out, e)
		}
		return rows.Err()
	})
	return out, err
}
