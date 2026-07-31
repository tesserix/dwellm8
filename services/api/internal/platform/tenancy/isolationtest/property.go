package isolationtest

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The property-scope contract from ADR-0009.
//
// ADR-0005 built the scope machinery and could only test it by calling
// is_delegated() directly, because no table had a property_id to scope. This is
// the file where that stops being true: the grant now reaches rows, and the
// question is whether it reaches exactly the ones it names.
//
// The distinction that matters is between the two granularities. A grant scoped
// to one flat must let the firm see the building — you cannot manage a flat
// without seeing the tower it is in — and must not let it see the flat next
// door. Those pull in opposite directions, and everything below is about the
// line between them.

// The owner's tree, seeded once and shared by every delegation test.
//
// Real rows, not invented uuids: since ADR-0009 a grant scope is validated
// against the grantor's own tree, so a scope naming a property that does not
// exist is refused at the point of writing it. That refusal is asserted below;
// it also means the fixtures have to be real.
const (
	// PropertyGranted holds the units a mandate names. PropertyOther belongs to
	// the same owner and is never granted to anybody.
	PropertyGranted = "b1111111-0000-0000-0000-000000000001"
	PropertyOther   = "b1111111-0000-0000-0000-000000000002"

	BlockGranted = "b2222222-0000-0000-0000-000000000001"

	// Two flats a firm manages, a third in the same property that it does not,
	// and the parking slot allotted to the first flat.
	UnitGrantedA    = "c1111111-0000-0000-0000-000000000001"
	UnitGrantedB    = "c1111111-0000-0000-0000-000000000002"
	UnitSibling     = "c1111111-0000-0000-0000-000000000003"
	UnitParkingA    = "c1111111-0000-0000-0000-000000000004"
	UnitParkingFree = "c1111111-0000-0000-0000-000000000005"
	// A unit in the property nobody was granted.
	UnitElsewhere = "c1111111-0000-0000-0000-000000000011"
)

// GrantedProperties and UngrantedProperties keep ADR-0005's shape — a mandate
// over part of a portfolio — and are now real properties belonging to OrgOwner.
var (
	GrantedProperties   = []string{PropertyGranted}
	UngrantedProperties = []string{PropertyOther}
)

// unitCodes maps a fixture id to the code the assertions report, so a failure
// says "the firm can see 103" rather than printing a uuid.
var unitCodes = map[string]string{
	UnitGrantedA:    "101",
	UnitGrantedB:    "102",
	UnitSibling:     "103",
	UnitParkingA:    "P-1",
	UnitParkingFree: "P-2",
	UnitElsewhere:   "201",
}

// SeedPropertyTree writes the owner's tree. Idempotent, and a platform act for
// the same reason seeding organisations is: the tests act as the firm, and the
// firm is precisely who must not be able to create these rows.
//
// It seeds the organisations first because the tree hangs off them. Leaving that
// to the caller passes on a database that already has them — every developer
// machine — and fails only on a fresh one, which is CI.
func SeedPropertyTree(t *testing.T, p tenancy.PlatformPool) {
	t.Helper()
	seedDelegationOrgs(t, p)

	err := tenancy.Platform(context.Background(), p, "seeding the property-scope contract",
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `
				INSERT INTO properties (id, tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin) VALUES
				  ($1, $3, 'HARNESS-1', 'Harness Towers', 'society',
				   '1 Harness Road', 'Indiranagar', 'Bengaluru', 'KA', '560038'),
				  ($2, $3, 'HARNESS-2', 'Harness Annexe', 'building',
				   '2 Harness Road', 'Indiranagar', 'Bengaluru', 'KA', '560038')
				ON CONFLICT (id) DO NOTHING`,
				PropertyGranted, PropertyOther, OrgOwner.String()); err != nil {
				return fmt.Errorf("seeding properties: %w", err)
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO blocks (id, tenant_id, property_id, code, name, floors)
				VALUES ($1, $2, $3, 'A', 'Wing A', 12)
				ON CONFLICT (id) DO NOTHING`,
				BlockGranted, OrgOwner.String(), PropertyGranted); err != nil {
				return fmt.Errorf("seeding the block: %w", err)
			}

			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, block_id, unit_kind, code,
				                   floor, carpet_area_sqft, builtup_area_sqft) VALUES
				  ($1, $5, $6, $7, 'flat', '101', 1, 620.00, 890.00),
				  ($2, $5, $6, $7, 'flat', '102', 1, 640.00, 910.00),
				  ($3, $5, $6, $7, 'flat', '103', 1, 660.00, 930.00),
				  ($4, $5, $8, NULL, 'flat', '201', 2, 700.00, 980.00)
				ON CONFLICT (id) DO NOTHING`,
				UnitGrantedA, UnitGrantedB, UnitSibling, UnitElsewhere,
				OrgOwner.String(), PropertyGranted, BlockGranted, PropertyOther); err != nil {
				return fmt.Errorf("seeding flats: %w", err)
			}

			// One slot allotted to flat 101, one free. The allotted slot is how
			// the ancillary hop is asserted; the free one is how the assertion
			// is stopped from passing by accident.
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, parent_unit_id) VALUES
				  ($1, $3, $4, 'parking', 'P-1', -1, $5),
				  ($2, $3, $4, 'parking', 'P-2', -1, NULL)
				ON CONFLICT (id) DO NOTHING`,
				UnitParkingA, UnitParkingFree, OrgOwner.String(), PropertyGranted, UnitGrantedA); err != nil {
				return fmt.Errorf("seeding parking: %w", err)
			}
			return nil
		})
	if err != nil {
		t.Fatalf("seeding the property tree: %v", err)
	}
}

// RunPropertyScope asserts what a grant reaches once the rows it points at
// exist. ADR-0009 §4.
func RunPropertyScope(t *testing.T, p tenancy.Pool, plat tenancy.PlatformPool) {
	t.Helper()
	ctx := context.Background()

	seedDelegationFixtures(t, plat)

	owner := tenancy.With(ctx, OrgOwner)

	t.Run("the owner sees its own tree", func(t *testing.T) {
		if got := visibleUnits(t, p, owner); len(got) != 6 {
			t.Fatalf("the owner sees units %v, want all six of its own", got)
		}
		if got := visibleProperties(t, p, owner); len(got) != 2 {
			t.Fatalf("the owner sees %d properties, want 2", len(got))
		}
	})

	t.Run("a unit-scoped mandate", func(t *testing.T) {
		grant := SeedGrant(t, plat, Grant{
			Grantor:     OrgOwner,
			Grantee:     OrgFirm,
			Permissions: []string{"property.read"},
			Units:       []string{UnitGrantedA, UnitGrantedB},
		})
		firm := tenancy.WithGrant(tenancy.With(ctx, OrgFirm), grant)

		t.Run("reaches exactly the units it names, and the parking that comes with them", func(t *testing.T) {
			// P-1 is allotted to flat 101, so it travels with it. P-2 is not
			// allotted to anything and must stay invisible; 103 is the flat next
			// door, which is the whole point of unit granularity.
			assertUnits(t, p, firm, []string{"101", "102", "P-1"})
		})

		t.Run("sees the building the flats are in", func(t *testing.T) {
			got := visibleProperties(t, p, firm)
			if len(got) != 1 || got[0] != "HARNESS-1" {
				t.Fatalf("the firm sees properties %v, want only HARNESS-1 — a firm managing a flat "+
					"must see the tower it is in, and nothing else of the owner's portfolio", got)
			}
		})

		t.Run("cannot write the unit it can read", func(t *testing.T) {
			err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx,
					`UPDATE units SET occupancy = 'occupied' WHERE id = $1`, UnitGrantedA)
				return err
			})
			if err == nil {
				t.Fatal("a property.read mandate changed the unit's occupancy — " +
					"USING and WITH CHECK are asking for the same permission")
			}
		})

		t.Run("cannot add a unit to the owner's property", func(t *testing.T) {
			err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO units (tenant_id, property_id, unit_kind, code, carpet_area_sqft)
					VALUES ($1, $2, 'flat', 'FIRM-1', 500)`, OrgOwner.String(), PropertyGranted)
				return err
			})
			if err == nil {
				t.Fatal("a firm created a unit inside the owner's property under a read mandate")
			}
		})

		t.Run("revocation closes the window", func(t *testing.T) {
			revoke(t, plat, grant)
			assertUnits(t, p, firm, nil)
			if got := visibleProperties(t, p, firm); len(got) != 0 {
				t.Fatalf("the firm still sees properties %v after revocation", got)
			}
		})
	})

	t.Run("a property-scoped mandate reaches every unit of that property and no other", func(t *testing.T) {
		grant := SeedGrant(t, plat, Grant{
			Grantor:     OrgOwner,
			Grantee:     OrgFirm,
			Permissions: []string{"property.read"},
			Properties:  []string{PropertyGranted},
		})
		firm := tenancy.WithGrant(tenancy.With(ctx, OrgFirm), grant)

		// Every unit of HARNESS-1, including the unallotted slot — and not 201,
		// which belongs to the property next door.
		assertUnits(t, p, firm, []string{"101", "102", "103", "P-1", "P-2"})
	})

	t.Run("an unrelated organisation sees none of it", func(t *testing.T) {
		assertUnits(t, p, tenancy.With(ctx, OrgOutsider), nil)
	})

	t.Run("a scope naming a property the grantor does not own is refused", func(t *testing.T) {
		// The outsider's own property, named in a grant the owner is writing.
		// Nothing about the owner's intent makes it theirs to delegate.
		other := seedOutsiderProperty(t, plat)

		grant := SeedGrant(t, plat, Grant{
			Grantor:     OrgOwner,
			Grantee:     OrgFirm,
			Permissions: []string{"property.read"},
			Properties:  []string{PropertyGranted},
		})
		err := tenancy.Platform(ctx, plat, "asserting the scope target check",
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO delegation_grant_scopes (grant_id, tenant_id, scope_kind, scope_id)
					VALUES ($1, $2, 'property', $3)`, grant.String(), OrgOwner.String(), other)
				return err
			})
		if err == nil {
			t.Fatal("an owner scoped a grant to a property belonging to another organisation — " +
				"scope_id is being taken on trust, and would start working the day that property changed hands")
		}
	})

	t.Run("no unit may be deleted, by either party", func(t *testing.T) {
		grant := SeedGrant(t, plat, Grant{
			Grantor:     OrgOwner,
			Grantee:     OrgFirm,
			Permissions: []string{"property.read", "property.write"},
			Units:       []string{UnitGrantedA},
		})
		firm := tenancy.WithGrant(tenancy.With(ctx, OrgFirm), grant)

		for _, actor := range []context.Context{owner, firm} {
			err := tenancy.Scoped(actor, p, func(ctx context.Context, tx pgx.Tx) error {
				tag, err := tx.Exec(ctx, `DELETE FROM units WHERE id = $1`, UnitGrantedA)
				if err != nil {
					return err // the privilege is withheld: also a pass
				}
				if tag.RowsAffected() != 0 {
					return fmt.Errorf("deleted %d unit rows", tag.RowsAffected())
				}
				return nil
			})
			if err != nil && !isPermissionDenied(err) {
				t.Fatalf("deleting a unit: %v", err)
			}
		}

		if got := visibleUnits(t, p, owner); len(got) != 6 {
			t.Fatalf("the owner sees units %v after the delete attempts — the tree the ledger "+
				"hangs from must not be removable", got)
		}
	})

	// Issue #10's validation scenario.
	t.Run("a duplicate unit code inside one property is rejected", func(t *testing.T) {
		err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO units (tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES ($1, $2, 'flat', '101', 500)`, OrgOwner.String(), PropertyGranted)
			return err
		})
		if err == nil {
			t.Fatal("two units share the code 101 in one property — every downstream reference " +
				"to \"flat 101\" is now ambiguous")
		}

		// The same code in a different property is a different flat, not a clash.
		err = tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO units (tenant_id, property_id, unit_kind, code, carpet_area_sqft)
				VALUES ($1, $2, 'flat', '101', 500)
				ON CONFLICT (property_id, code) DO NOTHING`, OrgOwner.String(), PropertyOther)
			return err
		})
		if err != nil {
			t.Fatalf("flat 101 of the second property was rejected (%v) — the uniqueness is "+
				"scoped too widely, and no society with two wings can be represented", err)
		}
	})
}

// assertUnits compares the exact set of unit codes a session can see. An exact
// set, not a count: a count of three passes when the three are the wrong three.
func assertUnits(t *testing.T, p tenancy.Pool, as context.Context, want []string) {
	t.Helper()
	got := visibleUnits(t, p, as)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("visible units %v, want %v", got, want)
	}
}

func visibleUnits(t *testing.T, p tenancy.Pool, as context.Context) []string {
	t.Helper()
	var codes []string
	err := tenancy.Scoped(as, p, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT code::text FROM units WHERE id = ANY($1)`, fixtureUnits())
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			codes = append(codes, c)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("listing visible units: %v", err)
	}
	sort.Strings(codes)
	return codes
}

func visibleProperties(t *testing.T, p tenancy.Pool, as context.Context) []string {
	t.Helper()
	var codes []string
	err := tenancy.Scoped(as, p, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx,
			`SELECT code::text FROM properties WHERE id = ANY($1)`,
			[]string{PropertyGranted, PropertyOther})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c string
			if err := rows.Scan(&c); err != nil {
				return err
			}
			codes = append(codes, c)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("listing visible properties: %v", err)
	}
	sort.Strings(codes)
	return codes
}

func fixtureUnits() []string {
	out := make([]string, 0, len(unitCodes))
	for id := range unitCodes {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// seedOutsiderProperty gives OrgOutsider a property of its own, so that "a
// property the grantor does not own" is a real row rather than a missing one.
// The two failures are indistinguishable to the writer, which is deliberate.
func seedOutsiderProperty(t *testing.T, p tenancy.PlatformPool) string {
	t.Helper()
	const id = "b1111111-0000-0000-0000-0000000000ff"
	err := tenancy.Platform(context.Background(), p, "seeding the outsider's property",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO properties (id, tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin)
				VALUES ($1, $2, 'OUTSIDER-1', 'Outsider House', 'standalone',
				        '9 Outsider Road', 'Koramangala', 'Bengaluru', 'KA', '560034')
				ON CONFLICT (id) DO NOTHING`, id, OrgOutsider.String())
			return err
		})
	if err != nil {
		t.Fatalf("seeding the outsider's property: %v", err)
	}
	return id
}
