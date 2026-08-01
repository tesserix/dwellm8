package authz

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Checker is what a Guard consults. *Client satisfies it; tests hand in fakes.
type Checker interface {
	Check(ctx context.Context, user, relation, object string) (bool, error)
}

// Check declares what a route requires: a relation, on an object resolved from
// the request. Declaration is the price of registration — Registrar has no
// method that takes a handler without one, which is what makes "a route with
// no declared authorisation" a compile error rather than a code-review hope.
type Check struct {
	Relation string
	Object   func(*http.Request) (string, error)
}

// Organisation scopes a check to the organisation the request acts for, read
// from the tenancy context the resolver bound — never from a client header.
func Organisation() func(*http.Request) (string, error) {
	return func(r *http.Request) (string, error) {
		id, ok := tenancy.From(r.Context())
		if !ok {
			return "", errors.New("no organisation in context")
		}
		return "organisation:" + string(id), nil
	}
}

// PathObject scopes a check to an object named in the route pattern, e.g.
// PathObject("agreement", "lease") on "GET /v1/tenancies/{lease}".
func PathObject(objectType, pathValue string) func(*http.Request) (string, error) {
	return func(r *http.Request) (string, error) {
		id := r.PathValue(pathValue)
		if id == "" {
			return "", errors.New("no " + pathValue + " in path")
		}
		return objectType + ":" + id, nil
	}
}

// Guard issues the check a Registrar declared, before any handler code runs.
//
// Fail closed, in every direction: no subject, no object, checker error,
// checker unreachable — all deny. When Enforce is false the guard passes
// requests through but the declarations still exist and still compile, so
// turning enforcement on is a config change, not a refactor.
//
// List endpoints do not enumerate objects through here. Lists are filtered in
// SQL under row-level security — which cannot leak existence or counts, the
// edge case dwellm8#150 names — and the guard gates the verb on the
// collection's scope instead. Batch checks arrive with the first endpoint
// that genuinely needs per-row verbs, not speculatively.
type Guard struct {
	Checker Checker
	Enforce bool
	Log     *slog.Logger
	// Cache bounds repeated checks; its TTL is the revocation contract — a
	// deleted tuple is honoured at most TTL later. Nil means no caching.
	Cache *Cache
}

// Subject is the canonical Dwellm8 user for a request: the resolved party
// where identity has bound one, else the verified principal's uid. Tuples are
// written against the same ids (dwellm8#151), which is what makes the two
// halves meet.
func Subject(ctx context.Context) (string, error) {
	if party, ok := tenancy.ResidentFrom(ctx); ok {
		return "user:" + string(party), nil
	}
	if p, ok := auth.From(ctx); ok && p.UID != "" {
		return "user:" + p.UID, nil
	}
	return "", errors.New("no principal in context")
}

// Wrap applies the guard to any http.Handler. Exported for the Gin registrar
// (platform/ginx): a second router must sit behind the same guard, not a copy
// of it.
func (g *Guard) Wrap(c Check, next http.Handler) http.Handler { return g.wrap(c, next) }

func (g *Guard) wrap(c Check, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.Enforce {
			next.ServeHTTP(w, r)
			return
		}
		user, err := Subject(r.Context())
		if err != nil {
			g.deny(w, r, c, err)
			return
		}
		object, err := c.Object(r)
		if err != nil {
			g.deny(w, r, c, err)
			return
		}
		allowed, hit := g.Cache.Get(user, c.Relation, object)
		if !hit {
			allowed, err = g.Checker.Check(r.Context(), user, c.Relation, object)
			if err != nil {
				g.deny(w, r, c, err)
				return
			}
			g.Cache.Put(user, c.Relation, object, allowed)
		}
		// The decision audit, dwellm8#154: subject, relation, object and result
		// on every enforced decision, allowed or not. A structured line rather
		// than an audit_events row for now — the row, its retention and the
		// granting-relationship trace stay #154's to land.
		if g.Log != nil {
			g.Log.Info("authz decision",
				"subject", user, "relation", c.Relation, "object", object,
				"allowed", allowed, "cached", hit)
		}
		if !allowed {
			g.deny(w, r, c, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (g *Guard) deny(w http.ResponseWriter, r *http.Request, c Check, err error) {
	// One refusal, whatever the cause — a distinguishable error is an oracle,
	// the same rule the token verifier follows (ADR-0027 §4).
	if g.Log != nil {
		g.Log.Warn("authorisation denied",
			"path", r.URL.Path, "relation", c.Relation, "error", err)
	}
	http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
}

// Registrar is how a module publishes routes. Every route states its check or
// states, in prose, why it has none — there is no third method.
type Registrar struct {
	mux   *http.ServeMux
	guard *Guard
	// Declared is exported for the wiring test: the contract that every
	// pattern carries either a relation or a reason.
	Declared []Declaration
}

type Declaration struct {
	Pattern  string
	Relation string
	Reason   string // set only for Open routes
}

func NewRegistrar(mux *http.ServeMux, g *Guard) *Registrar {
	return &Registrar{mux: mux, guard: g}
}

// Handle registers a guarded route.
func (r *Registrar) Handle(pattern string, c Check, h http.HandlerFunc) {
	r.Declared = append(r.Declared, Declaration{Pattern: pattern, Relation: c.Relation})
	r.mux.Handle(pattern, r.guard.wrap(c, h))
}

// Open registers a route that is deliberately not guarded here, and makes the
// registrant say why — "authenticated by HMAC over the delivery's own bytes"
// is a reason; an empty string panics at wiring time, before any traffic.
func (r *Registrar) Open(pattern, reason string, h http.HandlerFunc) {
	if reason == "" {
		panic("authz: an Open route must state its reason: " + pattern)
	}
	r.Declared = append(r.Declared, Declaration{Pattern: pattern, Reason: reason})
	r.mux.Handle(pattern, h)
}
