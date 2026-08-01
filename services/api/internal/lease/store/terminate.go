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
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ErrChecklistOutstanding is a tenancy the database refused to close because its
// move-out is unfinished. ADR-0032 §4.
//
// It should be unreachable: the service asks the maintenance module first and
// refuses with the whole list. It is here because the trigger is the backstop for
// the paths that do not go through the service, and an unrecognised
// check_violation surfacing as a 500 would hide the one refusal a person can act on.
var ErrChecklistOutstanding = errors.New("lease: the move-out checklist is not finished")

// Terminate ends a tenancy.
//
// The agreement is left exactly as signed and `ended_on` records that occupancy
// ceased — ADR-0010's central distinction, enforced here by writing only the columns
// a termination owns. The domain decides whether the move is legal and whether the
// retrospective decision is required; this writes the result and publishes the fact
// on the same commit.
// billedThrough comes from the money module's service seam rather than from a query
// here: journal_entries is money's table, and ADR-0001 §3 makes that a call rather
// than a join. Zero is a supported answer — a tenancy that never invoiced — and the
// schema's own trigger asks the ledger the same question for anything that did not
// come through here.
func (s *Leases) Terminate(ctx context.Context, id string, t domain.Termination,
	billedThrough effective.Date) (domain.Lease, error) {
	tenant, ok := tenancy.From(ctx)
	if !ok {
		return domain.Lease{}, tenancy.ErrNoTenant
	}

	var out domain.Lease
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		l, err := readForTermination(ctx, tx, id)
		if err != nil {
			return err
		}

		ended, _, err := l.Terminate(t, billedThrough)
		if err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE leases
			   SET state = 'terminated', ended_on = $3::date, terminated_by = $4,
			       terminated_reason = $5, settlement_decision = $6
			 WHERE id = $1 AND tenant_id = $2 AND state IN ('active', 'in_notice')`,
			id, tenant.String(), t.EffectiveOn.Time(), string(t.By), t.Reason, string(t.Decision))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoLease
		}

		env, err := endedEvent(ctx, tx, tenant.String(), ended)
		if err != nil {
			return err
		}
		if err := events.Append(ctx, tx, env); err != nil {
			return err
		}
		out = ended
		return nil
	})

	if isChecklistRefusal(err) {
		return domain.Lease{}, fmt.Errorf("%w: %s", ErrChecklistOutstanding, checkMessage(err))
	}
	return out, err
}

// readForTermination reads the lease as it stands inside the terminating
// transaction, so the state the domain judges is the state being written over.
func readForTermination(ctx context.Context, tx pgx.Tx, id string) (domain.Lease, error) {
	var l domain.Lease
	var state string
	var from time.Time
	var to, ended *time.Time
	err := tx.QueryRow(ctx, `
		SELECT id::text, tenant_id::text, property_id::text, unit_id::text, state,
		       valid_from, valid_to, ended_on, notice_days
		  FROM leases WHERE id = $1`, id).
		Scan(&l.ID, &l.TenantID, &l.Property, &l.Unit, &state, &from, &to, &ended, &l.NoticeDays)
	if errors.Is(err, pgx.ErrNoRows) {
		return l, ErrNoLease
	}
	if err != nil {
		return l, err
	}
	l.State = domain.State(state)
	term, err := interval(from, to)
	if err != nil {
		return l, err
	}
	l.Term = term
	if ended != nil {
		l.EndedOn = effective.DateOf(*ended, ended.Location())
	}
	return l, nil
}

// endedEvent builds lease.tenancy.ended with the unit and the parties, which the
// authz projector uses to remove the edges the start wrote.
func endedEvent(ctx context.Context, tx pgx.Tx, tenant string, l domain.Lease) (events.Envelope, error) {
	rows, err := tx.Query(ctx, `SELECT party_id, role FROM lease_parties WHERE lease_id = $1`, l.ID)
	if err != nil {
		return events.Envelope{}, fmt.Errorf("reading the parties for the event: %w", err)
	}
	defer rows.Close()

	type party struct {
		PartyID string `json:"party_id"`
		Role    string `json:"role"`
	}
	data := struct {
		UnitID  string  `json:"unit_id"`
		EndedOn string  `json:"ended_on"`
		Parties []party `json:"parties"`
	}{UnitID: l.Unit, EndedOn: l.EndedOn.String()}
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

	return events.New(string(domain.EventEnded), tenant,
		events.Subject{Kind: "lease", ID: l.ID},
		events.Actor{Kind: events.ActorSystem}, data)
}

func isChecklistRefusal(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	// Matched on the message rather than a constraint name because it is raised by a
	// trigger, which has none. The alternative — treating every check_violation from
	// an UPDATE of leases as this one — would report the tax-path refusal and the
	// retrospective-decision refusal as an unfinished checklist.
	return pgErr.Code == "23514" &&
		strings.Contains(pgErr.Message, "cannot be closed while its move-out is outstanding")
}

func checkMessage(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Message
	}
	return err.Error()
}
