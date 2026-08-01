package docurl

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/authz"
)

// fakeGrants watches what the handler records, which is most of what it must
// get right: every fetch logged, refused ones included, forged ones excluded.
type fakeGrants struct {
	revoked    bool
	revokedErr error
	logErr     error
	logged     []loggedFetch
}

type loggedFetch struct {
	g       Grant
	ip, ua  string
	outcome string
}

func (f *fakeGrants) Revoked(_ context.Context, g Grant) (bool, error) {
	return f.revoked, f.revokedErr
}

func (f *fakeGrants) Log(_ context.Context, g Grant, ip, ua, outcome string) error {
	if f.logErr != nil {
		return f.logErr
	}
	f.logged = append(f.logged, loggedFetch{g: g, ip: ip, ua: ua, outcome: outcome})
	return nil
}

type fakeSource struct {
	data []byte
	err  error
}

func (f fakeSource) Open(context.Context, string) ([]byte, string, error) {
	return f.data, "application/pdf", f.err
}

func serve(t *testing.T, h *Handler, token string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.Routes(authz.NewRegistrar(mux, &authz.Guard{}))
	req := httptest.NewRequest("GET", "/v1/esign/documents/"+token, nil)
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	req.Header.Set("User-Agent", "esp-agent")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func handler(t *testing.T, g *fakeGrants, src Source) (*Handler, string) {
	t.Helper()
	s := signer(t)
	h := NewHandler(s, g, src, slog.New(slog.DiscardHandler))
	tok, err := s.Token(Grant{Org: "org-1", DocumentRef: "d-1", TxnID: "t-1",
		ExpiresAt: time.Now().Add(10 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	return h, tok
}

// The primary scenario: a live grant serves the document, the fetch is logged
// against its transaction, and the headers keep the URL from outliving itself.
func TestALiveGrantServesAndIsLogged(t *testing.T) {
	grants := &fakeGrants{}
	h, tok := handler(t, grants, fakeSource{data: []byte("%PDF-1.7")})

	rec := serve(t, h, tok)
	if rec.Code != http.StatusOK || rec.Body.String() != "%PDF-1.7" {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	for k, want := range map[string]string{
		"Cache-Control":   "no-store",
		"Referrer-Policy": "no-referrer",
		"Content-Type":    "application/pdf",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s: got %q, want %q", k, got, want)
		}
	}
	if len(grants.logged) != 1 {
		t.Fatalf("logged %d fetches, want 1", len(grants.logged))
	}
	l := grants.logged[0]
	if l.outcome != "served" || l.g.TxnID != "t-1" || l.ip != "203.0.113.7" || l.ua != "esp-agent" {
		t.Fatalf("logged %+v", l)
	}
}

// The failure scenario: revoked, expired, tampered and forged all answer with
// one status and one body — a distinguishable refusal is an oracle.
func TestEveryRefusalLooksTheSame(t *testing.T) {
	grants := &fakeGrants{}
	h, tok := handler(t, grants, fakeSource{data: []byte("x")})

	expired, _ := h.signer.Token(Grant{Org: "org-1", DocumentRef: "d-1", TxnID: "t-old",
		ExpiresAt: time.Now().Add(-time.Minute)})

	revokedGrants := &fakeGrants{revoked: true}
	hRevoked, tokRevoked := handler(t, revokedGrants, fakeSource{data: []byte("x")})

	answers := map[string]*httptest.ResponseRecorder{
		"revoked":  serve(t, hRevoked, tokRevoked),
		"expired":  serve(t, h, expired),
		"tampered": serve(t, h, strings.Replace(tok, ".", "A.", 1)),
		"forged":   serve(t, h, "bm9wZQ.bm9wZQ"),
	}
	for name, rec := range answers {
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, rec.Code)
		}
		if rec.Body.String() != answers["forged"].Body.String() {
			t.Errorf("%s: body %q differs from the one refusal", name, rec.Body.String())
		}
	}

	// The revoked and expired attempts are evidence; the forged one is a
	// stranger and writes nothing into anybody's log.
	if len(revokedGrants.logged) != 1 || revokedGrants.logged[0].outcome != "refused" {
		t.Errorf("revoked fetch logged as %+v", revokedGrants.logged)
	}
	if len(grants.logged) != 1 || grants.logged[0].g.TxnID != "t-old" || grants.logged[0].outcome != "refused" {
		t.Errorf("expired fetch logged as %+v, want one refused row for t-old", grants.logged)
	}
}

// Accountability is not best-effort: a fetch that cannot be recorded is not
// served, and a revocation list that cannot be read might hide a revocation.
func TestTheHandlerFailsClosed(t *testing.T) {
	h, tok := handler(t, &fakeGrants{logErr: errors.New("db down")}, fakeSource{data: []byte("x")})
	if rec := serve(t, h, tok); rec.Code != http.StatusNotFound {
		t.Fatalf("unloggable fetch served: %d", rec.Code)
	}

	h, tok = handler(t, &fakeGrants{revokedErr: errors.New("db down")}, fakeSource{data: []byte("x")})
	if rec := serve(t, h, tok); rec.Code != http.StatusNotFound {
		t.Fatalf("unreadable revocation list served: %d", rec.Code)
	}

	h, tok = handler(t, &fakeGrants{}, fakeSource{err: errors.New("no such object")})
	if rec := serve(t, h, tok); rec.Code != http.StatusNotFound {
		t.Fatalf("source failure disclosed: %d", rec.Code)
	}

	// Wired before #62: no source at all refuses rather than panicking.
	h, tok = handler(t, &fakeGrants{}, nil)
	if rec := serve(t, h, tok); rec.Code != http.StatusNotFound {
		t.Fatalf("nil source: %d", rec.Code)
	}
}
