package isolationtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0008 against PostgreSQL, which is where the parts that matter live: an
// exclusion constraint cannot be tested in Go, and neither can the concurrency it
// exists for.
//
// The Go package tests the arithmetic of half-open intervals. These test the three
// things only the database can promise — that two revisions of the same subject
// cannot both be live, that a retired row does not block its own replacement, and
// that history cannot be edited in place.

func TestPropertyOwnershipIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "property_ownership",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			// The owner party carries the run token, for the reason the harness's own
			// comment gives: it commits, so a row from the last run would be counted
			// by this one and the first assertion would fail for a reason that has
			// nothing to do with isolation.
			//
			// It also carries the organisation, and that is the subtler half. The
			// exclusion constraint keys on the party, so a shared party would make
			// the "writing into another organisation is refused" subtest fail on the
			// constraint rather than on the policy — passing for the wrong reason,
			// which is the same as not testing it.
			_, err := tx.Exec(ctx, `
				INSERT INTO property_ownership (tenant_id, property_id, owner_party_id,
				                                valid_from, valid_to)
				VALUES ($1, $2, $3::uuid, '2020-01-01', '2020-02-01')`,
				tenant.String(), collectionProperty(tenant), ownershipParty(tenant, token))
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx, `
				SELECT count(*) FROM property_ownership WHERE owner_party_id::text LIKE $1`,
				"%"+token[:8]).Scan(&n)
			return n, err
		},
	})
}

// The story's primary and failure scenarios, on a real table.
//
// Two adjacent intervals are accepted and an as-of query answers correctly on both
// sides of the boundary; an overlapping one is refused by the constraint rather than
// by anybody remembering to check.
func TestOwnershipAnswersAsOfAndRefusesOverlap(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedOwnershipUnit(t, plat)
	party := "aaaaaaaa-0000-4000-8000-" + randomToken(t)[:12]

	// Owned by one party until 1 April, then by them again on a revised share —
	// the shape of a change: the predecessor closed, the successor open-ended.
	if err := tenancy.Platform(ctx, plat, "recording a change of ownership", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
			                                share_bps, valid_from, valid_to)
			VALUES ($1, $2, $3, $4, 10000, '2025-01-01', '2026-04-01')`,
			isolationtest.OrgA.String(), prop, unit, party); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
			                                share_bps, valid_from)
			VALUES ($1, $2, $3, $4, 5000, '2026-04-01')`,
			isolationtest.OrgA.String(), prop, unit, party)
		return err
	}); err != nil {
		t.Fatalf("two adjacent intervals were refused: %v — the upper bound is exclusive, so they "+
			"meet rather than overlap", err)
	}

	// The as-of question, from the same table, on both sides of the boundary and on
	// the boundary itself.
	asOf := func(on string) (int, string) {
		t.Helper()
		var share int
		var from string
		err := tenancy.Platform(ctx, plat, "as-of", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT share_bps, to_char(valid_from, 'YYYY-MM-DD') FROM property_ownership
				 WHERE unit_id = $1 AND retired_at IS NULL AND validity @> $2::date`,
				unit, on).Scan(&share, &from)
		})
		if err != nil {
			t.Fatalf("as-of %s: %v", on, err)
		}
		return share, from
	}

	for _, tc := range []struct {
		on        string
		wantShare int
	}{
		{"2026-03-15", 10000},
		{"2026-04-15", 5000},
		// The boundary. 31 March is the last day of the old row because the upper
		// bound is exclusive, and 1 April is the first day of the new one.
		{"2026-03-31", 10000},
		{"2026-04-01", 5000},
		// Open-ended, far out. The generated range is unbounded above without
		// anybody writing "valid_to IS NULL".
		{"2099-12-31", 5000},
	} {
		if got, from := asOf(tc.on); got != tc.wantShare {
			t.Errorf("on %s the share is %d (row from %s), want %d", tc.on, got, from, tc.wantShare)
		}
	}

	// Exactly one row is live on any given day, which is the invariant. Asserted by
	// the query above returning a single row — a second would have made Scan fail —
	// and by the constraint refusing the overlap that would create one.
	err := tenancy.Platform(ctx, plat, "writing an overlap", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
			                                valid_from, valid_to)
			VALUES ($1, $2, $3, $4, '2026-01-01', '2026-06-01')`,
			isolationtest.OrgA.String(), prop, unit, party)
		return err
	})
	if err == nil {
		t.Fatal("an interval overlapping two existing ones was accepted — an as-of query for " +
			"March now has two answers and nothing says which")
	}
	if !strings.Contains(err.Error(), "property_ownership_no_overlap_unit") {
		t.Errorf("refused, but not by the exclusion constraint: %v", err)
	}
	t.Logf("refused: %v", err)
}

// ADR-0008 §5's edge case, in the database. A correction retires the row it replaces
// and takes the *same* interval — so the retired row must not block it, which is what
// the constraint's WHERE clause is for.
func TestACorrectionReplacesTheSameIntervalAndTheRetiredRowDoesNotBlockIt(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedOwnershipUnit(t, plat)
	party := "bbbbbbbb-0000-4000-8000-" + randomToken(t)[:12]

	var wrongID string
	if err := tenancy.Platform(ctx, plat, "the wrong row", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
			                                share_bps, valid_from)
			VALUES ($1, $2, $3, $4, 10000, '2025-01-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit, party).Scan(&wrongID)
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	// The share was always 6000; somebody typed 10000. Retire and replace over the
	// same interval, in one transaction — the constraint is deferred to nothing, so
	// the retire has to land before the replacement is inserted.
	if err := tenancy.Platform(ctx, plat, "correcting", func(ctx context.Context, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx,
			`UPDATE property_ownership SET retired_at = now() WHERE id = $1`, wrongID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
			                                share_bps, valid_from, corrects)
			VALUES ($1, $2, $3, $4, 6000, '2025-01-01', $5)`,
			isolationtest.OrgA.String(), prop, unit, party, wrongID)
		return err
	}); err != nil {
		t.Fatalf("a correction over the same interval was refused: %v — a retired row must not "+
			"block its own replacement, which is what the constraint's WHERE clause is for", err)
	}

	// An as-of query for the earlier period now gives the corrected answer, because
	// it was always the corrected answer. That is what distinguishes a correction
	// from a change.
	var share int
	var corrects *string
	if err := tenancy.Platform(ctx, plat, "reading back", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT share_bps, corrects::text FROM property_ownership
			 WHERE unit_id = $1 AND retired_at IS NULL AND validity @> '2025-06-01'::date`,
			unit).Scan(&share, &corrects)
	}); err != nil {
		t.Fatalf("reading back: %v — more than one live row would fail here", err)
	}
	if share != 6000 {
		t.Errorf("after a correction, June 2025 says %d, want 6000", share)
	}
	if corrects == nil || *corrects != wrongID {
		t.Error("the replacement does not name the row it corrects, so the two are indistinguishable " +
			"from an ordinary change")
	}

	// The wrong row is kept. "We thought the share was 10000 until Tuesday" is
	// occasionally the answer to a dispute.
	var retired int
	if err := tenancy.Platform(ctx, plat, "counting retired", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT count(*) FROM property_ownership WHERE unit_id = $1 AND retired_at IS NOT NULL`,
			unit).Scan(&retired)
	}); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if retired != 1 {
		t.Errorf("%d retired rows, want 1 — a correction keeps what we believed", retired)
	}
}

// History is not editable. The two writes effective dating needs are closing an open
// interval and retiring a row; everything else has to be a new row, or the table has
// no history at all.
func TestOwnershipHistoryCannotBeEditedInPlace(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedOwnershipUnit(t, plat)
	party := "cccccccc-0000-4000-8000-" + randomToken(t)[:12]

	var id string
	if err := tenancy.Platform(ctx, plat, "seeding", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
			                                share_bps, valid_from)
			VALUES ($1, $2, $3, $4, 10000, '2025-01-01') RETURNING id`,
			isolationtest.OrgA.String(), prop, unit, party).Scan(&id)
	}); err != nil {
		t.Fatalf("seeding: %v", err)
	}

	edit := func(set string, args ...any) error {
		return tenancy.Platform(ctx, plat, "editing history", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `UPDATE property_ownership SET `+set+` WHERE id = $1`,
				append([]any{id}, args...)...)
			return err
		})
	}

	for _, tc := range []struct {
		name string
		set  string
		args []any
	}{
		{"the share, which would rewrite what we said it was", "share_bps = $2", []any{6000}},
		{"the owner, which would give the flat to somebody retrospectively", "owner_party_id = $2",
			[]any{"dddddddd-0000-4000-8000-000000000001"}},
		{"the start date, which would move a boundary something has reported on", "valid_from = $2",
			[]any{"2024-01-01"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := edit(tc.set, tc.args...); err == nil {
				t.Errorf("accepted an in-place edit of %s", tc.name)
			} else if !strings.Contains(err.Error(), "may not be edited in place") {
				t.Errorf("refused, but not by the append-only trigger: %v", err)
			}
		})
	}

	// The two writes that are permitted, and each only once.
	if err := edit("valid_to = '2026-04-01'"); err != nil {
		t.Fatalf("closing an open interval was refused: %v", err)
	}
	if err := edit("valid_to = '2026-05-01'"); err == nil {
		t.Error("an already-closed interval was re-closed, moving a boundary something downstream " +
			"has already reported on")
	}
	if err := edit("retired_at = now()"); err != nil {
		t.Fatalf("retiring a row was refused: %v", err)
	}
	if err := edit("retired_at = NULL"); err == nil {
		t.Error("a retired row was un-retired, which would make it live again alongside its replacement")
	}

	// And nothing may be deleted, whoever asks.
	err := tenancy.Platform(ctx, plat, "deleting history", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `DELETE FROM property_ownership WHERE id = $1`, id)
		return err
	})
	if err == nil {
		var n int
		_ = tenancy.Platform(ctx, plat, "counting", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM property_ownership WHERE id = $1`, id).Scan(&n)
		})
		if n == 0 {
			t.Error("an ownership row was deleted — the record of who owned a flat is what a " +
				"dispute turns on")
		}
	}
}

// A row true for no days at all satisfies no as-of query while looking like it
// should. The exclusive upper bound makes that shape expressible, so it is refused.
func TestAnEmptyIntervalIsRefused(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	prop, unit := seedOwnershipUnit(t, plat)

	for _, tc := range []struct{ name, from, to string }{
		{"an interval that ends where it starts", "2026-04-01", "2026-04-01"},
		{"an interval that ends before it starts", "2026-04-01", "2026-03-01"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tenancy.Platform(ctx, plat, "writing an empty interval", func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `
					INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
					                                valid_from, valid_to)
					VALUES ($1, $2, $3, gen_random_uuid(), $4::date, $5::date)`,
					isolationtest.OrgA.String(), prop, unit, tc.from, tc.to)
				return err
			})
			if err == nil {
				t.Errorf("accepted %s", tc.name)
			}
		})
	}

	// A single day is [1 April, 2 April), which is the shape the exclusive bound
	// makes people doubt.
	if err := tenancy.Platform(ctx, plat, "a single day", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO property_ownership (tenant_id, property_id, unit_id, owner_party_id,
			                                valid_from, valid_to)
			VALUES ($1, $2, $3, gen_random_uuid(), '2026-04-01', '2026-04-02')`,
			isolationtest.OrgA.String(), prop, unit)
		return err
	}); err != nil {
		t.Errorf("a single-day interval was refused: %v", err)
	}
}

// ownershipParty derives a per-run, per-organisation party uuid from the harness
// token, so rows are attributable to one run and one organisation.
func ownershipParty(tenant tenancy.ID, token string) string {
	return "0000000" + string(tenant)[:1] + "-0000-4000-8000-0000" + token[:8]
}

// seedOwnershipUnit creates a property and a lettable unit for one run, and returns
// their ids. A fresh unit per test, so the exclusion constraint under test is never
// tripped by another test's rows.
func seedOwnershipUnit(t *testing.T, plat tenancy.PlatformPool) (property, unit string) {
	t.Helper()
	ctx := context.Background()
	tok := randomToken(t)
	err := tenancy.Platform(ctx, plat, "seeding an ownership unit", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO properties (id, tenant_id, code, name, kind, address_line1,
			                        locality, city, state_code, pin)
			VALUES (gen_random_uuid(), $1, $2, 'Ownership Harness', 'building', '1 Harness Road',
			        'Indiranagar', 'Bengaluru', 'KA', '560038')
			RETURNING id`, isolationtest.OrgA.String(), "OWN-"+tok).Scan(&property); err != nil {
			return err
		}
		// carpet_area_sqft is required for a lettable unit — units_lettable_has_area.
		return tx.QueryRow(ctx, `
			INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
			VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 650)
			RETURNING id`, isolationtest.OrgA.String(), property, "U-"+tok).Scan(&unit)
	})
	if err != nil {
		t.Fatalf("seeding an ownership unit: %v", err)
	}
	return property, unit
}
