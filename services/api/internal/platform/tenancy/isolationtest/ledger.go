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

// The ledger contract from ADR-0006.
//
// Three properties are asserted here that cannot be asserted anywhere else,
// because they are PostgreSQL's rather than Go's: that an entry which does not
// balance is refused at COMMIT, that a posted entry cannot be edited or removed
// by anybody the application connects as, and that a firm under a unit-scoped
// mandate sees the money of the units it manages and no others.
//
// The arithmetic — which account each event touches, and how a payment splits
// between principal and advance — is tested in internal/money/domain, without a
// database, because it is arithmetic.

// The parties this run posts against.
//
// Derived from the run token rather than fixed, because the harness commits and
// a balance is a sum over everything ever written: with fixed party ids the
// second run of this test reads the first run's money and the first assertion
// fails for a reason that has nothing to do with the ledger. Measured, before
// the token was threaded through: "the receivable is 400000 ... want 0".
func runParty(prefix, tok string) string {
	return fmt.Sprintf("%s-0000-0000-0000-%s", prefix, tok[:12])
}

// entry writes one balanced entry and returns its id. Everything in this file
// posts through it, so no test can accidentally write an entry that the balance
// rule would have rejected.
type posting struct {
	account  string
	side     string
	amount   int64
	property string
	unit     string
	party    string // party kind
	partyID  string
}

func writeEntry(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, kind, key string, postings []posting) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `
		INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind, source_id, idempotency_key, memo)
		VALUES ($1, $2, current_date, 'harness', $3, $3, $4) RETURNING id`,
		tenant.String(), kind, key, key).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("writing the entry: %w", err)
	}
	for _, p := range postings {
		var prop, unit, partyID any
		if p.property != "" {
			prop = p.property
		}
		if p.unit != "" {
			unit = p.unit
		}
		party := p.party
		if party == "" {
			party = "none"
		}
		if p.partyID != "" {
			partyID = p.partyID
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO ledger_postings (entry_id, tenant_id, property_id, unit_id,
			                             account_code, side, amount_minor, party_kind, party_id, memo)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			id, tenant.String(), prop, unit, p.account, p.side, p.amount, party, partyID, key); err != nil {
			return "", fmt.Errorf("writing a %s posting: %w", p.account, err)
		}
	}
	return id, nil
}

// RunLedger asserts ADR-0006 against a real PostgreSQL.
func RunLedger(t *testing.T, p tenancy.Pool, plat tenancy.PlatformPool) {
	t.Helper()
	ctx := context.Background()

	seedDelegationFixtures(t, plat)
	owner := tenancy.With(ctx, OrgOwner)
	tok := token(t)
	tenantParty := runParty("eeeeeeee", tok)
	ownerParty := runParty("ffffffff", tok)
	platformParty := runParty("dddddddd", tok)

	// Issue #7's primary scenario, against the database this time: an invoice of
	// 25000 and a UPI payment of 25000, and a receivable that nets to zero.
	t.Run("an invoice and its payment net the receivable to zero", func(t *testing.T) {
		key := "inv-" + tok
		err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			if _, err := writeEntry(ctx, tx, OrgOwner, "invoice", key, []posting{
				{account: "tenant_receivable", side: "debit", amount: 2500000,
					property: PropertyGranted, unit: UnitGrantedA, party: "tenant", partyID: tenantParty},
				{account: "rent_income", side: "credit", amount: 2500000,
					property: PropertyGranted, unit: UnitGrantedA, party: "owner", partyID: ownerParty},
			}); err != nil {
				return err
			}
			_, err := writeEntry(ctx, tx, OrgOwner, "payment", "pay-"+tok, []posting{
				{account: "gateway_clearing", side: "debit", amount: 2500000,
					property: PropertyGranted, unit: UnitGrantedA, party: "platform", partyID: platformParty},
				{account: "tenant_receivable", side: "credit", amount: 2500000,
					property: PropertyGranted, unit: UnitGrantedA, party: "tenant", partyID: tenantParty},
			})
			return err
		})
		if err != nil {
			t.Fatalf("posting the invoice and the payment: %v", err)
		}

		if got := balance(t, p, owner, "tenant_receivable", tenantParty); got != 0 {
			t.Fatalf("the receivable is %d after an invoice and a matching payment, want 0 — "+
				"either a posting went to the wrong side or the balance is not being derived", got)
		}
		if got := balance(t, p, owner, "rent_income", ownerParty); got != -2500000 {
			t.Fatalf("rent income is %d, want -2500000 (a credit)", got)
		}
		if got := balance(t, p, owner, "gateway_clearing", platformParty); got != 2500000 {
			t.Fatalf("gateway clearing is %d, want 2500000 held by the provider until it settles", got)
		}
	})

	// The derived-balance rule, stated as an equality rather than as a policy:
	// there is no stored balance to disagree with, and the view must agree with
	// the arithmetic it claims to perform.
	t.Run("the balances view is the sum of the postings, nothing else", func(t *testing.T) {
		var viaView, viaSum int64
		err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `
				SELECT coalesce(sum(balance_minor), 0) FROM ledger_balances
				 WHERE account_code = 'tenant_receivable' AND party_id = $1`,
				tenantParty).Scan(&viaView); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `
				SELECT coalesce(sum(signed_minor), 0) FROM ledger_postings
				 WHERE account_code = 'tenant_receivable' AND party_id = $1`,
				tenantParty).Scan(&viaSum)
		})
		if err != nil {
			t.Fatalf("comparing the view with the postings: %v", err)
		}
		if viaView != viaSum {
			t.Fatalf("ledger_balances reports %d and the postings sum to %d — the view is not "+
				"security_invoker, or it is filtering something the caller can see", viaView, viaSum)
		}
	})

	// Double entry, enforced by the database rather than believed of the caller.
	//
	// The interesting half is *when*: the INSERT succeeds and COMMIT is what
	// fails, because the check has to be deferred — an entry is unbalanced
	// between its first line and its last.
	t.Run("an entry that does not balance is refused at commit", func(t *testing.T) {
		var insertErr, commitErr error
		insertErr = tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := writeEntry(ctx, tx, OrgOwner, "invoice", "bad-"+tok, []posting{
				{account: "tenant_receivable", side: "debit", amount: 2500000,
					property: PropertyGranted, unit: UnitGrantedA, party: "tenant", partyID: tenantParty},
				{account: "rent_income", side: "credit", amount: 2000000,
					property: PropertyGranted, unit: UnitGrantedA, party: "owner", partyID: ownerParty},
			})
			return err
		})
		commitErr = insertErr
		if commitErr == nil {
			t.Fatal("an entry with debits of 2500000 and credits of 2000000 was committed — " +
				"the ledger now holds 5000 rupees that came from nowhere")
		}
		if !strings.Contains(commitErr.Error(), "does not balance") {
			t.Fatalf("the entry was refused, but for the wrong reason: %v", commitErr)
		}
		// It must be the commit that failed, not the insert: code that checks
		// each Exec and ignores Commit would have believed this entry. Measured:
		// "tenancy: commit: ERROR: journal entry ... does not balance: debits
		// 2500000, credits 2000000 (SQLSTATE 23514)".
		if !strings.Contains(commitErr.Error(), "commit") {
			t.Fatalf("the entry was refused before COMMIT (%v) — the balance check is not deferred, "+
				"and no multi-line entry can ever be written", commitErr)
		}
	})

	t.Run("an entry with one line is refused", func(t *testing.T) {
		err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := writeEntry(ctx, tx, OrgOwner, "invoice", "single-"+tok, []posting{
				{account: "tenant_receivable", side: "debit", amount: 100,
					property: PropertyGranted, unit: UnitGrantedA, party: "tenant", partyID: tenantParty},
			})
			return err
		})
		if err == nil {
			t.Fatal("a one-line entry was committed — a balance nobody can explain")
		}
	})

	t.Run("an entry with no postings at all is refused", func(t *testing.T) {
		err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := writeEntry(ctx, tx, OrgOwner, "invoice", "empty-"+tok, nil)
			return err
		})
		if err == nil {
			t.Fatal("an entry with no lines was committed — a header in every statement and a total in none")
		}
	})

	// Issue #7's failure scenario.
	t.Run("a posted entry cannot be edited or removed", func(t *testing.T) {
		key := "immutable-" + tok
		var entryID string
		err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			var err error
			entryID, err = writeEntry(ctx, tx, OrgOwner, "late_fee", key, []posting{
				{account: "tenant_receivable", side: "debit", amount: 50000,
					property: PropertyGranted, unit: UnitGrantedA, party: "tenant", partyID: tenantParty},
				{account: "late_fee_income", side: "credit", amount: 50000,
					property: PropertyGranted, unit: UnitGrantedA, party: "owner", partyID: ownerParty},
			})
			return err
		})
		if err != nil {
			t.Fatalf("posting the late fee: %v", err)
		}

		for _, attempt := range []struct {
			what string
			sql  string
		}{
			{"change an amount", `UPDATE ledger_postings SET amount_minor = 1 WHERE memo = $1`},
			{"change an account", `UPDATE ledger_postings SET account_code = 'write_off' WHERE memo = $1`},
			{"delete a posting", `DELETE FROM ledger_postings WHERE memo = $1`},
			{"backdate the entry", `UPDATE journal_entries SET occurred_on = '2020-01-01' WHERE memo = $1`},
			{"delete the entry", `DELETE FROM journal_entries WHERE memo = $1`},
		} {
			err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
				tag, err := tx.Exec(ctx, attempt.sql, key)
				if err != nil {
					return err // the privilege is withheld: also a pass
				}
				if tag.RowsAffected() != 0 {
					return fmt.Errorf("affected %d rows", tag.RowsAffected())
				}
				return nil
			})
			if err != nil && !isPermissionDenied(err) {
				t.Fatalf("attempting to %s: %v", attempt.what, err)
			}
		}

		// And the money is still there, unchanged.
		if got := balance(t, p, owner, "late_fee_income", ownerParty); got != -50000 {
			t.Fatalf("late fee income is %d after the edit attempts, want -50000", got)
		}

		// The only correction there is.
		t.Run("a reversing entry is the correction", func(t *testing.T) {
			err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
				var id string
				if err := tx.QueryRow(ctx, `
					INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind, source_id,
					                             idempotency_key, reverses_entry_id, reversal_reason, memo)
					VALUES ($1, 'reversal', current_date, 'harness', $2, $2, $3, 'wrong_amount', $4)
					RETURNING id`,
					OrgOwner.String(), "rev-"+tok, entryID, "rev-"+key).Scan(&id); err != nil {
					return err
				}
				for _, l := range []struct {
					account string
					side    string
				}{{"tenant_receivable", "credit"}, {"late_fee_income", "debit"}} {
					party, partyID := "tenant", tenantParty
					if l.account == "late_fee_income" {
						party, partyID = "owner", ownerParty
					}
					if _, err := tx.Exec(ctx, `
						INSERT INTO ledger_postings (entry_id, tenant_id, property_id, unit_id,
						                             account_code, side, amount_minor, party_kind, party_id, memo)
						VALUES ($1, $2, $3, $4, $5, $6, 50000, $7, $8, $9)`,
						id, OrgOwner.String(), PropertyGranted, UnitGrantedA, l.account, l.side,
						party, partyID, "rev-"+key); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				t.Fatalf("posting the reversal: %v", err)
			}
			if got := balance(t, p, owner, "late_fee_income", ownerParty); got != 0 {
				t.Fatalf("late fee income is %d after reversal, want 0", got)
			}
		})

		t.Run("an entry is reversed once", func(t *testing.T) {
			err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO journal_entries (tenant_id, entry_kind, occurred_on, source_kind, source_id,
					                             idempotency_key, reverses_entry_id, reversal_reason)
					VALUES ($1, 'reversal', current_date, 'harness', $2, $2, $3, 'operator_error')`,
					OrgOwner.String(), "rev2-"+tok, entryID)
				return err
			})
			if err == nil {
				t.Fatal("the same entry was reversed twice — the correction is now double the original, " +
					"and it looks like activity rather than like a defect")
			}
		})
	})

	// ADR-0002's idempotent consumers, as a unique index. A webhook delivered
	// twice must not double an owner's income.
	t.Run("the same event posted twice produces one entry", func(t *testing.T) {
		key := "dup-" + tok
		post := func() error {
			return tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
				_, err := writeEntry(ctx, tx, OrgOwner, "payment", key, []posting{
					{account: "gateway_clearing", side: "debit", amount: 100000,
						property: PropertyGranted, unit: UnitGrantedA, party: "platform", partyID: platformParty},
					{account: "tenant_advance", side: "credit", amount: 100000,
						property: PropertyGranted, unit: UnitGrantedA, party: "tenant", partyID: tenantParty},
				})
				return err
			})
		}
		if err := post(); err != nil {
			t.Fatalf("the first delivery: %v", err)
		}
		if err := post(); err == nil {
			t.Fatal("a redelivered webhook posted a second entry — the owner's income is now double")
		}
	})

	// A posting cannot name another organisation's property or unit, whatever
	// tenant_id it carries. ADR-0009 §3's composite keys, one level down.
	t.Run("a posting cannot be hung off another organisation's unit", func(t *testing.T) {
		err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := writeEntry(ctx, tx, OrgOwner, "invoice", "cross-"+tok, []posting{
				// The property belongs to the owner; the unit belongs to the
				// property next door.
				{account: "tenant_receivable", side: "debit", amount: 100,
					property: PropertyGranted, unit: UnitElsewhere, party: "tenant", partyID: tenantParty},
				{account: "rent_income", side: "credit", amount: 100,
					property: PropertyGranted, unit: UnitElsewhere, party: "owner", partyID: ownerParty},
			})
			return err
		})
		if err == nil {
			t.Fatal("a posting names a unit that is not in the property it claims — every per-property " +
				"statement is now wrong and nothing looks wrong")
		}
	})

	runLedgerDelegation(t, p, plat, tok, tenantParty, ownerParty)
}

// runLedgerDelegation is the half that matters most: what a management firm can
// see of an owner's money.
func runLedgerDelegation(t *testing.T, p tenancy.Pool, plat tenancy.PlatformPool, tok, tenantParty, ownerParty string) {
	t.Helper()
	ctx := context.Background()
	owner := tenancy.With(ctx, OrgOwner)

	// Money on three places: the granted flat, the flat next door, the parking
	// slot allotted to the granted flat, and the organisation's own statutory
	// position, which belongs to no property at all.
	seed := []struct {
		key      string
		property string
		unit     string
	}{
		{"scope-granted-" + tok, PropertyGranted, UnitGrantedA},
		{"scope-sibling-" + tok, PropertyGranted, UnitSibling},
		{"scope-parking-" + tok, PropertyGranted, UnitParkingA},
		{"scope-orglevel-" + tok, "", ""},
	}
	err := tenancy.Scoped(owner, p, func(ctx context.Context, tx pgx.Tx) error {
		for _, s := range seed {
			if _, err := writeEntry(ctx, tx, OrgOwner, "invoice", s.key, []posting{
				{account: "tenant_receivable", side: "debit", amount: 100000,
					property: s.property, unit: s.unit, party: "tenant", partyID: tenantParty},
				{account: "rent_income", side: "credit", amount: 100000,
					property: s.property, unit: s.unit, party: "owner", partyID: ownerParty},
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("seeding the delegated ledger: %v", err)
	}

	grant := SeedGrant(t, plat, Grant{
		Grantor:     OrgOwner,
		Grantee:     OrgFirm,
		Permissions: []string{"property.read", "money.read"},
		Units:       []string{UnitGrantedA},
	})
	firm := tenancy.WithGrant(tenancy.With(ctx, OrgFirm), grant)

	t.Run("a unit-scoped mandate sees that unit's money and its parking, and no other", func(t *testing.T) {
		// The flat it manages, and the slot allotted to that flat — the
		// ancillary hop from ADR-0009 §4, which only works because the parent is
		// stamped onto the posting when it is written.
		assertPostings(t, p, firm, tok, []string{"scope-granted", "scope-parking"})
	})

	t.Run("and never the organisation's own postings", func(t *testing.T) {
		// The org-level row carries no property. is_delegated_unit() would match
		// a portfolio scope on a NULL property, which is why the policy requires
		// a property outright rather than passing NULL through.
		for _, got := range visiblePostings(t, p, firm, tok) {
			if got == "scope-orglevel" {
				t.Fatal("a firm can see the owner's statutory position — a NULL property is being " +
					"treated as \"anywhere\" rather than as \"not delegated\"")
			}
		}
	})

	t.Run("the owner sees all of it", func(t *testing.T) {
		assertPostings(t, p, owner, tok,
			[]string{"scope-granted", "scope-orglevel", "scope-parking", "scope-sibling"})
	})

	t.Run("an unrelated organisation sees none of it", func(t *testing.T) {
		assertPostings(t, p, tenancy.With(ctx, OrgOutsider), tok, nil)
	})

	t.Run("a firm cannot post into the owner's ledger under a read mandate", func(t *testing.T) {
		err := tenancy.Scoped(firm, p, func(ctx context.Context, tx pgx.Tx) error {
			_, err := writeEntry(ctx, tx, OrgOwner, "invoice", "firm-"+tok, []posting{
				{account: "tenant_receivable", side: "debit", amount: 100,
					property: PropertyGranted, unit: UnitGrantedA, party: "tenant", partyID: tenantParty},
				{account: "rent_income", side: "credit", amount: 100,
					property: PropertyGranted, unit: UnitGrantedA, party: "owner", partyID: ownerParty},
			})
			return err
		})
		if err == nil {
			t.Fatal("a money.read mandate raised a charge in the owner's ledger — " +
				"USING and WITH CHECK are asking for the same permission")
		}
	})

	t.Run("revocation closes the window and keeps the history", func(t *testing.T) {
		revoke(t, plat, grant)
		assertPostings(t, p, firm, tok, nil)
		assertPostings(t, p, owner, tok,
			[]string{"scope-granted", "scope-orglevel", "scope-parking", "scope-sibling"})
	})
}

// balance asks the derived view what an account stands at. There is no other
// way to ask: no table in this schema holds a balance.
func balance(t *testing.T, p tenancy.Pool, as context.Context, account, party string) int64 {
	t.Helper()
	var out int64
	err := tenancy.Scoped(as, p, func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT coalesce(sum(balance_minor), 0) FROM ledger_balances
			 WHERE account_code = $1 AND party_id = $2`, account, party).Scan(&out)
	})
	if err != nil {
		t.Fatalf("reading the %s balance: %v", account, err)
	}
	return out
}

func assertPostings(t *testing.T, p tenancy.Pool, as context.Context, tok string, want []string) {
	t.Helper()
	got := visiblePostings(t, p, as, tok)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("visible postings %v, want %v", got, want)
	}
}

// visiblePostings lists the seeded entries a session can see, by their memo
// prefix — an exact set rather than a count, because a count of two passes when
// the two are the wrong two.
func visiblePostings(t *testing.T, p tenancy.Pool, as context.Context, tok string) []string {
	t.Helper()
	var out []string
	err := tenancy.Scoped(as, p, func(ctx context.Context, tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT DISTINCT memo FROM ledger_postings
			 WHERE memo LIKE 'scope-%' AND memo LIKE '%' || $1`, tok)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var memo string
			if err := rows.Scan(&memo); err != nil {
				return err
			}
			out = append(out, strings.TrimSuffix(memo, "-"+tok))
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatalf("listing visible postings: %v", err)
	}
	sort.Strings(out)
	return out
}
