package activity_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/activity"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The feed against PostgreSQL: events and notes interleave newest-first, the
// keyset cursor pages without loss, and the resident audience is blind to
// financial facts and org-only notes — #196's two scenarios, on real rows.

func feedPool(t *testing.T) tenancy.Pool {
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

func ownerCtx() context.Context {
	return tenancy.With(context.Background(), isolationtest.OrgOwner)
}

func freshLease(t *testing.T) string {
	t.Helper()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return "lease-" + hex.EncodeToString(b)
}

// seedEvent writes one outbox fact through the same append every module uses.
func seedEvent(t *testing.T, pool tenancy.Pool, typ, subjectKind, subjectID string, at time.Time) {
	t.Helper()
	err := tenancy.Scoped(ownerCtx(), pool, func(ctx context.Context, tx pgx.Tx) error {
		e, err := events.New(typ, isolationtest.OrgOwner.String(),
			events.Subject{Kind: subjectKind, ID: subjectID},
			events.Actor{Kind: events.ActorSystem}, map[string]string{"seed": "test"})
		if err != nil {
			return err
		}
		e.OccurredAt = at
		e.ID = events.NewULID(at)
		return events.Append(ctx, tx, e)
	})
	if err != nil {
		t.Fatalf("seeding %s: %v", typ, err)
	}
}

func TestTheFeedInterleavesNewestFirstAndPages(t *testing.T) {
	pool := feedPool(t)
	st := activity.NewStore(pool)
	lease := freshLease(t)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

	seedEvent(t, pool, "lease.tenancy.started", "lease", lease, base)
	seedEvent(t, pool, "lease.notice.served", "lease", lease, base.Add(20*time.Minute))
	if _, err := st.AddNote(ownerCtx(), activity.Note{
		SubjectKind: "lease", SubjectID: lease,
		Author: "test", Body: "spoke to the tenant", Visibility: "org",
	}); err != nil {
		t.Fatal(err)
	}

	all, err := st.Feed(ownerCtx(), activity.Query{SubjectKind: "lease", SubjectID: lease})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d entries, want 3", len(all))
	}
	// The note was written last and leads; the facts follow newest-first.
	if all[0].Kind != "note" || all[1].Kind != "lease.notice.served" || all[2].Kind != "lease.tenancy.started" {
		t.Fatalf("order: %s, %s, %s", all[0].Kind, all[1].Kind, all[2].Kind)
	}

	// Page by twos: the second page continues exactly where the first ended.
	page1, err := st.Feed(ownerCtx(), activity.Query{SubjectKind: "lease", SubjectID: lease, Limit: 2})
	if err != nil || len(page1) != 2 {
		t.Fatalf("page 1: %d entries, %v", len(page1), err)
	}
	page2, err := st.Feed(ownerCtx(), activity.Query{
		SubjectKind: "lease", SubjectID: lease, Limit: 2,
		BeforeAt: page1[1].OccurredAt, BeforeID: page1[1].ID,
	})
	if err != nil || len(page2) != 1 {
		t.Fatalf("page 2: %d entries, %v", len(page2), err)
	}
	if page2[0].Kind != "lease.tenancy.started" {
		t.Fatalf("page 2 holds %s", page2[0].Kind)
	}
}

func TestARenterSeesLifecycleAndSharedNotesOnly(t *testing.T) {
	pool := feedPool(t)
	st := activity.NewStore(pool)
	lease := freshLease(t)
	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Millisecond)

	seedEvent(t, pool, "lease.tenancy.started", "lease", lease, base)
	// A financial fact wrongly attached to the lease subject must still not
	// reach the renter: the allowlist is on type, not only on subject.
	seedEvent(t, pool, "money.payout_account.changed", "lease", lease, base.Add(time.Minute))
	for vis, body := range map[string]string{"org": "owner asked about fees", "shared": "plumber booked friday"} {
		if _, err := st.AddNote(ownerCtx(), activity.Note{
			SubjectKind: "lease", SubjectID: lease, Author: "mgr", Body: body, Visibility: vis,
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := st.Feed(ownerCtx(), activity.Query{
		SubjectKind: "lease", SubjectID: lease, Audience: activity.AudienceResident,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("a renter sees %d entries, want 2 (lifecycle + shared note): %+v", len(got), got)
	}
	for _, e := range got {
		switch {
		case e.Kind == "note" && e.Body == "plumber booked friday":
		case e.Kind == "lease.tenancy.started":
			if e.Data != nil {
				t.Fatal("a renter must not receive event payloads")
			}
		default:
			t.Fatalf("a renter saw %s %q", e.Kind, e.Body)
		}
	}
}
