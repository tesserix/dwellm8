// Package store is the community module's SQL — lease_messages and
// gate_passes, and nothing of any other module's.
package store

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/community/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Community is the module's store.
type Community struct{ pool tenancy.Pool }

// New takes the request pool: a message is always somebody's.
func New(p tenancy.Pool) *Community { return &Community{pool: p} }

// ErrNoPass is no such pass — including one hidden by policy, deliberately the
// same answer.
var ErrNoPass = errors.New("community: no such gate pass")

const messageColumns = `
	id::text, tenant_id::text, lease_id::text, property_id::text, unit_id::text,
	sender, coalesce(sender_party_id::text, ''), body, created_at`

func scanMessage(row pgx.Row) (domain.Message, error) {
	var m domain.Message
	err := row.Scan(&m.ID, &m.TenantID, &m.LeaseID, &m.PropertyID, &m.UnitID,
		&m.Sender, &m.SenderPartyID, &m.Body, &m.CreatedAt)
	return m, err
}

// Send appends one message to the lease's thread.
func (s *Community) Send(ctx context.Context, m domain.Message) (domain.Message, error) {
	var out domain.Message
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanMessage(tx.QueryRow(ctx, `
			INSERT INTO lease_messages (tenant_id, lease_id, property_id, unit_id,
			                            sender, sender_party_id, body)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
			RETURNING `+messageColumns,
			m.TenantID, m.LeaseID, m.PropertyID, m.UnitID,
			m.Sender, nullable(m.SenderPartyID), m.Body))
		return err
	})
	return out, err
}

// Thread reads a lease's conversation, oldest first.
func (s *Community) Thread(ctx context.Context, leaseID string, limit int) ([]domain.Message, error) {
	var out []domain.Message
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+messageColumns+`
			  FROM lease_messages
			 WHERE lease_id = $1
			 ORDER BY created_at DESC, id DESC
			 LIMIT $2`, leaseID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			m, err := scanMessage(rows)
			if err != nil {
				return err
			}
			out = append(out, m)
		}
		return rows.Err()
	})
	// Newest LIMIT rows, presented oldest first — the shape a chat renders.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, err
}

const passColumns = `
	id::text, tenant_id::text, lease_id::text, property_id::text, unit_id::text,
	created_by_party_id::text, name, kind, code, valid_from, valid_to,
	state, created_at, updated_at`

func scanPass(row pgx.Row) (domain.Pass, error) {
	var p domain.Pass
	var kind string
	var validTo *time.Time
	err := row.Scan(&p.ID, &p.TenantID, &p.LeaseID, &p.PropertyID, &p.UnitID,
		&p.CreatedBy, &p.Name, &kind, &p.Code, &p.ValidFrom, &validTo,
		&p.State, &p.CreatedAt, &p.UpdatedAt)
	p.Kind = domain.PassKind(kind)
	if validTo != nil {
		p.ValidTo = *validTo
	}
	return p, err
}

// gateCode is four digits from a CSPRNG — a code, not a secret, but there is
// no reason for it to be guessable either.
func gateCode() (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%04d", (int(b[0])<<8|int(b[1]))%10000), nil
}

// CreatePass writes one expected visitor and mints their code.
func (s *Community) CreatePass(ctx context.Context, p domain.Pass) (domain.Pass, error) {
	code, err := gateCode()
	if err != nil {
		return domain.Pass{}, err
	}
	var out domain.Pass
	err = tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanPass(tx.QueryRow(ctx, `
			INSERT INTO gate_passes (tenant_id, lease_id, property_id, unit_id,
			                         created_by_party_id, name, kind, code, valid_to)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING `+passColumns,
			p.TenantID, p.LeaseID, p.PropertyID, p.UnitID,
			p.CreatedBy, p.Name, string(p.Kind), code, nullTime(p.ValidTo)))
		return err
	})
	return out, err
}

// PassesForLease lists a tenancy's passes, newest first.
func (s *Community) PassesForLease(ctx context.Context, leaseID string, limit int) ([]domain.Pass, error) {
	var out []domain.Pass
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT `+passColumns+`
			  FROM gate_passes
			 WHERE lease_id = $1
			 ORDER BY created_at DESC
			 LIMIT $2`, leaseID, limit)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			p, err := scanPass(rows)
			if err != nil {
				return err
			}
			out = append(out, p)
		}
		return rows.Err()
	})
	return out, err
}

// CancelPass withdraws an expected visitor. Only an expected pass can be
// cancelled — a visit that already happened is history, not an instruction —
// and the lease is part of the predicate so a URL cannot reach across
// tenancies before the check.
func (s *Community) CancelPass(ctx context.Context, leaseID, id string) (domain.Pass, error) {
	var out domain.Pass
	err := tenancy.Scoped(ctx, s.pool, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		out, err = scanPass(tx.QueryRow(ctx, `
			UPDATE gate_passes
			   SET state = 'cancelled', updated_at = now()
			 WHERE id = $1 AND lease_id = $2 AND state = 'expected'
			RETURNING `+passColumns, id, leaseID))
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoPass
		}
		return err
	})
	return out, err
}

func nullable(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t
}
