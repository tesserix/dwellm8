package events_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The relay against PostgreSQL, because every property worth testing here is a
// property of the claim: rows that a second replica must not take, a failure
// that must not mark anything published, and a backlog that survives the pod.

func platform(t *testing.T) tenancy.PlatformPool {
	t.Helper()
	dsn := os.Getenv("TEST_PLATFORM_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_PLATFORM_DATABASE_URL is not set")
	}
	p, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	t.Cleanup(p.Close)
	return tenancy.NewPlatformPool(p)
}

func discard() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// seedOrg creates an organisation to hang events off, and removes its events
// afterwards so a re-run of the suite starts from the same place.
func seedOrg(t *testing.T, plat tenancy.PlatformPool) string {
	t.Helper()
	ctx := context.Background()
	var id string
	err := tenancy.Platform(ctx, plat, "test: seeding an organisation",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `
				INSERT INTO organisations (slug, name, kind)
				VALUES ($1, 'Relay Test', 'owner') RETURNING id`,
				"relay-"+events.NewULID(time.Now())).Scan(&id)
		})
	if err != nil {
		t.Fatalf("seeding an organisation: %v", err)
	}
	t.Cleanup(func() {
		_ = tenancy.Platform(context.Background(), plat, "test: cleaning up",
			func(ctx context.Context, tx pgx.Tx) error {
				_, err := tx.Exec(ctx, `DELETE FROM outbox WHERE tenant_id = $1`, id)
				return err
			})
	})
	return id
}

func appendEvent(t *testing.T, plat tenancy.PlatformPool, tenant, typ string) events.Envelope {
	t.Helper()
	e, err := events.New(typ, tenant,
		events.Subject{Kind: "payment", ID: "p-1"},
		events.Actor{Kind: events.ActorSystem},
		map[string]any{"amount_minor": 5866667, "currency": "INR"})
	if err != nil {
		t.Fatalf("building an event: %v", err)
	}
	err = tenancy.Platform(context.Background(), plat, "test: appending",
		func(ctx context.Context, tx pgx.Tx) error { return events.Append(ctx, tx, e) })
	if err != nil {
		t.Fatalf("appending: %v", err)
	}
	return e
}

// recorder is a Transport that remembers what it was asked to publish.
type recorder struct {
	mu   sync.Mutex
	sent []string
	ids  []string
	fail error
}

func (r *recorder) Publish(_ context.Context, subject, id string, _ []byte) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail != nil {
		return r.fail
	}
	r.sent = append(r.sent, subject)
	r.ids = append(r.ids, id)
	return nil
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.sent)
}

func published(t *testing.T, plat tenancy.PlatformPool, id string) (bool, int, string) {
	t.Helper()
	var (
		done     bool
		attempts int
		lastErr  *string
	)
	err := tenancy.Platform(context.Background(), plat, "test: reading a row",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx,
				`SELECT published_at IS NOT NULL, attempts, last_error FROM outbox WHERE id = $1`, id).
				Scan(&done, &attempts, &lastErr)
		})
	if err != nil {
		t.Fatalf("reading outbox row: %v", err)
	}
	if lastErr == nil {
		return done, attempts, ""
	}
	return done, attempts, *lastErr
}

func TestDrainPublishesAndMarks(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	e := appendEvent(t, plat, tenant, "money.payment.received")

	rec := &recorder{}
	relay := events.NewRelay(plat, rec, discard(), events.RelayConfig{})
	if _, err := relay.Drain(context.Background()); err != nil {
		t.Fatalf("draining: %v", err)
	}

	done, _, _ := published(t, plat, e.ID)
	if !done {
		t.Fatal("a published event was not marked published")
	}
	if rec.count() == 0 {
		t.Fatal("nothing reached the transport")
	}
	// The id is what the broker deduplicates on. Without it a relay retry is a
	// second event rather than the same one.
	if rec.ids[0] != e.ID {
		t.Errorf("published with id %q, want the event id %q", rec.ids[0], e.ID)
	}
	if rec.sent[0] != e.SubjectName() {
		t.Errorf("subject = %q, want %q", rec.sent[0], e.SubjectName())
	}
}

// The failure the outbox exists for: the broker is down. Nothing is published,
// nothing is marked, and the row is still there to try again.
func TestAFailedPublishLeavesTheRowUnpublished(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	e := appendEvent(t, plat, tenant, "money.payment.received")

	rec := &recorder{fail: errors.New("broker is down")}
	relay := events.NewRelay(plat, rec, discard(), events.RelayConfig{})
	if _, err := relay.Drain(context.Background()); err != nil {
		t.Fatalf("draining: %v", err)
	}

	done, attempts, lastErr := published(t, plat, e.ID)
	if done {
		t.Fatal("a row the transport rejected was marked published")
	}
	if attempts != 1 {
		t.Errorf("attempts = %d, want 1", attempts)
	}
	if lastErr == "" {
		t.Error("no error was recorded, so nobody can see why it is stuck")
	}
}

// A backed-off row is not due yet, so the next drain must leave it alone rather
// than spinning on it — that is what turns a broker outage into a quiet backlog
// instead of a hot loop.
func TestABackedOffRowIsNotReclaimedImmediately(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	appendEvent(t, plat, tenant, "money.payment.received")

	rec := &recorder{fail: errors.New("broker is down")}
	relay := events.NewRelay(plat, rec, discard(), events.RelayConfig{BaseBackoff: time.Minute})

	if n, err := relay.Drain(context.Background()); err != nil || n != 1 {
		t.Fatalf("first drain: n=%d err=%v, want 1 and no error", n, err)
	}
	if n, err := relay.Drain(context.Background()); err != nil || n != 0 {
		t.Fatalf("second drain: n=%d err=%v, want 0 — the row is backed off", n, err)
	}
}

// ADR-0002 §4: after MaxAttempts the row stops being retried automatically and
// waits for a person. It is never dropped.
func TestAnExhaustedRowStopsBeingClaimed(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	e := appendEvent(t, plat, tenant, "money.payment.received")

	rec := &recorder{fail: errors.New("broker is down")}
	relay := events.NewRelay(plat, rec, discard(), events.RelayConfig{
		MaxAttempts: 3,
		BaseBackoff: time.Nanosecond,
	})

	for i := range 3 {
		if n, err := relay.Drain(context.Background()); err != nil || n != 1 {
			t.Fatalf("drain %d: n=%d err=%v", i, n, err)
		}
	}
	if n, err := relay.Drain(context.Background()); err != nil || n != 0 {
		t.Fatalf("drain after exhaustion: n=%d err=%v, want 0", n, err)
	}

	done, attempts, _ := published(t, plat, e.ID)
	if done {
		t.Fatal("an exhausted row was marked published")
	}
	if attempts != 3 {
		t.Errorf("attempts = %d, want 3", attempts)
	}
	// Still there. A dropped event is the one outcome the outbox may not have.
	if _, _, err := relay.Lag(context.Background()); err != nil {
		t.Fatalf("lag: %v", err)
	}
}

// Two replicas drain at once. FOR UPDATE SKIP LOCKED is what stops both taking
// the same row, and a double publish is only absorbed by the broker's duplicate
// window — which is a smaller net than not publishing twice at all.
func TestTwoRelaysDoNotClaimTheSameRow(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	for range 20 {
		appendEvent(t, plat, tenant, "money.payment.received")
	}

	recA, recB := &recorder{}, &recorder{}
	a := events.NewRelay(plat, recA, discard(), events.RelayConfig{BatchSize: 20})
	b := events.NewRelay(plat, recB, discard(), events.RelayConfig{BatchSize: 20})

	var wg sync.WaitGroup
	wg.Add(2)
	for _, r := range []*events.Relay{a, b} {
		go func() {
			defer wg.Done()
			_, _ = r.Drain(context.Background())
		}()
	}
	wg.Wait()

	seen := map[string]bool{}
	for _, id := range slices.Concat(recA.ids, recB.ids) {
		if seen[id] {
			t.Errorf("event %s was published by both relays", id)
		}
		seen[id] = true
	}
	if len(seen) == 0 {
		t.Fatal("neither relay published anything")
	}
}

// The rule the whole design rests on: an event exists only if the transaction
// that wrote it committed. A rolled back transaction leaves no event, so a
// consumer can never be told about a fact that did not happen.
func TestARolledBackTransactionLeavesNoEvent(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	ctx := context.Background()

	e, err := events.New("money.payment.received", tenant,
		events.Subject{Kind: "payment", ID: "p-rollback"},
		events.Actor{Kind: events.ActorSystem}, map[string]any{})
	if err != nil {
		t.Fatalf("building: %v", err)
	}

	// Append, then fail the transaction the way a handler's later statement would.
	err = tenancy.Platform(ctx, plat, "test: a transaction that fails after appending",
		func(ctx context.Context, tx pgx.Tx) error {
			if err := events.Append(ctx, tx, e); err != nil {
				return err
			}
			return errors.New("the handler failed after appending")
		})
	if err == nil {
		t.Fatal("the transaction was expected to fail")
	}

	var n int
	err = tenancy.Platform(ctx, plat, "test: counting",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE id = $1`, e.ID).Scan(&n)
		})
	if err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 0 {
		t.Fatal("an event survived the rollback of the transaction that wrote it")
	}
}

// Appending the same id twice is a no-op rather than an error, so a handler
// retried by its caller does not fail on the event it already wrote.
func TestAppendIsIdempotentOnID(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	e := appendEvent(t, plat, tenant, "money.payment.received")

	err := tenancy.Platform(context.Background(), plat, "test: appending again",
		func(ctx context.Context, tx pgx.Tx) error { return events.Append(ctx, tx, e) })
	if err != nil {
		t.Fatalf("second append: %v", err)
	}

	var n int
	_ = tenancy.Platform(context.Background(), plat, "test: counting",
		func(ctx context.Context, tx pgx.Tx) error {
			return tx.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE id = $1`, e.ID).Scan(&n)
		})
	if n != 1 {
		t.Fatalf("the same event is in the outbox %d times", n)
	}
}

// The payload round-trips as written: an integer minor amount is still an
// integer after PostgreSQL's jsonb and back, which is the property that stops a
// rupee becoming a float somewhere downstream.
func TestThePayloadSurvivesTheRoundTrip(t *testing.T) {
	plat := platform(t)
	tenant := seedOrg(t, plat)
	appendEvent(t, plat, tenant, "money.payment.received")

	rec := &captor{}
	relay := events.NewRelay(plat, rec, discard(), events.RelayConfig{})
	if _, err := relay.Drain(context.Background()); err != nil {
		t.Fatalf("draining: %v", err)
	}
	if len(rec.bodies) == 0 {
		t.Fatal("nothing was published")
	}

	var got events.Envelope
	if err := json.Unmarshal(rec.bodies[0], &got); err != nil {
		t.Fatalf("the published body is not an envelope: %v", err)
	}
	var data struct {
		AmountMinor json.Number `json:"amount_minor"`
	}
	if err := json.Unmarshal(got.Data, &data); err != nil {
		t.Fatalf("payload: %v", err)
	}
	if data.AmountMinor.String() != "5866667" {
		t.Fatalf("amount_minor = %s, want 5866667", data.AmountMinor)
	}
	if got.TenantID != tenant {
		t.Errorf("tenant = %q, want %q", got.TenantID, tenant)
	}
	if got.Actor.Kind != events.ActorSystem {
		t.Errorf("actor = %q, want system", got.Actor.Kind)
	}
}

type captor struct {
	mu     sync.Mutex
	bodies [][]byte
}

func (c *captor) Publish(_ context.Context, _, _ string, body []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bodies = append(c.bodies, body)
	return nil
}
