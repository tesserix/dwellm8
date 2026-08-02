// Package service resolves a verified sign-in into the organisation a request
// acts for. ADR-0027 §6.
package service

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// Resolver turns a principal into a tenancy context.
//
// The second half of authentication and the half that matters for isolation:
// verification says who signed in, and this says which organisation's rows they
// may touch. It is a database lookup on purpose — putting it in a token claim
// would let the client's own app compose the tenancy boundary.
type Resolver struct {
	principals *store.Principals
	log        *slog.Logger

	// impersonate is the development escape hatch. See Impersonation.
	impersonate *Impersonation
}

// Impersonation makes the API usable before Identity Platform is provisioned.
//
// It is a **development-only** shim: every request is treated as one fixed
// person in one fixed organisation, so the apps and the endpoints can be built
// against each other before the six GIP tenants exist. Issue #229 replaces it.
//
// Three things keep it from becoming a back door:
//
//  1. It is only constructed when authentication is not enforced, and
//     config.validate() refuses that outside dev — the process will not start.
//  2. The organisation is explicit configuration. There is no default, so a
//     deployment cannot acquire one by forgetting to set something.
//  3. It logs a warning on every request rather than once at startup, because a
//     line in the boot log scrolls away and a line per request does not.
type Impersonation struct {
	TenantID tenancy.ID
	PartyID  string
	Surface  auth.Surface
}

// NewResolver builds the ordinary resolver.
func NewResolver(p *store.Principals, log *slog.Logger) *Resolver {
	return &Resolver{principals: p, log: log}
}

// NewImpersonatingResolver builds the development one.
//
// Constructing it is the deliberate act — a caller has to name the organisation
// every request will act as, and main() only reaches this branch when
// authentication is off.
func NewImpersonatingResolver(i Impersonation, log *slog.Logger) (*Resolver, error) {
	if i.TenantID == "" {
		return nil, errors.New("identity: impersonation needs an organisation to impersonate — " +
			"there is no default, because a deployment must not acquire one by forgetting to " +
			"configure it")
	}
	if i.Surface == "" {
		i.Surface = auth.SurfaceOwn
	}
	return &Resolver{impersonate: &i, log: log}, nil
}

// ErrNoOrganisation is a verified person who cannot act for anybody: either the
// platform has never seen them, or they belong to no organisation, or they
// belong to several and have not said which.
var ErrNoOrganisation = errors.New("identity: no single organisation for this sign-in")

// Middleware puts the organisation into the request context.
//
// Runs after auth.Middleware, which is why it can be blunt about a missing
// principal: by here the request has been verified or refused.
func (r *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if r.impersonate != nil {
			r.log.Warn("impersonating — authentication is not enforced",
				"organisation", r.impersonate.TenantID, "surface", r.impersonate.Surface,
				"path", req.URL.Path)
			ctx := tenancy.With(req.Context(), r.impersonate.TenantID)
			ctx = auth.With(ctx, auth.Principal{
				UID: r.impersonate.PartyID, Surface: r.impersonate.Surface,
			})
			next.ServeHTTP(w, req.WithContext(ctx))
			return
		}

		p, ok := auth.From(req.Context())
		if !ok {
			// Unreachable behind auth.Middleware, and worth refusing rather than
			// assuming: a resolver that treats "no principal" as "no tenancy" would
			// hand an unauthenticated request to a handler that then reads nothing
			// and reports success.
			http.Error(w, `{"error":"sign in again"}`, http.StatusUnauthorized)
			return
		}

		person, err := r.principals.Lookup(req.Context(), p)
		if errors.Is(err, store.ErrUnknownPrincipal) && p.Surface == auth.SurfaceOwn && p.Phone != "" {
			// An owner their manager onboarded before they ever signed in (#240):
			// the verified number claims the reservation, the same move a
			// renter's first sign-in makes. Only then is "unknown" final.
			if _, claimErr := r.principals.ClaimOwner(req.Context(), p); claimErr == nil {
				person, err = r.principals.Lookup(req.Context(), p)
			}
		}
		switch {
		case errors.Is(err, store.ErrUnknownPrincipal):
			// Verified by Google and unknown here: a first sign-in. The answer is
			// to onboard, which is a route rather than an error, so it is named
			// distinctly from a refusal.
			http.Error(w, `{"error":"this sign-in has no account yet","onboard":true}`,
				http.StatusForbidden)
			return
		case err != nil:
			r.log.Error("resolving a sign-in", "surface", p.Surface, "error", err)
			http.Error(w, `{"error":"could not resolve the sign-in"}`, http.StatusInternalServerError)
			return
		}

		tenantID, single := person.Organisation()
		if !single {
			// Nought or several. Several is a real case — a manager who is also a
			// landlord — and the platform must not choose for them.
			http.Error(w, `{"error":"choose which organisation to act as"}`, http.StatusConflict)
			return
		}
		next.ServeHTTP(w, req.WithContext(tenancy.With(req.Context(), tenantID)))
	})
}
