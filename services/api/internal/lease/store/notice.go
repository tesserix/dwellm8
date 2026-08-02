package store

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/lease/domain"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// ServeNotice records notice served on a live tenancy. #239.
//
// Same shape as Terminate: the lease is read inside the transaction so the
// state the domain judges is the state written over, and the fact publishes on
// the same commit.
func (s *Leases) ServeNotice(ctx context.Context, id string, n domain.Notice) (domain.Lease, error) {
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

		noticed, event, err := l.ServeNotice(n)
		if err != nil {
			return err
		}

		tag, err := tx.Exec(ctx, `
			UPDATE leases
			   SET state = 'in_notice', notice_served_on = $3::date,
			       notice_served_by = $4, notice_move_out_on = $5::date,
			       updated_at = now()
			 WHERE id = $1 AND tenant_id = $2 AND state = 'active'`,
			id, tenant.String(), n.ServedOn.Time(), string(n.By), n.MoveOutOn.Time())
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNoLease
		}

		// The event actor is "who pressed the button" in the outbox's terms;
		// which side of the tenancy they are is in the payload's served_by.
		env, err := events.New(string(event), tenant.String(),
			events.Subject{Kind: "lease", ID: id},
			events.Actor{Kind: events.ActorUser},
			struct {
				UnitID    string `json:"unit_id"`
				ServedOn  string `json:"served_on"`
				MoveOutOn string `json:"move_out_on"`
				ServedBy  string `json:"served_by"`
			}{UnitID: noticed.Unit, ServedOn: n.ServedOn.String(),
				MoveOutOn: n.MoveOutOn.String(), ServedBy: string(n.By)})
		if err != nil {
			return err
		}
		if err := events.Append(ctx, tx, env); err != nil {
			return err
		}
		out = noticed
		return nil
	})
	return out, err
}
