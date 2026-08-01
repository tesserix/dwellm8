package isolationtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0019. The listing site is the first read path in this schema that does not fail
// closed, so the tests are about how narrow the hole is rather than that it exists.
//
// An anonymous visitor is an *unscoped* session — no app.tenant_id — which is precisely
// the state ADR-0003 designed every other policy to deny. So the assertions here are: a
// stranger sees published live listings, sees nothing else anywhere, and cannot write.

// anonymously runs a query with no tenant set, which is what the listing site does.
func anonymously[T any](t *testing.T, p tenancy.Pool, sql string, args ...any) T {
	t.Helper()
	ctx := context.Background()
	tx, err := p.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var out T
	if err := tx.QueryRow(ctx, sql, args...).Scan(&out); err != nil {
		t.Fatalf("anonymous query: %v", err)
	}
	return out
}

// seedListing puts one listing in each state and returns the live published one's id.
func seedListing(t *testing.T, plat tenancy.PlatformPool, token string) (live, draft string) {
	t.Helper()
	ctx := context.Background()
	prop, unitA := seedLeaseUnit(t, plat)
	// The second unit must be in the *same* property: listings carries a composite
	// (unit_id, property_id) foreign key, so a unit from another building does not pair
	// with this one.
	var unitB string
	if err := tenancy.Platform(ctx, plat, "seeding a second unit", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO units (id, tenant_id, property_id, unit_kind, code, carpet_area_sqft)
			VALUES (gen_random_uuid(), $1, $2, 'flat', $3, 700) RETURNING id`,
			isolationtest.OrgA.String(), prop, "LIST-B-"+token).Scan(&unitB)
	}); err != nil {
		t.Fatalf("seeding a second unit: %v", err)
	}
	err := tenancy.Platform(ctx, plat, "seeding listings", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO listings (tenant_id, property_id, unit_id, state, published_at, headline,
			                      locality, city, state_code, rent_minor, costs_confirmed)
			VALUES ($1, $2, $3, 'live', now(), $4, 'Indiranagar', 'Bengaluru', 'KA', 2500000, true)
			RETURNING id`,
			isolationtest.OrgA.String(), prop, unitA, "Live "+token).Scan(&live); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO listings (tenant_id, property_id, unit_id, state, headline,
			                      locality, city, state_code, rent_minor)
			VALUES ($1, $2, $3, 'draft', $4, 'Indiranagar', 'Bengaluru', 'KA', 2500000)
			RETURNING id`,
			isolationtest.OrgA.String(), prop, unitB, "Draft "+token).Scan(&draft)
	})
	if err != nil {
		t.Fatalf("seeding listings: %v", err)
	}
	return live, draft
}

// The public read path, and its edges. A stranger sees a published live listing, and does
// not see a draft — publication is an act by the owner, not a default.
func TestAnAnonymousVisitorSeesPublishedListingsAndNothingElse(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)

	tok := randomToken(t)
	live, draft := seedListing(t, plat, tok)

	if n := anonymously[int](t, p,
		`SELECT count(*) FROM listings WHERE id = $1`, live); n != 1 {
		t.Errorf("an anonymous visitor sees %d live listings, want 1 — the listing site would be "+
			"empty", n)
	}
	if n := anonymously[int](t, p,
		`SELECT count(*) FROM listings WHERE id = $1`, draft); n != 0 {
		t.Errorf("an anonymous visitor sees a draft listing — it would be advertised before its " +
			"owner published it")
	}

	// Unpublishing removes it immediately, which is what makes publication an act rather
	// than a flag somebody set once.
	if err := tenancy.Platform(context.Background(), plat, "pausing", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE listings SET state = 'paused' WHERE id = $1`, live)
		return err
	}); err != nil {
		t.Fatalf("pausing: %v", err)
	}
	if n := anonymously[int](t, p,
		`SELECT count(*) FROM listings WHERE id = $1`, live); n != 0 {
		t.Error("a paused listing is still publicly visible")
	}
}

// The hole is one table wide. Every other table still denies an unscoped session, which
// is what stops a public listing site becoming a public database.
func TestAnAnonymousVisitorSeesNothingButListings(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)
	tok := randomToken(t)
	seedListing(t, plat, tok)

	// The tables a listing points at, and the ones a curious visitor would try next.
	for _, table := range []string{
		"properties", "units", "organisations", "leases", "rent_schedule",
		"payments", "journal_entries", "ledger_postings", "kyc_verifications",
		"prospects", "enquiries", "contact_bridges", "property_ownership",
	} {
		t.Run(table, func(t *testing.T) {
			n := anonymously[int](t, p, `SELECT count(*) FROM `+table)
			if n != 0 {
				t.Errorf("an anonymous visitor sees %d rows in %s — the public listing branch is "+
					"supposed to be the only one", n, table)
			}
		})
	}
}

// The hole is read-only by construction: publishing is a write, and a write always needs
// a tenant.
func TestAnAnonymousVisitorCannotWriteAListing(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)
	tok := randomToken(t)
	live, _ := seedListing(t, plat, tok)

	ctx := context.Background()
	tx, err := p.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `UPDATE listings SET rent_minor = 1 WHERE id = $1`, live); err == nil {
		var rent int64
		_ = tenancy.Platform(context.Background(), plat, "checking", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT rent_minor FROM listings WHERE id = $1`, live).Scan(&rent)
		})
		if rent == 1 {
			t.Error("an anonymous visitor edited a listing's rent")
		}
	}
	// costs_confirmed is set so the refusal below is the policy's, not the
	// disclosure constraint's — this asserts RLS, not #134.
	if _, err := tx.Exec(ctx, `
		INSERT INTO listings (tenant_id, property_id, unit_id, state, headline, locality, city,
		                      state_code, rent_minor, costs_confirmed)
		VALUES ($1, gen_random_uuid(), gen_random_uuid(), 'live', 'Forged', 'X', 'Y', 'KA', 1, true)`,
		isolationtest.OrgA.String()); err == nil {
		t.Error("an anonymous visitor created a listing")
	}
}

// The verification point. Browsing and shortlisting are anonymous; making contact is not,
// and that is the only thing standing between an owner and a thousand fake enquiries.
func TestAnEnquiryNeedsAVerifiedProspect(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)
	tok := randomToken(t)
	live, _ := seedListing(t, plat, tok)

	newProspect := func(verified bool) string {
		t.Helper()
		var id string
		err := tenancy.Platform(ctx, plat, "creating a prospect", func(ctx context.Context, tx pgx.Tx) error {
			if verified {
				return tx.QueryRow(ctx, `
					INSERT INTO prospects (token_hash, verified_at, contact_ref, contact_masked)
					VALUES (sha256($1::bytea), now(), $2, 'XXXXXX4321') RETURNING id`,
					"tok-"+randomToken(t), "exotel-"+randomToken(t)).Scan(&id)
			}
			return tx.QueryRow(ctx, `
				INSERT INTO prospects (token_hash) VALUES (sha256($1::bytea)) RETURNING id`,
				"tok-"+randomToken(t)).Scan(&id)
		})
		if err != nil {
			t.Fatalf("creating a prospect: %v", err)
		}
		return id
	}

	enquire := func(prospect string) error {
		return tenancy.Platform(ctx, plat, "enquiring", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO enquiries (tenant_id, listing_id, prospect_id, kind)
				VALUES ($1, $2, $3, 'inspection')`,
				isolationtest.OrgA.String(), live, prospect)
			return err
		})
	}

	unverified := newProspect(false)
	if err := enquire(unverified); err == nil {
		t.Fatal("an unverified prospect booked an inspection — an owner would receive a thousand " +
			"of these before lunch")
	} else if !strings.Contains(err.Error(), "verified phone number") {
		t.Errorf("refused, but not by the verification point: %v", err)
	}

	verified := newProspect(true)
	if err := enquire(verified); err != nil {
		t.Errorf("a verified prospect could not book an inspection: %v", err)
	}

	// Half-verified is refused: a prospect who can be called and not displayed, or
	// displayed and not called, is a bug in whatever wrote them.
	err := tenancy.Platform(ctx, plat, "half-verifying", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO prospects (token_hash, verified_at) VALUES (sha256($1::bytea), now())`,
			"tok-"+randomToken(t))
		return err
	})
	if err == nil {
		t.Error("a prospect was verified with no contact reference and no masked form")
	}
}

// Neither number is exposed, and the bridge only opens once both sides have engaged.
func TestAContactBridgeNeedsBothSidesToHaveEngaged(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)
	seedCollectionProperties(t, plat)
	tok := randomToken(t)
	live, _ := seedListing(t, plat, tok)

	var prospect, enquiry string
	if err := tenancy.Platform(ctx, plat, "enquiring", func(ctx context.Context, tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO prospects (token_hash, verified_at, contact_ref, contact_masked)
			VALUES (sha256($1::bytea), now(), $2, 'XXXXXX4321') RETURNING id`,
			"tok-"+tok, "exotel-"+tok).Scan(&prospect); err != nil {
			return err
		}
		return tx.QueryRow(ctx, `
			INSERT INTO enquiries (tenant_id, listing_id, prospect_id, kind)
			VALUES ($1, $2, $3, 'enquiry') RETURNING id`,
			isolationtest.OrgA.String(), live, prospect).Scan(&enquiry)
	}); err != nil {
		t.Fatalf("enquiring: %v", err)
	}

	open := func() error {
		return tenancy.Platform(ctx, plat, "opening a bridge", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO contact_bridges (tenant_id, enquiry_id, provider, provider_ref,
				                             proxy_masked, expires_at)
				VALUES ($1, $2, 'exotel', $3, 'XXXXXX9876', now() + interval '7 days')`,
				isolationtest.OrgA.String(), enquiry, "br-"+tok)
			return err
		})
	}

	// The enquiry is 'new': the prospect engaged, the owner has not.
	if err := open(); err == nil {
		t.Fatal("a contact bridge opened on an unanswered enquiry — a prospect could dial an owner " +
			"who never replied")
	} else if !strings.Contains(err.Error(), "both sides have engaged") {
		t.Errorf("refused, but not by the mutual-engagement rule: %v", err)
	}

	if err := tenancy.Platform(ctx, plat, "responding", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE enquiries SET state = 'owner_responded' WHERE id = $1`, enquiry)
		return err
	}); err != nil {
		t.Fatalf("responding: %v", err)
	}
	if err := open(); err != nil {
		t.Errorf("a bridge could not be opened after the owner responded: %v", err)
	}

	// And no raw number is anywhere in the row. The provider holds both; this holds two
	// references and two masks.
	var masked, ref string
	if err := tenancy.Platform(ctx, plat, "reading back", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT proxy_masked, provider_ref FROM contact_bridges WHERE enquiry_id = $1`,
			enquiry).Scan(&masked, &ref)
	}); err != nil {
		t.Fatalf("reading back: %v", err)
	}
	if !strings.HasPrefix(masked, "XXXXXX") {
		t.Errorf("the proxy number %q is not masked", masked)
	}
}
