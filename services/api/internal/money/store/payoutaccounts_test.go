package store_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/money/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The impersonated-owner control (#227) against PostgreSQL: a change survives
// as history, enters a cool-off the payout must respect, and cannot cover its
// tracks by changing back.

func paPools(t *testing.T) (tenancy.Pool, tenancy.PlatformPool) {
	t.Helper()
	dsn, plat := os.Getenv("TEST_DATABASE_URL"), os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" || plat == "" {
		t.Skip("TEST_DATABASE_URL and TEST_PLATFORM_DATABASE_URL are not set")
	}
	req, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(req.Close)
	p, err := pgxpool.New(context.Background(), plat)
	if err != nil {
		t.Fatalf("connecting as platform: %v", err)
	}
	t.Cleanup(p.Close)
	isolationtest.SeedPropertyTree(t, tenancy.NewPlatformPool(p))
	return req, tenancy.NewPlatformPool(p)
}

func ownerCtx() context.Context {
	return tenancy.With(context.Background(), isolationtest.OrgOwner)
}

// newID is a fresh party per test, so cool-off state cannot leak between them.
func newID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// backdate moves every row of one fingerprint into the past, standing in for
// the three days this test will not wait.
func backdate(t *testing.T, plat tenancy.PlatformPool, owner, fp string, d time.Duration) {
	t.Helper()
	err := tenancy.Platform(context.Background(), plat, "test: aging a payout account",
		func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				UPDATE payout_accounts SET valid_from = valid_from - $3::interval
				 WHERE owner_party_id = $1 AND account_fp = $2`,
				owner, fp, d.String())
			return err
		})
	if err != nil {
		t.Fatalf("backdating: %v", err)
	}
}

func change(t *testing.T, s *store.PayoutAccounts, owner, masked, fp string) store.Changed {
	t.Helper()
	out, err := s.Change(ownerCtx(), store.AccountChange{
		OwnerPartyID: owner, Masked: masked, IFSC: "HDFC0001234", Fingerprint: fp})
	if err != nil {
		t.Fatalf("changing to %s: %v", masked, err)
	}
	return out
}

func TestANewAccountIsHeldForTheCoolOffAndTheOldOneSurvives(t *testing.T) {
	pool, plat := paPools(t)
	s := store.NewPayoutAccounts(pool)
	owner := newID(t)

	change(t, s, owner, "XX1111", "fp-old")
	backdate(t, plat, owner, "fp-old", 100*time.Hour)

	p, err := s.Payable(ownerCtx(), owner)
	if err != nil || p.Held {
		t.Fatalf("a 100-hour-old account must be payable: %+v %v", p, err)
	}

	out := change(t, s, owner, "XX2222", "fp-new")
	if out.OldMasked != "XX1111" {
		t.Fatalf("the change did not report the account it replaced: %+v", out)
	}

	p, err = s.Payable(ownerCtx(), owner)
	if err != nil || !p.Held {
		t.Fatalf("a fresh account inside its cool-off must be a hold: %+v %v", p, err)
	}
	if p.Masked != "XX2222" || time.Until(p.PayableAt) < 71*time.Hour {
		t.Fatalf("the hold must say which account and until when: %+v", p)
	}

	// The previous account survives as a closed row - it is what the
	// notification names and what "where was March's rent sent" reads.
	var closed int
	err = tenancy.Platform(context.Background(), plat, "test: reading history",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				SELECT count(*) FROM payout_accounts
				 WHERE owner_party_id = $1 AND valid_to IS NOT NULL`, owner).Scan(&closed)
		})
	if err != nil || closed != 1 {
		t.Fatalf("the replaced account must survive as history: closed=%d err=%v", closed, err)
	}
}

// The changed-back edge: an attacker who reverts to the long-standing account
// resumes payouts to it - and gains nothing, because their own account's
// cool-off is keyed on first appearance, not on the latest row.
func TestChangingBackNeitherStrandsTheOwnerNorHelpsTheAttacker(t *testing.T) {
	pool, plat := paPools(t)
	s := store.NewPayoutAccounts(pool)
	owner := newID(t)

	change(t, s, owner, "XX1111", "fp-old")
	backdate(t, plat, owner, "fp-old", 100*time.Hour)
	change(t, s, owner, "XX2222", "fp-attacker")
	change(t, s, owner, "XX1111", "fp-old") // the revert

	p, err := s.Payable(ownerCtx(), owner)
	if err != nil || p.Held {
		t.Fatalf("reverting to the long-standing account must pay at once: %+v %v", p, err)
	}

	change(t, s, owner, "XX2222", "fp-attacker") // the attacker tries again
	p, err = s.Payable(ownerCtx(), owner)
	if err != nil || !p.Held {
		t.Fatalf("re-entering the attacker's account must not shorten its cool-off: %+v %v", p, err)
	}
}

func TestTheChangeIsAuditedAndPublished(t *testing.T) {
	pool, plat := paPools(t)
	s := store.NewPayoutAccounts(pool)
	owner := newID(t)

	change(t, s, owner, "XX1111", "fp-a")
	out := change(t, s, owner, "XX2222", "fp-b")

	var audits, outboxed int
	err := tenancy.Platform(context.Background(), plat, "test: reading the trail",
		func(ctx context.Context, tx pgx.Tx) error {
			if err := tx.QueryRow(ctx, `
				SELECT count(*) FROM audit_events
				 WHERE action = 'payout_account.changed' AND subject_id = $1
				   AND detail->>'old_masked' = 'XX1111' AND detail->>'new_masked' = 'XX2222'`,
				out.ID).Scan(&audits); err != nil {
				return err
			}
			return tx.QueryRow(ctx, `
				SELECT count(*) FROM outbox
				 WHERE type = 'money.payout_account.changed' AND subject_id = $1`,
				out.ID).Scan(&outboxed)
		})
	if err != nil || audits != 1 || outboxed != 1 {
		t.Fatalf("the change must be audited and published: audits=%d outboxed=%d err=%v",
			audits, outboxed, err)
	}
}

func TestNamingTheSameAccountIsNotAChange(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewPayoutAccounts(pool)
	owner := newID(t)

	change(t, s, owner, "XX1111", "fp-a")
	out := change(t, s, owner, "XX1111", "fp-a")
	if !out.Unchanged {
		t.Fatalf("naming the account on file must be a no-op, got %+v", out)
	}
}

func TestAnOwnerWithNoAccountIsAnError(t *testing.T) {
	pool, _ := paPools(t)
	s := store.NewPayoutAccounts(pool)

	_, err := s.Payable(ownerCtx(), newID(t))
	if err == nil {
		t.Fatal("no account on file must be an error the payout run can name")
	}
}
