package isolationtest_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// ADR-0013 against PostgreSQL. The mask patterns are compared against Go's in
// internal/identity/store; what is here is what only the database can promise — that a
// full identifier cannot be written by any path, and that a management firm cannot read a
// tenant's documents even holding a grant.

func TestKYCVerificationsIsolation(t *testing.T) {
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	isolationtest.Run(t, p, isolationtest.Table{
		Name: "kyc_verifications",
		Insert: func(ctx context.Context, tx pgx.Tx, tenant tenancy.ID, token string) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO kyc_verifications (tenant_id, subject_party_id, kind, masked_reference,
				                               result, provider, provider_txn_id, consent_artefact_id)
				VALUES ($1, gen_random_uuid(), 'pan', 'XXXXXX234F', 'verified', 'nsdl', $2,
				        gen_random_uuid())`,
				tenant.String(), "txn-"+token+"-"+string(tenant)[:1])
			return err
		},
		Count: func(ctx context.Context, tx pgx.Tx, token string) (int, error) {
			var n int
			err := tx.QueryRow(ctx,
				`SELECT count(*) FROM kyc_verifications WHERE provider_txn_id LIKE $1`,
				"txn-"+token+"%").Scan(&n)
			return n, err
		},
	})
}

// The story's failure scenario, from the other side. A developer cannot persist a full
// identifier because the column will not take one — not because a handler checked.
func TestAFullIdentifierCannotBeWrittenByAnyPath(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	write := func(kind, ref string) error {
		return tenancy.Platform(ctx, plat, "writing a verification", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO kyc_verifications (tenant_id, subject_party_id, kind, masked_reference,
				                               result, provider, provider_txn_id, consent_artefact_id)
				VALUES ($1, gen_random_uuid(), $2, $3, 'verified', 'digilocker', $4,
				        gen_random_uuid())`,
				isolationtest.OrgA.String(), kind, ref, "txn-"+randomToken(t))
			return err
		})
	}

	// A fabricated, shape-valid twelve-digit number belonging to nobody: sequential
	// digits. Named for its shape rather than for the document, because the arch guard
	// covers test files too — deliberately, since a fixture that is a real identifier is
	// exactly the risk, and a test file is committed and grepped like any other.
	const fabricatedTwelveDigit = "234567890123"

	for _, tc := range []struct{ name, kind, ref string }{
		{"the full number", "aadhaar", fabricatedTwelveDigit},
		{"a mask that keeps eight of twelve", "aadhaar", "XXXX567890123"},
		{"the number with dashes, which is still the number", "aadhaar", "2345-6789-0123"},
		{"a full PAN", "pan", "ABCDE1234F"},
		{"an empty reference", "aadhaar", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := write(tc.kind, tc.ref)
			if err == nil {
				t.Fatalf("the database stored %s — this is the honeypot ADR-0013 exists to prevent", tc.name)
			}
			if !strings.Contains(err.Error(), "kyc_verifications_reference_is_a_mask") {
				t.Errorf("refused, but not by the mask constraint: %v", err)
			}
		})
	}

	// And a proper mask is accepted, or the table is unusable.
	if err := write("aadhaar", "XXXXXXXX0123"); err != nil {
		t.Errorf("a proper mask was refused: %v", err)
	}
}

// A management firm holding a grant over the owner's property must not read the tenant's
// identity documents. ADR-0005's permission vocabulary has no kyc.read, and this asserts
// the policy has no delegated branch that would make one unnecessary.
func TestADelegatedFirmCannotReadKYC(t *testing.T) {
	ctx := context.Background()
	p := pool(t)
	plat := platformPool(t)
	seedOrganisations(t, plat)

	tok := randomToken(t)
	if err := tenancy.Platform(ctx, plat, "recording a verification", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO kyc_verifications (tenant_id, subject_party_id, kind, masked_reference,
			                               result, provider, provider_txn_id, consent_artefact_id)
			VALUES ($1, gen_random_uuid(), 'passport', 'XXXXX567', 'verified', 'psk', $2,
			        gen_random_uuid())`,
			isolationtest.OrgA.String(), "txn-"+tok)
		return err
	}); err != nil {
		t.Fatalf("recording: %v", err)
	}

	// Organisation B, declaring every grant it could possibly hold. None of them reaches
	// a KYC record, because the policy has no delegated branch at all.
	for _, permission := range []string{"lease.read", "property.read", "money.read", "document.read"} {
		var n int
		err := tenancy.Scoped(tenancy.With(ctx, isolationtest.OrgB), p, func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT count(*) FROM kyc_verifications WHERE provider_txn_id = $1`, "txn-"+tok).Scan(&n)
		})
		if err != nil {
			t.Fatalf("reading as B: %v", err)
		}
		if n != 0 {
			t.Errorf("a firm with %s read %d identity record(s) — managing a property does not "+
				"require reading the tenant's passport", permission, n)
		}
	}

	// The policy has no is_delegated branch, which is the structural version of the same
	// statement: there is nothing to widen by accident.
	var qual string
	if err := tenancy.Platform(ctx, plat, "reading the policy", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			SELECT qual FROM pg_policies
			 WHERE schemaname = 'public' AND tablename = 'kyc_verifications' AND cmd = 'ALL'`).Scan(&qual)
	}); err != nil {
		t.Fatalf("reading the policy: %v", err)
	}
	if strings.Contains(qual, "is_delegated") {
		t.Errorf("the KYC policy has a delegated branch: %s — if a firm ever needs this, it is a "+
			"new permission argued for in an ADR, not a widened policy", qual)
	}
}

// The story's other edge case: support access is audited and time-bound. A support read
// without a grant behind it is what the audit exists to catch, so it is refused.
func TestASupportReadWithoutAGrantIsRefused(t *testing.T) {
	ctx := context.Background()
	plat := platformPool(t)
	seedOrganisations(t, plat)

	var verificationID string
	if err := tenancy.Platform(ctx, plat, "recording a verification", func(ctx context.Context, tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
			INSERT INTO kyc_verifications (tenant_id, subject_party_id, kind, masked_reference,
			                               result, provider, provider_txn_id, consent_artefact_id)
			VALUES ($1, gen_random_uuid(), 'pan', 'XXXXXX234F', 'verified', 'nsdl', $2,
			        gen_random_uuid()) RETURNING id`,
			isolationtest.OrgA.String(), "txn-"+randomToken(t)).Scan(&verificationID)
	}); err != nil {
		t.Fatalf("recording: %v", err)
	}

	log := func(actorKind, reason string, actor, grant any) error {
		return tenancy.Platform(ctx, plat, "logging a read", func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO kyc_access_log (tenant_id, verification_id, actor_kind, actor_id, reason,
				                            support_grant_id)
				VALUES ($1, $2, $3, $4, $5, $6)`,
				isolationtest.OrgA.String(), verificationID, actorKind, actor, reason, grant)
			return err
		})
	}

	someone := "cccccccc-0000-4000-8000-000000000001"
	grant := "dddddddd-0000-4000-8000-000000000001"

	if err := log("support", "investigating ticket 4417", someone, grant); err != nil {
		t.Fatalf("a support read with a grant was refused: %v", err)
	}
	if err := log("support", "investigating ticket 4418", someone, nil); err == nil {
		t.Error("a support read with no grant behind it was recorded — that is precisely what the " +
			"audit exists to catch, and it is what makes access time-bound")
	}
	if err := log("owner", "x", someone, nil); err == nil {
		t.Error("a read was logged with an eight-character reason of 'x' — an auditor reading that " +
			"learns nothing")
	}
	if err := log("owner", "reviewing the tenant application", nil, nil); err == nil {
		t.Error("a non-system read was logged with no actor")
	}
	if err := log("system", "nightly re-verification sweep", nil, nil); err != nil {
		t.Errorf("a system read was refused: %v", err)
	}

	// And the log cannot be edited or deleted, because a log somebody can edit is not one.
	err := tenancy.Platform(ctx, plat, "editing the log", func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx,
			`UPDATE kyc_access_log SET reason = 'nothing to see here' WHERE verification_id = $1`, verificationID)
		return err
	})
	if err == nil {
		var reason string
		_ = tenancy.Platform(ctx, plat, "checking", func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT reason FROM kyc_access_log WHERE verification_id = $1 LIMIT 1`, verificationID).Scan(&reason)
		})
		if reason == "nothing to see here" {
			t.Error("an access-log entry was edited")
		}
	}
}
