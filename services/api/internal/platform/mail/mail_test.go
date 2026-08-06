package mail

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTheRequestCarriesTheMessageAndTheKey(t *testing.T) {
	t.Parallel()
	var (
		gotPath string
		gotAuth string
		gotType string
		body    map[string]any
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotType = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Content-Type")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"re_1"}`))
	}))
	t.Cleanup(srv.Close)

	s := &sender{cfg: Resend{APIKey: "re_test", From: "Dwellm8 <no-reply@dwellm8.com>"}, endpoint: srv.URL + "/emails", client: srv.Client()}
	err := s.Send(context.Background(), Message{
		To: "manager@example.test", Subject: "Your Dwellm8 code", Body: "123456 is your code.",
	})
	if err != nil {
		t.Fatalf("sending: %v", err)
	}

	if gotPath != "/emails" {
		t.Errorf("Resend takes the message at /emails, got %q", gotPath)
	}
	if gotAuth != "Bearer re_test" {
		t.Errorf("the key is a bearer token, got %q", gotAuth)
	}
	if gotType != "application/json" {
		t.Errorf("the body is JSON, got %q", gotType)
	}
	if body["from"] != "Dwellm8 <no-reply@dwellm8.com>" {
		t.Errorf("the configured sender must be used, got %v", body["from"])
	}
	to, _ := body["to"].([]any)
	if len(to) != 1 || to[0] != "manager@example.test" {
		t.Errorf("the recipient must be a list of one, got %v", body["to"])
	}
	if body["subject"] != "Your Dwellm8 code" || body["text"] != "123456 is your code." {
		t.Errorf("subject and text must survive, got %v / %v", body["subject"], body["text"])
	}
}

func TestARefusalFromResendIsAnErrorRatherThanASentMail(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"domain is not verified"}`))
	}))
	t.Cleanup(srv.Close)

	s := &sender{cfg: Resend{APIKey: "re_test", From: "a@b.test"}, endpoint: srv.URL + "/emails", client: srv.Client()}
	err := s.Send(context.Background(), Message{To: "c@d.test", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("a 422 from the provider is not a sent mail")
	}
	if !strings.Contains(err.Error(), "422") || !strings.Contains(err.Error(), "domain is not verified") {
		t.Fatalf("the error must carry the status and what Resend said, got %v", err)
	}
}

func TestAnUnconfiguredSenderSaysSoRatherThanSwallowingTheMail(t *testing.T) {
	t.Parallel()
	// Silently dropping is the failure that looks like a working sign-up until
	// somebody waits ten minutes for a code that was never sent.
	err := New(Resend{}).Send(context.Background(), Message{To: "a@b.test", Subject: "s", Body: "b"})
	if err == nil {
		t.Fatal("an unconfigured mailer must refuse, not pretend")
	}
	if !strings.Contains(err.Error(), "no mail") {
		t.Fatalf("the refusal must name the cause, got %v", err)
	}
}

func TestConfiguredReadsTheCredentialsThemselves(t *testing.T) {
	t.Parallel()
	if (Resend{}).Configured() {
		t.Error("nothing configured is not configured")
	}
	if (Resend{APIKey: "re_test"}).Configured() {
		t.Error("a key with no sender address cannot send")
	}
	if (Resend{From: "no-reply@dwellm8.com"}).Configured() {
		t.Error("a sender address with no key cannot send")
	}
	if !(Resend{APIKey: "re_test", From: "no-reply@dwellm8.com"}).Configured() {
		t.Error("a key and a sender is all Resend needs")
	}
}

func TestACancelledContextStopsTheRequest(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s := &sender{cfg: Resend{APIKey: "re_test", From: "a@b.test"}, endpoint: srv.URL + "/emails", client: srv.Client()}
	if err := s.Send(ctx, Message{To: "c@d.test", Subject: "s", Body: "b"}); err == nil {
		t.Fatal("a cancelled request must not report a sent mail")
	}
}
