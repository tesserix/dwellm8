package docurl_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/docurl"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The revocation semantics against PostgreSQL: a closed window stays closed,
// the first terminal state wins, and the reason vocabulary is the table's,
// not the caller's.

func storePool(t *testing.T) tenancy.Pool {
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
	return req
}

func freshTxn(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "txn-" + hex.EncodeToString(b)
}

func TestRevocationClosesTheWindowOnce(t *testing.T) {
	st := docurl.NewStore(storePool(t))
	ctx := context.Background()
	g := docurl.Grant{Org: isolationtest.OrgOwner, DocumentRef: "lease/doc-1",
		TxnID: freshTxn(t), ExpiresAt: time.Now().Add(time.Hour)}

	open, err := st.Revoked(ctx, g)
	if err != nil || open {
		t.Fatalf("an unrevoked transaction reads revoked=%v, err=%v", open, err)
	}

	if err := st.Revoke(ctx, g.Org, g.TxnID, "completed"); err != nil {
		t.Fatal(err)
	}
	// A second terminal report is a fact of webhook life, not an error — and
	// it must not overwrite the first reason.
	if err := st.Revoke(ctx, g.Org, g.TxnID, "cancelled"); err != nil {
		t.Fatalf("a repeated revocation must be absorbed: %v", err)
	}

	closed, err := st.Revoked(ctx, g)
	if err != nil || !closed {
		t.Fatalf("a revoked transaction reads revoked=%v, err=%v", closed, err)
	}

	if err := st.Revoke(ctx, g.Org, freshTxn(t), "signer changed their mind"); err == nil {
		t.Fatal("a reason outside the vocabulary must be refused by the table")
	}
}

func TestTheAccessLogTakesBothOutcomes(t *testing.T) {
	st := docurl.NewStore(storePool(t))
	ctx := context.Background()
	g := docurl.Grant{Org: isolationtest.OrgOwner, DocumentRef: "lease/doc-1",
		TxnID: freshTxn(t), ExpiresAt: time.Now().Add(time.Hour)}

	for _, outcome := range []string{"served", "refused"} {
		if err := st.Log(ctx, g, "203.0.113.7", "esp-agent", outcome); err != nil {
			t.Fatalf("logging %s: %v", outcome, err)
		}
	}
	if err := st.Log(ctx, g, "203.0.113.7", "esp-agent", "maybe"); err == nil {
		t.Fatal("an outcome outside the vocabulary must be refused by the table")
	}
}
