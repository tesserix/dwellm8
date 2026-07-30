package store_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/pii"
)

// ADR-0013's two copies: the mask patterns exist in internal/platform/pii, because Go
// produces the mask, and in kyc_verifications_reference_is_a_mask, because the column is
// what actually refuses a full identifier.
//
// A mask Go can produce and the column would refuse is a verification that fails at the
// counter, with a customer present. A mask the column accepts and Go would not is worse:
// it means something other than Go wrote one, and the whole point is that nothing can.

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the KYC contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestTheGoMaskPatternsMatchTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'kyc_verifications_reference_is_a_mask'`).Scan(&def); err != nil {
		t.Fatalf("reading kyc_verifications_reference_is_a_mask: %v — it is the constraint that "+
			"makes a full identifier unstorable", err)
	}

	for _, k := range pii.Kinds() {
		pattern, err := pii.MaskPattern(k)
		if err != nil {
			t.Fatalf("pattern for %s: %v", k, err)
		}
		// The kind must appear in the CASE at all — a kind the constraint does not
		// mention falls through to NULL, and a CHECK that evaluates to NULL passes.
		// That is the quiet failure this loop exists for.
		if !strings.Contains(def, "'"+string(k)+"'") {
			t.Errorf("kind %q is producible in Go and is not in the mask constraint — a CHECK that "+
				"evaluates to NULL passes, so that kind would accept anything", k)
			continue
		}
		// And the pattern the schema uses for it must be the one Go generates. Compared
		// after normalising the doubled backslashes pg_get_constraintdef emits.
		normalised := strings.ReplaceAll(def, `\\`, `\`)
		if !strings.Contains(normalised, pattern) {
			t.Errorf("the schema's pattern for %s is not Go's %q", k, pattern)
		}
	}
	t.Logf("constraint: %s", def)
}

// Evaluated rather than read: PostgreSQL's own regex engine against Go's, over the values
// that matter — a full identifier, a proper mask, and a mask that keeps too much.
func TestTheDatabaseRefusesAFullIdentifier(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	for _, tc := range []struct {
		name, kind, value string
		want              bool
	}{
		{"a full Aadhaar number", "aadhaar", "234567890123", false},
		{"a proper Aadhaar mask", "aadhaar", "XXXXXXXX0123", true},
		{"a mask that keeps eight of twelve", "aadhaar", "XXXX567890123", false},
		{"a full PAN", "pan", "ABCDE1234F", false},
		{"a proper PAN mask", "pan", "XXXXXX234F", true},
		{"an IFSC, which is stored as it is", "ifsc", "HDFC0001234", true},
		{"a GSTIN, which is a public register", "gstin", "27ABCDE1234F1Z5", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The constraint's own expression, applied to this value.
			var accepted bool
			err := p.QueryRow(ctx, `
				SELECT CASE $1::text
					WHEN 'aadhaar'         THEN $2::text ~ '^X+[0-9A-Z]{4}$'
					WHEN 'pan'             THEN $2::text ~ '^X+[0-9A-Z]{4}$'
					WHEN 'bank_account'    THEN $2::text ~ '^X+[0-9A-Z]{4}$'
					WHEN 'passport'        THEN $2::text ~ '^X+[0-9A-Z]{3}$'
					WHEN 'driving_licence' THEN $2::text ~ '^X+[0-9A-Z]{4}$'
					WHEN 'voter_id'        THEN $2::text ~ '^X+[0-9A-Z]{4}$'
					WHEN 'ifsc'            THEN $2::text ~ '^[A-Z]{4}0[A-Z0-9]{6}$'
					WHEN 'gstin'           THEN $2::text ~ '^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][0-9A-Z][Z][0-9A-Z]$'
					WHEN 'upi_vpa'         THEN $2::text ~ '^[a-zA-Z0-9.\-_]{2,256}@[a-zA-Z]{2,64}$'
				END`, tc.kind, tc.value).Scan(&accepted)
			if err != nil {
				t.Fatalf("evaluating: %v", err)
			}
			if accepted != tc.want {
				t.Errorf("the database accepts %q for %s = %v, want %v", tc.value, tc.kind, accepted, tc.want)
			}
			// Go must agree, which is the other half of the contract.
			pattern, _ := pii.MaskPattern(pii.Kind(tc.kind))
			if inGo := regexp.MustCompile(pattern).MatchString(tc.value); inGo != tc.want {
				t.Errorf("Go accepts %q for %s = %v, want %v", tc.value, tc.kind, inGo, tc.want)
			}
		})
	}
}

// The allowlist assertion 15 enforces, asserted from the Go side too so the failure names
// the field rather than the bootstrap.
func TestTheKYCTableHoldsNothingElse(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	rows, err := p.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		 WHERE table_schema = 'public' AND table_name = 'kyc_verifications'
		 ORDER BY column_name`)
	if err != nil {
		t.Fatalf("reading columns: %v", err)
	}
	defer rows.Close()

	allowed := map[string]bool{
		"id": true, "tenant_id": true, "subject_party_id": true, "kind": true,
		"masked_reference": true, "result": true, "provider": true, "provider_txn_id": true,
		"consent_artefact_id": true, "verified_at": true, "expires_at": true, "created_at": true,
	}
	var got []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan: %v", err)
		}
		got = append(got, c)
		if !allowed[c] {
			t.Errorf("kyc_verifications has column %q, which ADR-0013 does not list — a completed "+
				"verification holds the result, a masked reference, the provider and its "+
				"transaction, a timestamp and the consent artefact", c)
		}
	}
	for c := range allowed {
		found := false
		for _, g := range got {
			if g == c {
				found = true
			}
		}
		if !found {
			t.Errorf("kyc_verifications is missing %q", c)
		}
	}
	t.Logf("columns: %v", got)
}

// Every result Go can produce, against the CHECK. A result the schema refuses is a failed
// verification that cannot be recorded — and a verification that can only be stored when
// it succeeded is one nobody can audit.
func TestTheGoResultVocabularyIsAcceptedByTheSchema(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'kyc_verifications_result_check'`).Scan(&def); err != nil {
		t.Fatalf("reading kyc_verifications_result_check: %v", err)
	}
	for _, r := range pii.Results() {
		if !strings.Contains(def, "'"+string(r)+"'") {
			t.Errorf("result %q is producible in Go and refused by the schema", r)
		}
	}

	var kinds string
	if err := p.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'kyc_verifications_kind_check'`).Scan(&kinds); err != nil {
		t.Fatalf("reading kyc_verifications_kind_check: %v", err)
	}
	for _, k := range pii.Kinds() {
		if !strings.Contains(kinds, "'"+string(k)+"'") {
			t.Errorf("kind %q is producible in Go and refused by the schema", k)
		}
	}
}
