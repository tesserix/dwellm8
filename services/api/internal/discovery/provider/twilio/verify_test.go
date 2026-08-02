package twilio

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The adapter against a fake Twilio: credentials on the wire, the approved and
// mismatch answers, and the 404-means-expired mapping.

func fake(t *testing.T, status int, body string) (*Verifier, *[]string) {
	t.Helper()
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if user != "AC123" || pass != "secret" {
			t.Errorf("credentials on the wire = %q, %q", user, pass)
		}
		_ = r.ParseForm()
		seen = append(seen, r.URL.Path+"?"+r.PostForm.Encode())
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New("AC123", "secret", "VA9", srv.URL), &seen
}

func TestStartAndConfirm(t *testing.T) {
	v, seen := fake(t, 200, `{"status":"approved"}`)
	if err := v.Start(context.Background(), "+919845000000"); err != nil {
		t.Fatalf("start: %v", err)
	}
	ref, masked, err := v.Confirm(context.Background(), "+919845000000", "123456")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !strings.HasPrefix(ref, "twv-") || masked != "XXXXXX0000" {
		t.Fatalf("ref %q masked %q", ref, masked)
	}
	if strings.Contains(ref, "9845") {
		t.Fatal("the contact reference leaks the number")
	}
	if len(*seen) != 2 || !strings.Contains((*seen)[0], "Verifications?Channel=sms") ||
		!strings.Contains((*seen)[1], "VerificationCheck?Code=123456") {
		t.Fatalf("requests = %v", *seen)
	}
}

func TestWrongCode(t *testing.T) {
	v, _ := fake(t, 200, `{"status":"pending"}`)
	if _, _, err := v.Confirm(context.Background(), "+919845000000", "999999"); !errors.Is(err, ErrCodeMismatch) {
		t.Fatalf("pending = %v, want ErrCodeMismatch", err)
	}
}

func TestExpiredCodeIs404(t *testing.T) {
	v, _ := fake(t, 404, `{"code":20404}`)
	if _, _, err := v.Confirm(context.Background(), "+919845000000", "123456"); !errors.Is(err, ErrCodeMismatch) {
		t.Fatalf("404 = %v, want ErrCodeMismatch — expiry is the person's retry, not an outage", err)
	}
}
