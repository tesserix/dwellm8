package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// The middleware, and the line it draws.
//
// Verifying a token says *who signed in*. It does not say what they may act as:
// a GIP uid identifies a person and Dwellm8's isolation is by organisation, and
// the step between them is a membership lookup. So this middleware puts a
// Principal in the context and stops — resolving the organisation is the
// identity module's, because it is a database question and this package must not
// reach a database.
//
// Anything that skipped that step and trusted a claim for the organisation would
// be handing an attacker the tenant boundary in a token they control the
// contents of, minus the signature.

type principalKey struct{}

// With puts a principal in a context.
func With(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// From reads the principal, and reports whether the request was authenticated.
func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(principalKey{}).(Principal)
	return p, ok
}

// ErrNoPrincipal is a request that reached something needing a person without
// carrying one.
var ErrNoPrincipal = errors.New("auth: the request is not authenticated")

// Middleware verifies the bearer token and attaches the principal.
//
// Unauthenticated requests are refused here rather than passed through with an
// empty principal. A handler that has to remember to check is a handler that
// will eventually forget, and the forgetting is invisible — the endpoint keeps
// working, for everybody.
func Middleware(v *Verifier, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, ok := bearer(r)
		if !ok {
			unauthorised(w)
			return
		}
		p, err := v.Verify(r.Context(), raw)
		if err != nil {
			// One answer for every refusal. Distinguishing "expired" from "wrong
			// audience" here would tell an attacker which half of their forgery
			// worked.
			unauthorised(w)
			return
		}
		next.ServeHTTP(w, r.WithContext(With(r.Context(), p)))
	})
}

// Public wraps handlers that must stay reachable without a token — health, and
// the listing surface ADR-0019 opened deliberately. It attaches nothing.
//
// A named wrapper rather than a path exemption inside Middleware, so that making
// something public is a visible act in the route table rather than a regex
// somebody widens.
func Public(next http.Handler) http.Handler { return next }

// RequireSurface refuses a principal who signed into a different app.
//
// The pools already separate the user bases, so this is the second lock rather
// than the first: it catches a token that is genuinely valid for Live being
// presented to an Ops-only route. GIP would happily verify it — it is a real
// token for a real person — and it is not a manager.
func RequireSurface(s Surface, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := From(r.Context())
		switch {
		case !ok:
			unauthorised(w)
		case p.Staff:
			// Staff reach every surface, and every such act is audited through
			// tenancy.Support() rather than being invisible here.
			next.ServeHTTP(w, r)
		case p.Surface != s:
			forbidden(w, "this app is not the one you signed into")
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// RequireStaff refuses anybody who signed in through a surface.
//
// Not a claim check: a surface token carries a tenant and a staff token does
// not, and GIP will not mint a tenantless token for somebody who authenticated
// against a tenant. The boundary is Google's, not a boolean of ours.
func RequireStaff(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := From(r.Context())
		if !ok {
			unauthorised(w)
			return
		}
		if !p.Staff {
			forbidden(w, "this is an internal route")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	// Case-insensitive scheme, exactly one space, and nothing else — a lenient
	// parser here is a parser that accepts something a proxy in front of it read
	// differently.
	const prefix = "bearer "
	if len(h) <= len(prefix) || !strings.EqualFold(h[:len(prefix)], prefix) {
		return "", false
	}
	token := h[len(prefix):]
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", false
	}
	return token, true
}

func unauthorised(w http.ResponseWriter) {
	// WWW-Authenticate, because a client that does not know it needs to
	// re-authenticate retries the same expired token forever.
	w.Header().Set("WWW-Authenticate", `Bearer realm="dwellm8"`)
	write(w, http.StatusUnauthorized, "sign in again")
}

func forbidden(w http.ResponseWriter, msg string) { write(w, http.StatusForbidden, msg) }

func write(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
