package store

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/maintenance/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Tickets is the maintenance module's ticket store.
type Tickets struct{ pool tenancy.Pool }

// NewTickets takes the request pool: a ticket is always somebody's.
func NewTickets(p tenancy.Pool) *Tickets { return &Tickets{pool: p} }

// ErrNoTicket is no such ticket — including one hidden by policy, deliberately
// the same answer.
var ErrNoTicket = errors.New("ticket: no such ticket")

const ticketColumns = `
	id::text, tenant_id::text, lease_id::text, property_id::text, unit_id::text,
	raised_by_party_id::text, category, title, body, status,
	coalesce(liability, ''), coalesce(liability_reason, ''),
	coalesce(slot, ''), coalesce(vendor, ''), cost_minor, created_at, updated_at`

func scanTicket(row pgx.Row) (domain.Ticket, error) {
	var t domain.Ticket
	var category, status string
	err := row.Scan(&t.ID, &t.TenantID, &t.LeaseID, &t.PropertyID, &t.UnitID,
		&t.RaisedBy, &category, &t.Title, &t.Body, &status,
		&t.Liability, &t.LiabilityReason, &t.Slot, &t.Vendor, &t.CostMinor,
		&t.CreatedAt, &t.UpdatedAt)
	t.Category, t.Status = domain.TicketCategory(category), domain.TicketStatus(status)
	return t, err
}

// Raise writes the ticket and its first timeline line in one transaction.
func (s *Tickets) Raise(ctx context.Context, t domain.Ticket, opening string) (domain.Ticket, error) {
	var out domain.Ticket
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			INSERT INTO tickets (tenant_id, lease_id, property_id, unit_id,
			                     raised_by_party_id, category, title, body)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING `+ticketColumns,
			t.TenantID, t.LeaseID, t.PropertyID, t.UnitID,
			t.RaisedBy, string(t.Category), t.Title, t.Body)
		var err error
		if out, err = scanTicket(row); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO ticket_events (tenant_id, ticket_id, actor, body)
			VALUES ($1, $2, 'resident', $3)`, out.TenantID, out.ID, opening)
		return err
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return out, nil
}

// ForLease lists a tenancy's tickets, newest first, without timelines.
func (s *Tickets) ForLease(ctx context.Context, leaseID string, limit int) ([]domain.Ticket, error) {
	var out []domain.Ticket
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+ticketColumns+`
			  FROM tickets
			 WHERE lease_id = $1
			 ORDER BY created_at DESC
			 LIMIT $2`, leaseID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			t, err := scanTicket(rows)
			if err != nil {
				return err
			}
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// ForOrg lists the organisation's tickets, open or settled, newest first,
// labelled with the unit and property the screen sorts by.
func (s *Tickets) ForOrg(ctx context.Context, settled bool, limit int) ([]domain.Ticket, error) {
	cond := "t.status NOT IN ('resolved', 'cancelled')"
	if settled {
		cond = "t.status IN ('resolved', 'cancelled')"
	}
	var out []domain.Ticket
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT t.id::text, t.tenant_id::text, t.lease_id::text, t.property_id::text,
			       t.unit_id::text, t.raised_by_party_id::text, t.category, t.title,
			       t.body, t.status, coalesce(t.liability, ''),
			       coalesce(t.liability_reason, ''), coalesce(t.slot, ''),
			       coalesce(t.vendor, ''), t.cost_minor, t.created_at, t.updated_at,
			       u.code::text, p.name
			  FROM tickets t
			  JOIN units u      ON u.id = t.unit_id     AND u.tenant_id = t.tenant_id
			  JOIN properties p ON p.id = t.property_id AND p.tenant_id = t.tenant_id
			 WHERE `+cond+`
			 ORDER BY t.created_at DESC
			 LIMIT $1`, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var t domain.Ticket
			var category, status string
			if err := rows.Scan(&t.ID, &t.TenantID, &t.LeaseID, &t.PropertyID, &t.UnitID,
				&t.RaisedBy, &category, &t.Title, &t.Body, &status,
				&t.Liability, &t.LiabilityReason, &t.Slot, &t.Vendor, &t.CostMinor,
				&t.CreatedAt, &t.UpdatedAt, &t.UnitCode, &t.PropertyName); err != nil {
				return err
			}
			t.Category, t.Status = domain.TicketCategory(category), domain.TicketStatus(status)
			out = append(out, t)
		}
		return rows.Err()
	})
	return out, err
}

// Apply writes an advanced ticket and its timeline line in one transaction.
func (s *Tickets) Apply(ctx context.Context, t domain.Ticket, line string) (domain.Ticket, error) {
	var out domain.Ticket
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		row := tx.QueryRow(ctx, `
			UPDATE tickets
			   SET status = $2, liability = $3, liability_reason = $4,
			       slot = $5, vendor = $6, cost_minor = $7, updated_at = now()
			 WHERE id = $1
			RETURNING `+ticketColumns,
			t.ID, string(t.Status), nullable(t.Liability), nullable(t.LiabilityReason),
			nullable(t.Slot), nullable(t.Vendor), t.CostMinor)
		var err error
		if out, err = scanTicket(row); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNoTicket
			}
			return err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO ticket_events (tenant_id, ticket_id, actor, body)
			VALUES ($1, $2, 'manager', $3)`, out.TenantID, out.ID, line)
		return err
	})
	if err != nil {
		return domain.Ticket{}, err
	}
	return out, nil
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Read returns one ticket with its timeline, oldest line first.
func (s *Tickets) Read(ctx context.Context, id string) (domain.Ticket, error) {
	var out domain.Ticket
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		t, err := scanTicket(tx.QueryRow(ctx, `
			SELECT `+ticketColumns+` FROM tickets WHERE id = $1`, id))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoTicket
		}
		if err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `
			SELECT at, actor, body FROM ticket_events
			 WHERE ticket_id = $1 ORDER BY at, id`, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e domain.TicketEvent
			if err := rows.Scan(&e.At, &e.Actor, &e.Body); err != nil {
				return err
			}
			t.Events = append(t.Events, e)
		}
		out = t
		return rows.Err()
	})
	return out, err
}
