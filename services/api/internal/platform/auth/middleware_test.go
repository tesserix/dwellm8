package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
)

func ok(hit *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hit = true
		w.WriteHeader(http.StatusOK)
	})
}

func request(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/v1/leases", nil)
	if token != "" {
		r.Header.Set("Authorization", token)
	}
	return r
}

// An unauthenticated request is refused by the middleware, not passed through
// for a handler to remember to check.
func TestAnUnauthenticatedRequestNeverReachesTheHandler(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()
	v := verifier(ring, now)

	for _, c := range []struct{ name, header string }{
		{"no header at all", ""},
		{"a header with no scheme", "abc.def.ghi"},
		{"the wrong scheme", "Basic dXNlcjpwYXNz"},
		{"Bearer and nothing after it", "Bearer "},
		{"a token with a space in it", "Bearer abc def"},
		{"a forged token", m.token(t, map[string]any{"alg": "none", "kid": "k1"}, goodClaims(now))},
	} {
		t.Run(c.name, func(t *testing.T) {
			var hit bool
			rec := httptest.NewRecorder()
			auth.Middleware(v, ok(&hit)).ServeHTTP(rec, request(c.header))

			if hit {
				t.Error("the handler ran for an unauthenticated request")
			}
			if rec.Code != http.StatusUnauthorized {
				t.Errorf("answered %d, want 401", rec.Code)
			}
			if rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("no WWW-Authenticate — a client retries the same dead token forever")
			}
		})
	}

	// And the scheme is case-insensitive, because clients send "bearer".
	var hit bool
	rec := httptest.NewRecorder()
	auth.Middleware(v, ok(&hit)).ServeHTTP(rec, request("bearer "+m.token(t, nil, goodClaims(now))))
	if !hit || rec.Code != http.StatusOK {
		t.Errorf("a lower-case scheme was refused: %d", rec.Code)
	}
}

// A verified request carries its principal to the handler.
func TestAVerifiedRequestCarriesItsPrincipal(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()

	var got auth.Principal
	h := auth.Middleware(verifier(ring, now), http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			p, ok := auth.From(r.Context())
			if !ok {
				t.Error("the handler received no principal")
			}
			got = p
		}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request("Bearer "+m.token(t, nil, goodClaims(now))))
	if got.Surface != auth.SurfaceLive || got.UID != "uid-123" {
		t.Errorf("the handler saw %+v", got)
	}
}

// A genuinely valid token for the wrong app. GIP verifies it happily — it is a
// real token for a real person — and that person is not a manager.
func TestATokenForAnotherAppIsRefusedByTheRoute(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()

	var hit bool
	h := auth.Middleware(verifier(ring, now),
		auth.RequireSurface(auth.SurfaceOps, ok(&hit)))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, request("Bearer "+m.token(t, nil, goodClaims(now)))) // a live token
	if hit {
		t.Error("a tenant reached an Ops-only route")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("answered %d, want 403 — the token is valid and the person is not a manager", rec.Code)
	}

	// The right surface passes.
	c := goodClaims(now)
	c["firebase"] = map[string]any{"tenant": "dwellm8-ops", "sign_in_provider": "phone"}
	rec = httptest.NewRecorder()
	hit = false
	h.ServeHTTP(rec, request("Bearer "+m.token(t, nil, c)))
	if !hit || rec.Code != http.StatusOK {
		t.Errorf("a manager was refused their own app: %d", rec.Code)
	}
}

// Staff reach every surface, and only staff reach an internal route.
func TestStaffCrossSurfacesAndNobodyElseReachesInternalRoutes(t *testing.T) {
	m, ring := newMint(t)
	now := time.Now()

	staff := goodClaims(now)
	staff["firebase"] = map[string]any{"sign_in_provider": "google.com"}

	var hit bool
	surfaceRoute := auth.Middleware(verifier(ring, now),
		auth.RequireSurface(auth.SurfaceOps, ok(&hit)))
	rec := httptest.NewRecorder()
	surfaceRoute.ServeHTTP(rec, request("Bearer "+m.token(t, nil, staff)))
	if !hit || rec.Code != http.StatusOK {
		t.Errorf("staff were refused a surface route: %d", rec.Code)
	}

	internal := auth.Middleware(verifier(ring, now), auth.RequireStaff(ok(&hit)))

	hit = false
	rec = httptest.NewRecorder()
	internal.ServeHTTP(rec, request("Bearer "+m.token(t, nil, goodClaims(now)))) // a tenant
	if hit {
		t.Error("a tenant reached an internal route")
	}
	if rec.Code != http.StatusForbidden {
		t.Errorf("answered %d, want 403", rec.Code)
	}

	hit = false
	rec = httptest.NewRecorder()
	internal.ServeHTTP(rec, request("Bearer "+m.token(t, nil, staff)))
	if !hit || rec.Code != http.StatusOK {
		t.Errorf("staff were refused an internal route: %d", rec.Code)
	}
}
