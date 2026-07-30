package isolationtest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The delegation contract from ADR-0005.
//
// Run() asserts that an organisation cannot reach another organisation's rows.
// This file asserts the one exception: that it can, through a grant, exactly as
// far as the grant goes and not one row further — and that the moment the grant
// ends, so does the reach.
//
// Two entry points, because the properties divide cleanly:
//
//	RunGrantModel  — the grant object itself. Table-independent, run once.
//	RunDelegated   — one delegable table. Every module with one calls it.

// Orgs used by the delegation contract. OrgA and OrgB come from Run()'s
// contract; the grantor and grantee are separate so that a delegation test and
// an isolation test can share a database without interfering.
const (
	// OrgOwner grants; OrgFirm receives; OrgOutsider holds a grant from the
	// same owner and must gain nothing from the fact.
	OrgOwner    = tenancy.ID("33333333-3333-3333-3333-333333333333")
	OrgFirm     = tenancy.ID("44444444-4444-4444-4444-444444444444")
	OrgOutsider = tenancy.ID("55555555-5555-5555-5555-555555555555")
)

// Grant describes a grant to seed. Seeding is a platform-session act because a
// test is not a request, and because the grantor's own session is exactly what
// the contract must not require to be running.
type Grant struct {
	Grantor     tenancy.ID
	Grantee     tenancy.ID
	Permissions []string
	// Properties the grant covers, and Units for the finer mandate ADR-0009
	// made enforceable. Both empty means the whole portfolio.
	Properties []string
	Units      []string
}

// SeedGrant writes a grant and returns its id.
//
// It seeds the fixtures first. Since ADR-0009 a scope row is validated against
// the grantor's own tree, so a grant cannot be written before the properties and
// units it names exist — and a test that ran alone used to depend on another
// test having seeded them.
func SeedGrant(t *testing.T, p tenancy.PlatformPool, g Grant) tenancy.GrantID {
	t.Helper()
	ctx := context.Background()
	seedDelegationFixtures(t, p)

	var id tenancy.GrantID
	err := tenancy.Platform(ctx, p, "seeding the delegation contract", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO delegation_grants (tenant_id, grantee_org_id, permissions)
			VALUES ($1, $2, $3) RETURNING id`,
			g.Grantor.String(), g.Grantee.String(), g.Permissions).Scan(&id); err != nil {
			return fmt.Errorf("inserting the grant: %w", err)
		}
		if len(g.Properties) == 0 && len(g.Units) == 0 {
			_, err := tx.Exec(ctx, `
				INSERT INTO delegation_grant_scopes (grant_id, tenant_id, scope_kind, scope_id)
				VALUES ($1, $2, 'portfolio', NULL)`, id.String(), g.Grantor.String())
			return err
		}
		for kind, ids := range map[string][]string{"property": g.Properties, "unit": g.Units} {
			for _, target := range ids {
				if _, err := tx.Exec(ctx, `
					INSERT INTO delegation_grant_scopes (grant_id, tenant_id, scope_kind, scope_id)
					VALUES ($1, $2, $3, $4)`, id.String(), g.Grantor.String(), kind, target); err != nil {
					return fmt.Errorf("inserting a %s scope: %w", kind, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding a grant from %s to %s: %v", g.Grantor, g.Grantee, err)
	}
	return id
}

// revoke ends a grant the way the owner would: an update, never a delete.
func revoke(t *testing.T, p tenancy.PlatformPool, id tenancy.GrantID) {
	t.Helper()
	err := tenancy.Platform(context.Background(), p, "revoking in the delegation contract",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE delegation_grants SET revoked_at = now(), revoked_reason = $2 WHERE id = $1`,
				id.String(), "the delegation contract")
			return err
		})
	if err != nil {
		t.Fatalf("revoking grant %s: %v", id, err)
	}
}

// delegated asks the database the question a policy asks: does this grant reach
// this row, for this permission, now?
func delegated(t *testing.T, p tenancy.Pool, ctx context.Context, owner tenancy.ID, property, permission string) bool {
	t.Helper()
	var out bool
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		var prop any
		if property != "" {
			prop = property
		}
		return tx.QueryRow(ctx, `SELECT is_delegated($1, $2, $3)`, owner.String(), prop, permission).Scan(&out)
	})
	if err != nil {
		t.Fatalf("is_delegated(%s, %s, %s): %v", owner, property, permission, err)
	}
	return out
}

// RunGrantModel asserts the properties of the grant object itself: what it
// reaches, what it refuses, and what happens when it ends.
//
// It needs the platform pool because creating a grant for an organisation the
// test is not acting as is, correctly, not something a scoped session can do.
func RunGrantModel(t *testing.T, p tenancy.Pool, plat tenancy.PlatformPool) {
	t.Helper()
	ctx := context.Background()

	seedDelegationFixtures(t, plat)
	grant := SeedGrant(t, plat, Grant{
		Grantor:     OrgOwner,
		Grantee:     OrgFirm,
		Permissions: []string{"property.read", "money.read", "money.collect"},
		Properties:  GrantedProperties,
	})
	// The same owner, a different firm, the whole portfolio. Nothing about its
	// existence may help OrgFirm.
	outsiderGrant := SeedGrant(t, plat, Grant{
		Grantor:     OrgOwner,
		Grantee:     OrgOutsider,
		Permissions: []string{"property.read"},
	})

	firm := tenancy.WithGrant(tenancy.With(ctx, OrgFirm), grant)

	t.Run("a grant reaches the properties it names", func(t *testing.T) {
		for _, prop := range GrantedProperties {
			if !delegated(t, p, firm, OrgOwner, prop, "property.read") {
				t.Fatalf("the grant does not reach %s, which it names — a firm cannot see the units it manages", prop)
			}
		}
	})

	t.Run("and no further", func(t *testing.T) {
		for _, prop := range UngrantedProperties {
			if delegated(t, p, firm, OrgOwner, prop, "property.read") {
				t.Fatalf("the grant reaches %s, which it does not name — a two-unit mandate has become a portfolio", prop)
			}
		}
	})

	t.Run("a permission the grant does not carry is refused", func(t *testing.T) {
		if delegated(t, p, firm, OrgOwner, GrantedProperties[0], "property.write") {
			t.Fatal("a read grant conferred write — the permission set is decorative")
		}
		if delegated(t, p, firm, OrgOwner, GrantedProperties[0], "money.payout") {
			t.Fatal("a grant of money.collect conferred money.payout — a firm could move the owner's money out")
		}
	})

	t.Run("quoting another organisation's grant confers nothing", func(t *testing.T) {
		borrowed := tenancy.WithGrant(tenancy.With(ctx, OrgFirm), outsiderGrant)
		if delegated(t, p, borrowed, OrgOwner, GrantedProperties[0], "property.read") {
			t.Fatal("a grant issued to another organisation worked when quoted by this one — " +
				"app.grant_id is being trusted rather than checked")
		}
	})

	t.Run("no grant declared, no reach", func(t *testing.T) {
		bare := tenancy.With(ctx, OrgFirm)
		if delegated(t, p, bare, OrgOwner, GrantedProperties[0], "property.read") {
			t.Fatal("a session that declared no grant still reached across organisations")
		}
	})

	t.Run("the grantee cannot widen, revoke or re-delegate", func(t *testing.T) {
		err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx,
				`UPDATE delegation_grants SET permissions = ARRAY['property.write'] WHERE id = $1`, grant.String())
			return err
		})
		if err == nil {
			t.Fatal("the grantee rewrote its own permissions — WITH CHECK is not naming the grantor")
		}

		err = tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO delegation_grants (tenant_id, grantee_org_id, permissions)
				VALUES ($1, $2, ARRAY['property.read'])`, OrgFirm.String(), OrgOutsider.String())
			return err
		})
		if err == nil {
			t.Fatal("a session acting under a grant created a grant — a firm can pass the owner's units on")
		}
	})

	t.Run("the grant cannot be deleted, by either party", func(t *testing.T) {
		// A revoked grant is the evidence of what was permitted, and when.
		for _, actor := range []context.Context{firm, tenancy.With(ctx, OrgOwner)} {
			err := tenancy.Scoped(actor, p, func(ctx context.Context, tx pgx.Tx) error {
				tag, err := tx.Exec(ctx, `DELETE FROM delegation_grants WHERE id = $1`, grant.String())
				if err != nil {
					return err // the privilege is withheld: also a pass
				}
				if tag.RowsAffected() != 0 {
					return fmt.Errorf("deleted %d grant rows", tag.RowsAffected())
				}
				return nil
			})
			if err != nil && !isPermissionDenied(err) {
				t.Fatalf("deleting a grant: %v", err)
			}
		}
		var n int
		err := tenancy.Scoped(tenancy.With(ctx, OrgOwner), p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM delegation_grants WHERE id = $1`, grant.String()).Scan(&n)
		})
		if err != nil {
			t.Fatalf("counting the grant: %v", err)
		}
		if n != 1 {
			t.Fatal("the grant row is gone — a revoked grant must survive as the record of what was allowed")
		}
	})

	t.Run("revocation ends the reach, and keeps the record", func(t *testing.T) {
		revoke(t, plat, grant)

		for _, prop := range GrantedProperties {
			if delegated(t, p, firm, OrgOwner, prop, "property.read") {
				t.Fatalf("the firm still reaches %s after revocation", prop)
			}
		}

		// The firm keeps sight of the mandate it used to hold — it has its own
		// reason to know what it was permitted to do last month.
		var n int
		err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM delegation_grants WHERE id = $1 AND revoked_at IS NOT NULL`,
				grant.String()).Scan(&n)
		})
		if err != nil {
			t.Fatalf("reading the revoked grant as the grantee: %v", err)
		}
		if n != 1 {
			t.Fatal("the grantee cannot see the grant it used to hold — its own audit becomes unexplainable")
		}
	})
}

// DelegatedTable describes one table that a grant can reach into.
//
// InsertAsGrantor writes a row the owner owns. CountVisible reports how many of
// this run's rows the current session can see, whatever session that is — the
// contract runs it as the owner, as the firm under a grant, and as the firm
// after revocation.
type DelegatedTable struct {
	Name            string
	InsertAsGrantor func(ctx context.Context, tx pgx.Tx, owner tenancy.ID, grant tenancy.GrantID, actor tenancy.ID, token string) error
	CountVisible    func(ctx context.Context, tx pgx.Tx, token string) (int, error)
}

// RunDelegated asserts, for one table, that a grant is a window and revocation
// closes it.
func RunDelegated(t *testing.T, p tenancy.Pool, plat tenancy.PlatformPool, tbl DelegatedTable) {
	t.Helper()
	ctx := context.Background()
	tok := token(t)

	seedDelegationFixtures(t, plat)
	grant := SeedGrant(t, plat, Grant{
		Grantor:     OrgOwner,
		Grantee:     OrgFirm,
		Permissions: []string{"property.read", "money.read"},
		Properties:  GrantedProperties,
	})
	firm := tenancy.WithGrant(tenancy.With(ctx, OrgFirm), grant)

	t.Run(tbl.Name+"/a delegated session writes into the grantor, stamped", func(t *testing.T) {
		err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
			return tbl.InsertAsGrantor(ctx, tx, OrgOwner, grant, OrgFirm, tok)
		})
		if err != nil {
			t.Fatalf("a session under a valid grant could not write to %s in the grantor's tenant: %v", tbl.Name, err)
		}
	})

	t.Run(tbl.Name+"/the same write without a grant is refused", func(t *testing.T) {
		bare := tenancy.With(ctx, OrgFirm)
		err := tenancy.Scoped(bare, p, func(ctx context.Context, tx pgx.Tx) error {
			return tbl.InsertAsGrantor(ctx, tx, OrgOwner, grant, OrgFirm, tok)
		})
		if err == nil {
			t.Fatalf("a session with no grant wrote into another organisation's %s — "+
				"the delegated branch of the policy is not checking the grant", tbl.Name)
		}
	})

	t.Run(tbl.Name+"/the grantor sees the row", func(t *testing.T) {
		if n := countAs(t, p, tbl, tenancy.With(ctx, OrgOwner), tok); n != 1 {
			t.Fatalf("the owner sees %d rows in %s, want 1 — an access made under a grant "+
				"must appear in the owner's own record", n, tbl.Name)
		}
	})

	t.Run(tbl.Name+"/the grantee sees it while the grant lives", func(t *testing.T) {
		if n := countAs(t, p, tbl, firm, tok); n != 1 {
			t.Fatalf("the firm sees %d rows in %s under a live grant, want 1", n, tbl.Name)
		}
	})

	t.Run(tbl.Name+"/an unrelated organisation never sees it", func(t *testing.T) {
		if n := countAs(t, p, tbl, tenancy.With(ctx, OrgOutsider), tok); n != 0 {
			t.Fatalf("an organisation with no grant sees %d rows in %s", n, tbl.Name)
		}
	})

	t.Run(tbl.Name+"/revocation closes the window, the owner keeps the history", func(t *testing.T) {
		revoke(t, plat, grant)

		if n := countAs(t, p, tbl, firm, tok); n != 0 {
			t.Fatalf("the firm still sees %d rows in %s after revocation — access outlived the grant", n, tbl.Name)
		}
		if n := countAs(t, p, tbl, tenancy.With(ctx, OrgOwner), tok); n != 1 {
			t.Fatalf("the owner sees %d rows in %s after revoking, want 1 — revocation destroyed history", n, tbl.Name)
		}
		err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
			return tbl.InsertAsGrantor(ctx, tx, OrgOwner, grant, OrgFirm, tok)
		})
		if err == nil {
			t.Fatalf("the firm wrote to %s under a revoked grant", tbl.Name)
		}
	})
}

func countAs(t *testing.T, p tenancy.Pool, tbl DelegatedTable, as context.Context, tok string) int {
	t.Helper()
	var n int
	err := tenancy.Scoped(as, p, func(ctx context.Context, tx pgx.Tx) error {
		var err error
		n, err = tbl.CountVisible(ctx, tx, tok)
		return err
	})
	if err != nil {
		t.Fatalf("counting %s: %v", tbl.Name, err)
	}
	return n
}

// seedDelegationFixtures is the organisations and the owner's tree. Both are
// needed before a grant can be written: the organisations because the grant
// references them, the tree because ADR-0009 validates every scope row against
// it.
func seedDelegationFixtures(t *testing.T, p tenancy.PlatformPool) {
	t.Helper()
	seedDelegationOrgs(t, p)
	SeedPropertyTree(t, p)
}

func seedDelegationOrgs(t *testing.T, p tenancy.PlatformPool) {
	t.Helper()
	err := tenancy.Platform(context.Background(), p, "seeding the delegation contract",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO organisations (id, slug, name, kind) VALUES
				  ($1, 'harness-owner',    'Harness Owner',    'owner'),
				  ($2, 'harness-firm',     'Harness Firm',     'agency'),
				  ($3, 'harness-outsider', 'Harness Outsider', 'agency')
				ON CONFLICT (id) DO NOTHING`,
				OrgOwner.String(), OrgFirm.String(), OrgOutsider.String())
			return err
		})
	if err != nil {
		t.Fatalf("seeding the delegation organisations: %v", err)
	}
}

// isPermissionDenied reports whether the error is PostgreSQL refusing the
// privilege (42501) rather than the policy refusing the row. Both are passes
// for the deny-delete property; they are different locks on the same door.
func isPermissionDenied(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "42501"
}
