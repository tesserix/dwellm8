package docscan_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/identity/docscan"
)

type visionCall struct {
	auth  string
	query string
	body  map[string]any
}

func vision(t *testing.T, status int, reply string) (*docscan.Vision, *visionCall) {
	t.Helper()
	seen := &visionCall{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.auth = r.Header.Get("Authorization")
		seen.query = r.URL.RawQuery
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &seen.body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, reply)
	}))
	t.Cleanup(srv.Close)

	v := docscan.NewVision(func(context.Context) (string, error) { return "a-token", nil })
	v.Endpoint = srv.URL
	return v, seen
}

func TestTheImageIsSentForDenseTextAndTheTextComesBack(t *testing.T) {
	v, seen := vision(t, http.StatusOK,
		`{"responses":[{"fullTextAnnotation":{"text":"INCOME TAX\nABCPD1234E"}}]}`)

	got, err := v.Text(context.Background(), []byte("jpeg-bytes"))
	if err != nil {
		t.Fatalf("calling vision: %v", err)
	}
	if !strings.Contains(got, "ABCPD1234E") {
		t.Errorf("the annotation came back as %q", got)
	}

	request := seen.body["requests"].([]any)[0].(map[string]any)
	if enc := request["image"].(map[string]any)["content"].(string); enc !=
		base64.StdEncoding.EncodeToString([]byte("jpeg-bytes")) {
		t.Errorf("the image was not sent as it was given: %q", enc)
	}
	feature := request["features"].([]any)[0].(map[string]any)["type"]
	if feature != "DOCUMENT_TEXT_DETECTION" {
		t.Errorf("asked for %v — a machine-readable zone needs the dense-text model", feature)
	}
}

// A URL is logged by every hop between here and Google, and this one has an
// identity document behind it.
func TestTheCredentialTravelsInTheHeaderAndNotTheQueryString(t *testing.T) {
	v, seen := vision(t, http.StatusOK, `{"responses":[{"fullTextAnnotation":{"text":"x"}}]}`)

	if _, err := v.Text(context.Background(), []byte("jpeg-bytes")); err != nil {
		t.Fatalf("calling vision: %v", err)
	}
	if seen.auth != "Bearer a-token" {
		t.Errorf("the authorization header is %q", seen.auth)
	}
	if strings.Contains(seen.query, "a-token") || strings.Contains(seen.query, "key=") {
		t.Errorf("the credential is in the query string: %q", seen.query)
	}
}

// Vision answers 200 with a per-image error, so a caller that only checks the
// status reads a refused image as a blank document.
func TestARefusedImageIsAnErrorEvenThoughTheCallSucceeded(t *testing.T) {
	v, _ := vision(t, http.StatusOK, `{"responses":[{"error":{"message":"Bad image data"}}]}`)

	_, err := v.Text(context.Background(), []byte("jpeg-bytes"))
	if err == nil {
		t.Fatal("a refused image read as a document with no text in it")
	}
	if !strings.Contains(err.Error(), "Bad image data") {
		t.Errorf("the refusal does not say why: %v", err)
	}
}

func TestAFailedCallIsNotReadAsAnEmptyDocument(t *testing.T) {
	for _, c := range []struct {
		what   string
		status int
		reply  string
	}{
		{"a rejected credential", http.StatusUnauthorized, `{"error":{"message":"nope"}}`},
		{"a quota refusal", http.StatusTooManyRequests, `{}`},
		{"an empty answer", http.StatusOK, `{"responses":[]}`},
		{"something that is not JSON", http.StatusOK, `<html>`},
	} {
		v, _ := vision(t, c.status, c.reply)
		if _, err := v.Text(context.Background(), []byte("jpeg-bytes")); err == nil {
			t.Errorf("%s came back as a document with no text in it", c.what)
		}
	}
}

func TestAMissingCredentialStopsTheCallRatherThanMakingItAnonymously(t *testing.T) {
	v, seen := vision(t, http.StatusOK, `{"responses":[{"fullTextAnnotation":{"text":"x"}}]}`)
	v.Token = func(context.Context) (string, error) { return "", io.ErrUnexpectedEOF }

	if _, err := v.Text(context.Background(), []byte("jpeg-bytes")); err == nil {
		t.Fatal("the call went out with no credential")
	}
	if seen.auth != "" {
		t.Error("the request reached the endpoint anyway")
	}
}
