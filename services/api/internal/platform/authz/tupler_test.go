package authz

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/events"
)

func started(t *testing.T) events.Envelope {
	t.Helper()
	env, err := events.New("lease.tenancy.started", "org-1",
		events.Subject{Kind: "lease", ID: "l-1"},
		events.Actor{Kind: events.ActorUser, ID: "owner"},
		map[string]any{
			"unit_id": "u-9",
			"parties": []map[string]string{
				{"party_id": "p-tenant", "role": "tenant"},
				{"party_id": "p-owner", "role": "owner"},
				{"party_id": "p-guardian", "role": "guardian"},
			},
		})
	if err != nil {
		t.Fatal(err)
	}
	return env
}

func TestStartedTenancyWritesTheResidentEdges(t *testing.T) {
	writes, deletes, err := TuplesFor(started(t))
	if err != nil || len(deletes) != 0 {
		t.Fatalf("err=%v deletes=%v", err, deletes)
	}
	want := []Tuple{
		{User: "unit:u-9", Relation: "unit", Object: "agreement:l-1"},
		{User: "user:p-tenant", Relation: "tenant", Object: "agreement:l-1"},
		{User: "user:p-guardian", Relation: "guardian", Object: "agreement:l-1"},
	}
	if len(writes) != len(want) {
		t.Fatalf("got %v", writes)
	}
	for i, w := range want {
		if writes[i] != w {
			t.Errorf("write %d: got %+v want %+v", i, writes[i], w)
		}
	}
}

// The owner reaches the agreement through the organisation and, delegated,
// through the mandate — a direct edge would survive an ownership transfer.
func TestOwnersGetNoDirectEdge(t *testing.T) {
	writes, _, _ := TuplesFor(started(t))
	for _, w := range writes {
		if w.User == "user:p-owner" {
			t.Fatalf("owner got a direct edge: %+v", w)
		}
	}
}

func TestEndedTenancyDeletesTheSameEdges(t *testing.T) {
	env := started(t)
	env.Type = "lease.tenancy.ended"
	writes, deletes, err := TuplesFor(env)
	if err != nil || len(writes) != 0 || len(deletes) != 3 {
		t.Fatalf("writes=%v deletes=%v err=%v", writes, deletes, err)
	}
}

func TestAnUnknownEventMovesNoEdges(t *testing.T) {
	env, _ := events.New("money.payment.captured", "org-1",
		events.Subject{Kind: "payment", ID: "p-1"}, events.Actor{Kind: events.ActorSystem}, nil)
	writes, deletes, err := TuplesFor(env)
	if err != nil || writes != nil || deletes != nil {
		t.Fatalf("writes=%v deletes=%v err=%v", writes, deletes, err)
	}
}

func TestAKnownEventThatCannotDecodeIsAnError(t *testing.T) {
	env := started(t)
	env.Data = json.RawMessage(`{"parties": "not-a-list"}`)
	if _, _, err := TuplesFor(env); err == nil {
		t.Fatal("an undecodable relationship event must error, not silently move nothing")
	}
}

// The reconciliation read sees exactly what the projector wrote — the
// round-trip the rebuild path depends on.
func TestReadTuplesReturnsWhatWasWritten(t *testing.T) {
	f := &fakeFGA{stores: map[string]string{}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.Bootstrap(context.Background(), "dwellm8"); err != nil {
		t.Fatal(err)
	}

	writes, _, _ := TuplesFor(started(t))
	if err := c.WriteTuples(context.Background(), writes, nil); err != nil {
		t.Fatal(err)
	}
	got, err := c.ReadTuples(context.Background(), "agreement:l-1")
	if err != nil || len(got) != len(writes) {
		t.Fatalf("got %v err=%v, want the %d written tuples", got, err, len(writes))
	}
}

// At-least-once delivery: applying the same event twice must land in the same
// place, which the client guarantees by reading duplicate-write and
// missing-delete refusals as success.
func TestWriteTuplesIsIdempotent(t *testing.T) {
	f := &fakeFGA{stores: map[string]string{}}
	srv := httptest.NewServer(f.handler())
	defer srv.Close()
	c := NewClient(srv.URL)
	if err := c.Bootstrap(context.Background(), "dwellm8"); err != nil {
		t.Fatal(err)
	}

	ts := []Tuple{{User: "user:p", Relation: "tenant", Object: "agreement:l"}}
	if err := c.WriteTuples(context.Background(), ts, nil); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := c.WriteTuples(context.Background(), ts, nil); err != nil {
		t.Fatalf("second write of the same tuple: %v", err)
	}
	if err := c.WriteTuples(context.Background(), nil, ts); err != nil {
		t.Fatalf("first delete: %v", err)
	}
	if err := c.WriteTuples(context.Background(), nil, ts); err != nil {
		t.Fatalf("second delete of the same tuple: %v", err)
	}
}
