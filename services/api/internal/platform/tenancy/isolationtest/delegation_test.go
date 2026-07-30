package isolationtest_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0005. The grant is the only way one organisation reaches another's rows,
// so it gets the same treatment as the tenancy model itself: asserted against a
// real PostgreSQL, because what is being tested is PostgreSQL's behaviour.

func TestGrantModel(t *testing.T) {
	p := pool(t)
	isolationtest.RunGrantModel(t, p, platformPool(t))
}

// audit_events is the first delegable table, and the one every other module's
// delegated access ends up writing to. A firm may add to the owner's audit
// trail — that is the record of the access it just made — and may read back
// only what it wrote under its own grant.
func TestAuditEventsDelegation(t *testing.T) {
	p := pool(t)

	isolationtest.RunDelegated(t, p, platformPool(t), isolationtest.DelegatedTable{
		Name: "audit_events",
		InsertAsGrantor: func(ctx context.Context, tx pgx.Tx, owner tenancy.ID, grant tenancy.GrantID, actor tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO audit_events
				    (tenant_id, actor_kind, module, action, subject_kind, subject_id, actor_org_id, grant_id)
				VALUES ($1, 'user', 'property', 'harness.delegated', 'test', $2, $3, $4)`,
				owner.String(), token, actor.String(), grant.String())
			return err
		},
		CountVisible: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM audit_events WHERE action = 'harness.delegated' AND subject_id = $1`,
				token).Scan(&n)
			return n, err
		},
	})
}

// The write a delegated session must not be able to make: an entry in the
// owner's trail that does not say who really made it. A firm adding to the
// owner's history is intended; a firm writing history in the owner's name is
// the thing that would make the trail worthless.
func TestDelegatedSessionCannotForgeAnAuditEntry(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)

	grant := isolationtest.SeedGrant(t, plat, isolationtest.Grant{
		Grantor:     isolationtest.OrgOwner,
		Grantee:     isolationtest.OrgFirm,
		Permissions: []string{"property.read"},
		Properties:  isolationtest.GrantedProperties,
	})
	firm := tenancy.WithGrant(tenancy.With(context.Background(), isolationtest.OrgFirm), grant)

	cases := []struct {
		name              string
		actorOrg, grantID string
	}{
		{"stamped as the owner", isolationtest.OrgOwner.String(), grant.String()},
		{"stamped with no grant at all", isolationtest.OrgFirm.String(), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
				var g any
				if tc.grantID != "" {
					g = tc.grantID
				}
				_, err := tx.Exec(ctx, `
					INSERT INTO audit_events
					    (tenant_id, actor_kind, module, action, subject_kind, subject_id, actor_org_id, grant_id)
					VALUES ($1, 'user', 'property', 'harness.forged', 'test', 'forged', $2, $3)`,
					isolationtest.OrgOwner.String(), tc.actorOrg, g)
				return err
			})
			if err == nil {
				t.Fatal("a delegated session wrote an audit entry that does not name it — " +
					"the owner's trail can be forged by the firm it hired")
			}
		})
	}
}

// The rule for work already in flight when a grant is revoked, asserted rather
// than asserted-in-prose. tenancy.Scoped runs at READ COMMITTED, so the next
// statement in an open transaction takes a fresh snapshot and sees the
// revocation. A firm's in-flight step cannot straddle the revocation.
func TestRevocationStopsAnOpenTransaction(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	ctx := context.Background()

	grant := isolationtest.SeedGrant(t, plat, isolationtest.Grant{
		Grantor:     isolationtest.OrgOwner,
		Grantee:     isolationtest.OrgFirm,
		Permissions: []string{"money.collect"},
		Properties:  isolationtest.GrantedProperties,
	})
	firm := tenancy.WithGrant(tenancy.With(ctx, isolationtest.OrgFirm), grant)

	err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
		var before, after bool
		if err := tx.QueryRow(ctx, `SELECT is_delegated($1, $2, 'money.collect')`,
			isolationtest.OrgOwner.String(), isolationtest.GrantedProperties[0]).Scan(&before); err != nil {
			return err
		}
		if !before {
			t.Fatal("the grant did not reach the property it names")
		}

		// The owner revokes, on another connection, mid-transaction.
		if err := tenancy.Platform(ctx, plat, "revoking mid-transaction", func(ctx context.Context, ptx pgx.Tx) error {
			_, err := ptx.Exec(ctx, `UPDATE delegation_grants SET revoked_at = now() WHERE id = $1`, grant.String())
			return err
		}); err != nil {
			return err
		}

		if err := tx.QueryRow(ctx, `SELECT is_delegated($1, $2, 'money.collect')`,
			isolationtest.OrgOwner.String(), isolationtest.GrantedProperties[0]).Scan(&after); err != nil {
			return err
		}
		if after {
			t.Fatal("an open transaction kept its access after the owner revoked — " +
				"revocation is only as immediate as the isolation level allows, and this one is not READ COMMITTED")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("the in-flight revocation test: %v", err)
	}
}
