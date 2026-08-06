package isolationtest

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The resident scope, against PostgreSQL. ADR-0029, issue #51.
//
// ADR-0003's harness asserts that one organisation cannot read another's. That
// is the boundary the whole product rests on and it is the wrong boundary for a
// renter: a tenant scoped to their landlord's organisation would read every
// other tenant of that landlord — their rent, their arrears, their phone-shaped
// party id — and every screen would look correct while doing it.
//
// So there is a second narrowing, and the properties it must have are the ones
// below. They are asserted against a real database because they are PostgreSQL's:
// a RESTRICTIVE policy, a session variable, and what happens when the variable
// was never set.

// The fixture's people and tenancies.
//
// Two landlords and two renters, arranged so that every way of getting this
// wrong shows up as a specific failure rather than as "the count is off":
//
//   - Priya rents flat 101 from the owner and flat 401 from a second landlord.
//     She is the story's edge case, and neither landlord may learn of the other.
//   - Rohit rents flat 102 from the *same* owner. He is the leak that a
//     tenant-scoped-only policy would produce, and the reason this file exists.
const (
	ResidentPriya = "d1111111-0000-0000-0000-000000000001"
	ResidentRohit = "d1111111-0000-0000-0000-000000000002"

	// LeasePriyaOwner and LeaseRohitOwner belong to the same landlord.
	LeasePriyaOwner = "e1111111-0000-0000-0000-000000000001"
	LeaseRohitOwner = "e1111111-0000-0000-0000-000000000002"
	// LeasePriyaOther is the second landlord's, and is what makes "two
	// organisations, one renter" a thing the database is asked about.
	LeasePriyaOther = "e1111111-0000-0000-0000-000000000003"

	// The second landlord's tree.
	PropertySecond = "b1111111-0000-0000-0000-00000000000a"
	UnitSecond     = "c1111111-0000-0000-0000-00000000000a"
)

// SeedResidentFixtures writes both landlords' tenancies and the money on them.
//
// A platform act, and it has to be: it writes rows for two organisations, and
// the sessions under test are precisely the ones that must not be able to.
func SeedResidentFixtures(t *testing.T, plat tenancy.PlatformPool) {
	t.Helper()
	SeedPropertyTree(t, plat)

	err := tenancy.Platform(context.Background(), plat, "seeding the resident-scope contract",
		func(ctx context.Context, tx pgx.Tx) error {
			// A second landlord, with a building of its own. OrgOutsider already
			// exists as an organisation; what it has never had is property.
			if _, err := tx.Exec(ctx, `
				INSERT INTO properties (id, tenant_id, code, name, kind,
				                        address_line1, locality, city, state_code, pin)
				VALUES ($1, $2, 'HARNESS-3', 'Second Landlord House', 'building',
				        '3 Harness Road', 'Koramangala', 'Bengaluru', 'KA', '560034')
				ON CONFLICT (id) DO NOTHING`,
				PropertySecond, OrgOutsider.String()); err != nil {
				return fmt.Errorf("seeding the second landlord's property: %w", err)
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO units (id, tenant_id, property_id, unit_kind, code, floor, carpet_area_sqft)
				VALUES ($1, $2, $3, 'flat', '401', 4, 700.00)
				ON CONFLICT (id) DO NOTHING`,
				UnitSecond, OrgOutsider.String(), PropertySecond); err != nil {
				return fmt.Errorf("seeding the second landlord's flat: %w", err)
			}

			// The two renters as people, not just party ids: a manager's screen
			// names who is in a flat, and that name lives here.
			if _, err := tx.Exec(ctx, `
				INSERT INTO identity_principals (surface, gip_uid, party_id, phone, sign_in_provider, display_name)
				VALUES ('live', 'harness-priya', $1, '+919876500011', 'phone', 'Priya Nair'),
				       ('live', 'harness-rohit', $2, '+919876500012', 'phone', 'Rohit Menon')
				ON CONFLICT (surface, gip_uid) DO NOTHING`,
				ResidentPriya, ResidentRohit); err != nil {
				return fmt.Errorf("seeding the renters: %w", err)
			}

			leases := []struct {
				id, org, property, unit, party string
			}{
				{LeasePriyaOwner, OrgOwner.String(), PropertyGranted, UnitGrantedA, ResidentPriya},
				{LeaseRohitOwner, OrgOwner.String(), PropertyGranted, UnitGrantedB, ResidentRohit},
				{LeasePriyaOther, OrgOutsider.String(), PropertySecond, UnitSecond, ResidentPriya},
			}
			for _, l := range leases {
				// Everything below is written once and only once. The harness
				// commits, so a second run of the suite replays this function
				// against rows that already exist — and the effective-dated
				// tables refuse a second live row by exclusion constraint rather
				// than by primary key, so ON CONFLICT DO NOTHING does not save
				// them. Measured: "conflicting key value violates exclusion
				// constraint lease_tax_facts_no_overlap" on the second run.
				var seeded string
				err := tx.QueryRow(ctx, `
					INSERT INTO leases (id, tenant_id, property_id, unit_id, state, valid_from, valid_to)
					VALUES ($1, $2, $3, $4, 'active', date '2026-01-01', date '2026-12-31')
					ON CONFLICT (id) DO NOTHING
					RETURNING id::text`,
					l.id, l.org, l.property, l.unit).Scan(&seeded)
				if errors.Is(err, pgx.ErrNoRows) {
					continue // an earlier run wrote this tenancy and everything on it
				}
				if err != nil {
					return fmt.Errorf("seeding lease %s: %w", l.id, err)
				}
				if err := SeedLeaseTaxFacts(ctx, tx, l.org, l.id, "2026-01-01"); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO lease_parties (tenant_id, lease_id, party_id, role, valid_from)
					VALUES ($1, $2, $3, 'tenant', date '2026-01-01')
					ON CONFLICT DO NOTHING`, l.org, l.id, l.party); err != nil {
					return fmt.Errorf("seeding the tenant on %s: %w", l.id, err)
				}
				if _, err := tx.Exec(ctx, `
					INSERT INTO rent_schedule (tenant_id, lease_id, amount_minor, due_day, valid_from)
					VALUES ($1, $2, 2500000, 5, date '2026-01-01')
					ON CONFLICT DO NOTHING`, l.org, l.id); err != nil {
					return fmt.Errorf("seeding the rent on %s: %w", l.id, err)
				}

				// One invoice per tenancy: a receivable against the tenant and the
				// matching income against the owner. The second line is the one a
				// renter must not see — what their landlord earns is not theirs.
				if err := seedResidentInvoice(ctx, tx, l.id, l.org, l.property, l.unit, l.party); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		t.Fatalf("seeding the resident fixtures: %v", err)
	}
}

func seedResidentInvoice(ctx context.Context, tx pgx.Tx, lease, org, property, unit, party string) error {
	var entry string
	err := tx.QueryRow(ctx, `
		INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, lease_id,
		                             source_kind, source_id, idempotency_key, memo)
		VALUES ($1, 'invoice', date '2026-02-01', $2::uuid, 'lease_charge', $2::text, $3, 'Rent, February 2026')
		ON CONFLICT (tenant_id, idempotency_key) DO NOTHING
		RETURNING id`, org, lease, "resident-fixture:"+lease).Scan(&entry)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil // already seeded by an earlier run
	}
	if err != nil {
		return fmt.Errorf("seeding the invoice on %s: %w", lease, err)
	}

	for _, line := range []struct {
		account, side, party, partyID string
	}{
		{"tenant_receivable", "debit", "tenant", party},
		{"rent_income", "credit", "owner", ownerPartyFor(org)},
	} {
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger_postings (entry_id, tenant_id, property_id, unit_id,
			                             account_code, side, amount_minor, party_kind, party_id)
			VALUES ($1, $2, $3, $4, $5, $6, 2500000, $7, $8)`,
			entry, org, property, unit, line.account, line.side, line.party, line.partyID); err != nil {
			return fmt.Errorf("seeding the %s line on %s: %w", line.account, lease, err)
		}
	}
	return nil
}

// ownerPartyFor gives each landlord a stable owner party. Derived from the
// organisation so the two landlords' income lines are distinguishable in a
// failure message.
func ownerPartyFor(org string) string { return "d2222222-0000-0000-0000-" + org[len(org)-12:] }

// RunResidentScope asserts ADR-0029's properties.
func RunResidentScope(t *testing.T, p tenancy.Pool, plat tenancy.PlatformPool) {
	t.Helper()
	ctx := context.Background()
	SeedResidentFixtures(t, plat)

	// The two sessions the whole design turns on: the same person, scoped to one
	// landlord at a time, never to both at once.
	priyaAtOwner := tenancy.WithResident(tenancy.With(ctx, OrgOwner), ResidentPriya)
	priyaAtOther := tenancy.WithResident(tenancy.With(ctx, OrgOutsider), ResidentPriya)
	rohitAtOwner := tenancy.WithResident(tenancy.With(ctx, OrgOwner), ResidentRohit)
	// And the landlord, who is not a renter and must be entirely unaffected.
	owner := tenancy.With(ctx, OrgOwner)

	t.Run("a renter sees their own tenancy and not their neighbour's", func(t *testing.T) {
		got := leaseIDs(t, p, priyaAtOwner)
		if len(got) != 1 || got[0] != LeasePriyaOwner {
			t.Fatalf("Priya sees leases %v, want only her own (%s) — a tenant scoped only to the "+
				"organisation reads every other tenant of the same landlord, which is the leak "+
				"ADR-0029 exists to close", got, LeasePriyaOwner)
		}
		if got := leaseIDs(t, p, rohitAtOwner); len(got) != 1 || got[0] != LeaseRohitOwner {
			t.Fatalf("Rohit sees leases %v, want only %s", got, LeaseRohitOwner)
		}
	})

	t.Run("a renter with two landlords sees one at a time", func(t *testing.T) {
		if got := leaseIDs(t, p, priyaAtOther); len(got) != 1 || got[0] != LeasePriyaOther {
			t.Fatalf("scoped to the second landlord Priya sees %v, want only %s", got, LeasePriyaOther)
		}
		// The property that matters for the story's edge case: neither read
		// contains the other's lease, so neither landlord can be shown a row
		// that would tell them the other exists.
		for _, id := range leaseIDs(t, p, priyaAtOwner) {
			if id == LeasePriyaOther {
				t.Fatalf("a session scoped to one landlord returned the other landlord's lease — " +
					"the two organisations would learn of each other through their shared tenant")
			}
		}
	})

	t.Run("the landlord is unaffected", func(t *testing.T) {
		got := leaseIDs(t, p, owner)
		var mine, theirs int
		for _, id := range got {
			switch id {
			case LeasePriyaOwner, LeaseRohitOwner:
				mine++
			case LeasePriyaOther:
				theirs++
			}
		}
		if mine != 2 {
			t.Fatalf("the landlord sees %d of their own two tenancies — the resident policy is "+
				"narrowing a session that is not a renter's", mine)
		}
		if theirs != 0 {
			t.Fatalf("the landlord sees another organisation's lease — ADR-0003 is broken")
		}
	})

	t.Run("a renter reads their own postings and not the owner's income", func(t *testing.T) {
		accounts := postingAccounts(t, p, priyaAtOwner)
		for _, a := range accounts {
			if a != "tenant_receivable" {
				t.Fatalf("Priya can read a %s posting — a renter sees what they owe, and what their "+
					"landlord earns on the same invoice is not theirs", a)
			}
		}
		if len(accounts) == 0 {
			t.Fatalf("Priya can read none of her own postings — the policy is denying rather than narrowing")
		}
	})

	t.Run("a renter cannot read another renter's postings", func(t *testing.T) {
		var n int
		err := tenancy.Scoped(priyaAtOwner, p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*) FROM ledger_postings WHERE party_id = $1::uuid`,
				ResidentRohit).Scan(&n)
		})
		if err != nil {
			t.Fatalf("counting: %v", err)
		}
		if n != 0 {
			t.Fatalf("Priya can read %d of Rohit's postings — they share a landlord, which is "+
				"precisely why the organisation is not a sufficient boundary here", n)
		}
	})

	t.Run("a renter cannot read a table nobody opened to them", func(t *testing.T) {
		// The deny-by-default loop. mandates is a real table with real rows for
		// this organisation and no business being on a tenant's screen; the point
		// of the assertion is that nobody had to remember it.
		for _, table := range []string{"mandates", "kyc_verifications", "property_ownership", "audit_events"} {
			var n int
			err := tenancy.Scoped(priyaAtOwner, p, func(ctx context.Context, tx pgx.Tx) error {
				return tx.QueryRow(ctx, `SELECT count(*) FROM `+table).Scan(&n)
			})
			if err != nil {
				t.Fatalf("counting %s as a renter returned an error (%v) — the deny policy must "+
					"return no rows quietly, not raise", table, err)
			}
			if n != 0 {
				t.Fatalf("a renter can read %d rows of %s — the ADR-0029 deny loop did not cover it", n, table)
			}
		}
	})

	t.Run("a renter cannot write a payment for somebody else", func(t *testing.T) {
		err := tenancy.Scoped(priyaAtOwner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO payments (tenant_id, property_id, unit_id, lease_id, payer_kind, payer_id,
				                      amount_minor, method, provider, status, idempotency_key)
				VALUES ($1, $2, $3, $4, 'tenant', $5, 100000, 'upi_intent', 'offline', 'created', $6)`,
				OrgOwner.String(), PropertyGranted, UnitGrantedB, LeaseRohitOwner, ResidentRohit,
				"resident-forgery-"+token(t))
			return err
		})
		if err == nil {
			t.Fatalf("a renter wrote a payment against another tenant's lease — the WITH CHECK on " +
				"payments_resident_scope is missing or is not checking the payer")
		}
	})

	t.Run("a renter can pay their own rent", func(t *testing.T) {
		err := tenancy.Scoped(priyaAtOwner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO payments (tenant_id, property_id, unit_id, lease_id, payer_kind, payer_id,
				                      amount_minor, method, provider, status, idempotency_key)
				VALUES ($1, $2, $3, $4, 'tenant', $5, 100000, 'upi_intent', 'offline', 'created', $6)`,
				OrgOwner.String(), PropertyGranted, UnitGrantedA, LeasePriyaOwner, ResidentPriya,
				"resident-own-"+token(t))
			return err
		})
		if err != nil {
			t.Fatalf("a renter could not write a payment for their own tenancy: %v — the scope is "+
				"denying the one write it is supposed to permit", err)
		}
	})

	t.Run("a payment naming no tenancy is refused", func(t *testing.T) {
		// resident_holds_lease() is false for a NULL lease, which is what makes
		// "a renter's payment always names the tenancy it pays" a property of the
		// database rather than of the handler that happens to set it.
		err := tenancy.Scoped(priyaAtOwner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO payments (tenant_id, property_id, unit_id, payer_kind, payer_id,
				                      amount_minor, method, provider, status, idempotency_key)
				VALUES ($1, $2, $3, 'tenant', $4, 100000, 'upi_intent', 'offline', 'created', $5)`,
				OrgOwner.String(), PropertyGranted, UnitGrantedA, ResidentPriya,
				"resident-unattached-"+token(t))
			return err
		})
		if err == nil {
			t.Fatalf("a renter wrote a payment attached to no tenancy — it would post against " +
				"nothing and appear on no statement")
		}
	})

	t.Run("a renter sees only their own party row", func(t *testing.T) {
		var n int
		err := tenancy.Scoped(priyaAtOwner, p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM lease_parties WHERE party_id <> $1::uuid`,
				ResidentPriya).Scan(&n)
		})
		if err != nil {
			t.Fatalf("counting: %v", err)
		}
		if n != 0 {
			t.Fatalf("a renter can read %d other people's lease_parties rows — that is the table "+
				"the scope check itself reads, so widening it widens everything", n)
		}
	})
}

func leaseIDs(t *testing.T, p tenancy.Pool, ctx context.Context) []string {
	t.Helper()
	var out []string
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT id::text FROM leases
			 WHERE id = ANY($1) ORDER BY id`,
			[]string{LeasePriyaOwner, LeaseRohitOwner, LeasePriyaOther})
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading leases: %v", err)
	}
	return out
}

func postingAccounts(t *testing.T, p tenancy.Pool, ctx context.Context) []string {
	t.Helper()
	var out []string
	err := tenancy.Scoped(ctx, p, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT l.account_code
			  FROM ledger_postings l
			  JOIN journal_entries e ON e.id = l.entry_id AND e.tenant_id = l.tenant_id
			 WHERE e.lease_id = $1::uuid
			 ORDER BY 1`, LeasePriyaOwner)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var a string
			if err := rows.Scan(&a); err != nil {
				return err
			}
			out = append(out, a)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("reading postings: %v", err)
	}
	return out
}
