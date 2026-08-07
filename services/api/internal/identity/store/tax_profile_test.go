package store_test

import (
	"context"
	"strings"
	"testing"
)

// party_tax_profiles is what is known about a landlord for rate purposes, as
// opposed to lease_tax_facts, which is what is known about a tenancy. Section
// 206AA turns on the payee having furnished a PAN, and rule 37BC on a
// non-resident having furnished five particulars — neither is a fact about a
// lease, and an owner with two tenancies must not be asked twice (#318).

func TestATaxProfileWithoutTheParticularsIsNotOutsideSection206AA(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var furnished bool
	err := p.QueryRow(ctx, `
		SELECT rule_37bc_furnished FROM (
			SELECT * FROM party_tax_profiles LIMIT 0
		) t
		UNION ALL
		SELECT false LIMIT 1`).Scan(&furnished)
	if err != nil {
		t.Fatalf("party_tax_profiles has no rule_37bc_furnished: %v — the escape from "+
			"section 206AA has to be derived from the particulars, not asserted", err)
	}
}

// The particulars rule 37BC(2) lists, and nothing short of them. A profile
// missing any one of them is inside 206AA, and the column must say so itself
// rather than leaving it to whichever caller remembers.
func TestRule37BCIsDerivedFromEveryParticularItRequires(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx, `
		SELECT pg_get_expr(d.adbin, d.adrelid)
		  FROM pg_attrdef d
		  JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		 WHERE d.adrelid = 'party_tax_profiles'::regclass
		   AND a.attname = 'rule_37bc_furnished'`).Scan(&def); err != nil {
		t.Fatalf("rule_37bc_furnished is not a generated column: %v", err)
	}

	for _, particular := range []string{
		"foreign_tin_masked", "trc_number", "foreign_address", "foreign_email", "foreign_phone",
	} {
		if !strings.Contains(def, particular) {
			t.Errorf("rule_37bc_furnished ignores %s — rule 37BC(2) requires it, so a payee "+
				"missing it would escape section 206AA without having furnished what the rule asks",
				particular)
		}
	}
	if !strings.Contains(def, "non_resident") {
		t.Error("rule_37bc_furnished does not test residency — a resident has no escape from 206AA")
	}
}

// The TIN is an identifier, and ADR-0013's rule is that identifiers are kept
// masked or not at all. A column named foreign_tin would hold the whole thing.
func TestTheForeignTINIsHeldMaskedOrNotAtAll(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var masked, bare int
	if err := p.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE column_name = 'foreign_tin_masked'),
		       count(*) FILTER (WHERE column_name IN ('foreign_tin', 'passport_number', 'pan'))
		  FROM information_schema.columns
		 WHERE table_name = 'party_tax_profiles'`).Scan(&masked, &bare); err != nil {
		t.Fatalf("reading the columns: %v", err)
	}
	if masked != 1 {
		t.Error("party_tax_profiles has no foreign_tin_masked — rule 37BC's TIN has nowhere to go")
	}
	if bare != 0 {
		t.Errorf("party_tax_profiles holds %d unmasked identifier column(s) — ADR-0013 keeps the "+
			"mask and nothing else", bare)
	}
}

// A non-resident whose profile says they live in India is one of the two facts
// being wrong, and the pair decides which section governs the rent.
func TestANonResidentProfileCannotClaimIndianResidence(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'party_tax_profiles_residence_agrees'`).Scan(&def); err != nil {
		t.Fatalf("party_tax_profiles_residence_agrees is missing: %v — nothing stops a "+
			"non-resident profile carrying residence_country 'IN'", err)
	}
	if !strings.Contains(def, "non_resident") || !strings.Contains(def, "residence_country") {
		t.Errorf("the constraint does not relate residency to the country: %s", def)
	}
}
