package push

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The sender against a fake Expo: batch shape on the wire, and the dead-token
// pairing that rides ticket order.

func TestSendReportsDeadTokens(t *testing.T) {
	var got []Message
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decoding: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":[
			{"status":"ok"},
			{"status":"error","details":{"error":"DeviceNotRegistered"}},
			{"status":"error","details":{"error":"MessageRateExceeded"}}
		]}`))
	}))
	t.Cleanup(srv.Close)

	msgs := []Message{
		{To: "ExponentPushToken[alive]", Body: "a"},
		{To: "ExponentPushToken[dead]", Body: "b"},
		{To: "ExponentPushToken[busy]", Body: "c"},
	}
	dead, err := New(srv.URL).Send(context.Background(), msgs)
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(got) != 3 || got[0].To != "ExponentPushToken[alive]" {
		t.Fatalf("wire = %+v", got)
	}
	// Only DeviceNotRegistered is dead; a rate limit is Expo's bad day.
	if len(dead) != 1 || dead[0] != "ExponentPushToken[dead]" {
		t.Fatalf("dead = %v, want exactly the unregistered one", dead)
	}
}

func TestSendBatches(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var in []Message
		_ = json.NewDecoder(r.Body).Decode(&in)
		if len(in) > 100 {
			t.Errorf("batch of %d, the ceiling is 100", len(in))
		}
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	t.Cleanup(srv.Close)

	msgs := make([]Message, 150)
	for i := range msgs {
		msgs[i] = Message{To: "ExponentPushToken[x]"}
	}
	if _, err := New(srv.URL).Send(context.Background(), msgs); err != nil {
		t.Fatalf("send: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2 batches for 150", calls)
	}
}
