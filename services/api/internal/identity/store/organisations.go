package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Organisations lists the platform's tenants, for the jobs that span it.
//
// The privileged pool, and it is the narrowest possible use of it: a billing run
// has to know who to bill for, and that question has no tenant to be asked
// inside. Everything the run then *does* happens in one organisation's session,
// so this returns ids and nothing else — an enumeration, not a way to read
// across the boundary.
type Organisations struct{ platform tenancy.PlatformPool }

// NewOrganisations takes the platform pool.
func NewOrganisations(p tenancy.PlatformPool) *Organisations {
	return &Organisations{platform: p}
}

// Active returns the organisations a periodic job should act for.
//
// Suspended and closed ones are excluded: billing an organisation that has been
// suspended produces invoices nobody will collect and a statement somebody has
// to explain. Onboarding ones are included — an organisation mid-onboarding may
// already have a signed tenancy, and its first rent is the worst one to miss.
//
// Sandboxes are included too, deliberately. A demo that does not generate its
// own invoices is a demo that shows an empty screen (ADR-0021), and nothing in
// one can cause a side effect.
func (o *Organisations) Active(ctx context.Context) ([]tenancy.ID, error) {
	var out []tenancy.ID
	err := tenancy.Platform(ctx, o.platform, "listing organisations for a periodic job",
		func(ctx context.Context, tx pgx.Tx) error {
			rows, err := tx.Query(ctx, `
				SELECT id::text FROM organisations
				 WHERE state IN ('onboarding', 'active')
				 ORDER BY created_at, id`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					return err
				}
				out = append(out, tenancy.ID(id))
			}
			return rows.Err()
		})
	if err != nil {
		return nil, fmt.Errorf("identity: listing organisations: %w", err)
	}
	return out, nil
}

// ErrNoOrganisation is no organisation with that id.
var ErrNoOrganisation = errors.New("identity: no such organisation")

// Name returns an organisation's display name and kind ("agency", "owner",
// "society", ...), for a caller that only has its id from a foreign key on
// another module's row — a property's tenant_id, say.
//
// The platform pool, and for the same reason as Active: organisations_tenant_
// isolation only lets a session read its own row, so a request-scoped read of
// someone else's organisation — the agency managing a delegated property, for
// instance — has nothing to select from.
func (o *Organisations) Name(ctx context.Context, id tenancy.ID) (name, kind string, err error) {
	err = tenancy.Platform(ctx, o.platform, "resolving an organisation's display name",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT name, kind FROM organisations WHERE id = $1::uuid`, id.String(),
			).Scan(&name, &kind)
		})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return "", "", ErrNoOrganisation
	case err != nil:
		return "", "", fmt.Errorf("identity: resolving organisation %s: %w", id, err)
	}
	return name, kind, nil
}
