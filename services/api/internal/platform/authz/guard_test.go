package authz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

type fakeChecker struct {
	allowed bool
	err     error
	calls   int
}

func (f *fakeChecker) Check(ctx context.Context, user, relation, object string) (bool, error) {
	f.calls++
	return f.allowed, f.err
}

func request(t *testing.T, party string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	if party != "" {
		r = r.WithContext(tenancy.WithResident(r.Context(), tenancy.ResidentID(party)))
	}
	return r
}

func serve(g *Guard, c Check, r *http.Request) *httptest.ResponseRecorder {
	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	rec := httptest.NewRecorder()
	g.wrap(c, ok).ServeHTTP(rec, r)
	return rec
}

func staticObject(s string) func(*http.Request) (string, error) {
	return func(*http.Request) (string, error) { return s, nil }
}

func TestGuardAllowsWhatTheCheckerAllows(t *testing.T) {
	g := &Guard{Checker: &fakeChecker{allowed: true}, Enforce: true}
	rec := serve(g, Check{Relation: "can_view", Object: staticObject("agreement:a1")}, request(t, "p1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("allowed check answered %d", rec.Code)
	}
}

func TestGuardDeniesWhatTheCheckerDenies(t *testing.T) {
	g := &Guard{Checker: &fakeChecker{allowed: false}, Enforce: true}
	rec := serve(g, Check{Relation: "can_view", Object: staticObject("agreement:a1")}, request(t, "p1"))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("denied check answered %d", rec.Code)
	}
}

// The failure scenario dwellm8#150 names: an unreachable checker is a deny,
// and no code path falls back to permitting the action.
func TestGuardFailsClosed(t *testing.T) {
	cases := map[string]struct {
		g *Guard
		c Check
		r *http.Request
	}{
		"checker error": {
			g: &Guard{Checker: &fakeChecker{allowed: true, err: errors.New("unreachable")}, Enforce: true},
			c: Check{Relation: "can_view", Object: staticObject("agreement:a1")},
			r: request(t, "p1"),
		},
		"no subject": {
			g: &Guard{Checker: &fakeChecker{allowed: true}, Enforce: true},
			c: Check{Relation: "can_view", Object: staticObject("agreement:a1")},
			r: request(t, ""),
		},
		"no object": {
			g: &Guard{Checker: &fakeChecker{allowed: true}, Enforce: true},
			c: Check{Relation: "can_view", Object: func(*http.Request) (string, error) { return "", errors.New("missing") }},
			r: request(t, "p1"),
		},
	}
	for name, tc := range cases {
		if rec := serve(tc.g, tc.c, tc.r); rec.Code != http.StatusForbidden {
			t.Errorf("%s: answered %d, want 403", name, rec.Code)
		}
	}
}

func TestGuardOffPassesThroughWithoutChecking(t *testing.T) {
	f := &fakeChecker{}
	g := &Guard{Checker: f, Enforce: false}
	rec := serve(g, Check{Relation: "can_view", Object: staticObject("agreement:a1")}, request(t, ""))
	if rec.Code != http.StatusOK || f.calls != 0 {
		t.Fatalf("disabled guard: code=%d calls=%d", rec.Code, f.calls)
	}
}

// The cache's contract: within TTL a revoked relationship may still answer
// from cache; after TTL it must be re-asked.
func TestCacheBoundsRevocation(t *testing.T) {
	f := &fakeChecker{allowed: true}
	g := &Guard{Checker: f, Enforce: true, Cache: NewCache(30*time.Millisecond, 10)}
	c := Check{Relation: "can_view", Object: staticObject("agreement:a1")}

	serve(g, c, request(t, "p1"))
	f.allowed = false // the tuple is deleted
	if rec := serve(g, c, request(t, "p1")); rec.Code != http.StatusOK {
		t.Fatalf("inside TTL the cached allow should hold, got %d", rec.Code)
	}
	if f.calls != 1 {
		t.Fatalf("second request should not re-check inside TTL, calls=%d", f.calls)
	}

	time.Sleep(40 * time.Millisecond)
	if rec := serve(g, c, request(t, "p1")); rec.Code != http.StatusForbidden {
		t.Fatalf("past TTL the revocation must be honoured, got %d", rec.Code)
	}
}

func TestOpenRouteMustStateItsReason(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("an Open route with no reason must panic at wiring time")
		}
	}()
	NewRegistrar(http.NewServeMux(), &Guard{}).Open("GET /x", "", func(http.ResponseWriter, *http.Request) {})
}

func TestOrganisationObjectComesFromContextNotHeaders(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/x", nil)
	r.Header.Set("X-Dwellm8-Org", "attacker-org")
	if _, err := Organisation()(r); err == nil {
		t.Fatal("no tenancy in context must be an error, whatever the headers say")
	}
	r = r.WithContext(tenancy.With(r.Context(), tenancy.ID("org-1")))
	obj, err := Organisation()(r)
	if err != nil || obj != "organisation:org-1" {
		t.Fatalf("got %q, %v", obj, err)
	}
}
