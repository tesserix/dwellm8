package store_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/effective"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds"
	"github.com/tesserix/dwellm8/services/api/internal/platform/statutory/tds/store"
)

// The contract between ADR-0025's Go vocabulary and the table that holds it, and
// the two invariants that are the schema's alone.

func pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set — skipping the certificate contract")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestTheGoSectionsAreAcceptedByTheCertificateTable(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'tds_certificates_section_check'`).Scan(&def); err != nil {
		t.Fatalf("reading the section CHECK: %v — ADR-0025 requires it", err)
	}
	for _, s := range tds.Sections() {
		if !strings.Contains(def, "'"+string(s)+"'") {
			t.Errorf("section %s can hold a certificate in Go and is refused by the schema", s)
		}
	}
}

// A certificate always expires. It is the one effective-dated table here whose
// upper bound is NOT NULL, because an open-ended one would keep lowering a
// deduction for years after the officer's determination lapsed.
func TestACertificateCannotBeOpenEnded(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var nullable string
	if err := p.QueryRow(ctx, `
		SELECT is_nullable FROM information_schema.columns
		 WHERE table_name = 'tds_certificates' AND column_name = 'valid_to'`).Scan(&nullable); err != nil {
		t.Fatalf("reading valid_to: %v", err)
	}
	if nullable != "NO" {
		t.Error("tds_certificates.valid_to is nullable — an open-ended certificate would go on " +
			"lowering a deduction after the determination behind it lapsed")
	}
}

// One live certificate per landlord per section per day, or two rates would apply
// to one deduction and whichever sorted first would win.
func TestOneLiveCertificatePerPayeePerSection(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	var def string
	if err := p.QueryRow(ctx, `
		SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'tds_certificates_no_overlap'`).Scan(&def); err != nil {
		t.Fatalf("reading tds_certificates_no_overlap: %v", err)
	}
	for _, want := range []string{"tenant_id", "party_id", "section", "validity", "&&"} {
		if !strings.Contains(def, want) {
			t.Errorf("the exclusion constraint does not mention %s: %s", want, def)
		}
	}
}

// The reader returns nothing rather than an error where no certificate is held,
// because most landlords hold none and that is not a failure.
func TestNoCertificateIsNotAnError(t *testing.T) {
	ctx := context.Background()
	p := pool(t)

	tx, err := p.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, found, err := store.New(tx).ForParty(ctx,
		"00000000-0000-0000-0000-0000000000ff", tds.Section194I, effective.Day(2026, 6, 1))
	if err != nil {
		t.Fatalf("reading a party with no certificate returned an error: %v", err)
	}
	if found {
		t.Error("a certificate was found for a party that has none")
	}
}
