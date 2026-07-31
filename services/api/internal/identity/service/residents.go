// Package service also resolves a renter's sign-in, which is a different
// question from an owner's. ADR-0029 §3.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/tesserix/dwellm8/services/api/internal/identity/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// A renter is not a small organisation.
//
// Resolver above turns a sign-in into *one* organisation and refuses when there
// are several, which is right for a person who works for a firm and wrong for a
// person who rents two flats: their two landlords are two organisations, the
// answer is both, and neither may learn of the other. So the resident path
// resolves to a set of tenancies rather than to a tenant, and the organisation
// is chosen per lease, one scoped read at a time.

// Residency is one tenancy a renter is on.
//
// Declared here rather than re-exported from the store so that a surface reading
// it does not name another module's persistence types. ADR-0001 §3.
type Residency struct {
	LeaseID      string
	TenantID     tenancy.ID
	Organisation string
	State        string
}

// Session is who a resident request is for, and everything it may reach.
//
// The residency list is resolved once per request and is the authorisation: a
// lease id that is not in it is not this person's, and no query is issued for
// it. PostgreSQL refuses the same thing independently (ADR-0029), so a bug here
// is a wrong answer rather than a disclosure.
type Session struct {
	PartyID     string
	Phone       string
	Residencies []Residency
}

// Residency returns the tenancy with this id, and whether it is one of theirs.
func (s Session) Residency(leaseID string) (Residency, bool) {
	for _, r := range s.Residencies {
		if r.LeaseID == leaseID {
			return r, true
		}
	}
	return Residency{}, false
}

// Scope returns a context scoped to one of the renter's tenancies: the
// landlord's organisation, narrowed to this renter.
//
// Both settings, always together. A context carrying the organisation without
// the renter would read the landlord's whole portfolio, and that is precisely
// the mistake this function exists so that nobody writes twice.
func (s Session) Scope(ctx context.Context, r Residency) context.Context {
	return tenancy.WithResident(tenancy.With(ctx, r.TenantID), tenancy.ResidentID(s.PartyID))
}

type residentKey struct{}

// WithSession puts a resident session in a context.
func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, residentKey{}, s)
}

// SessionFrom reads the resident session, and reports whether there is one.
func SessionFrom(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(residentKey{}).(Session)
	return s, ok && s.PartyID != ""
}

// Residents resolves renters. It is also the seam the lease module reaches
// identity through when it needs a party id for a mobile number.
type Residents struct {
	principals *store.Principals
	log        *slog.Logger

	// impersonate is the development mobile number. See config.Identity.
	impersonate string
}

// NewResidents builds the service.
func NewResidents(p *store.Principals, log *slog.Logger) *Residents {
	return &Residents{principals: p, log: log}
}

// Impersonating returns a copy that resolves every request to one renter, named
// by mobile number, for use while authentication is off.
//
// The number must belong to somebody already named on a tenancy: this resolves
// an existing renter and never creates one, so a misconfigured dev environment
// fails to serve rather than inventing a person.
func (r *Residents) Impersonating(phone string) *Residents {
	out := *r
	out.impersonate = phone
	return &out
}

// PartyFor returns the party id a mobile number identifies, pre-registering the
// renter if no tenancy has ever named them.
//
// Called when a lease is written, which is the moment a renter acquires an
// identity in this product — weeks before they see it.
func (r *Residents) PartyFor(ctx context.Context, phone string) (string, error) {
	return r.principals.EnsureResident(ctx, phone)
}

// ErrNoTenancy is a verified renter who is on no lease.
//
// Distinct from a refusal: the sign-in is genuine and there is simply nothing
// for them here, which is what a person sees if their landlord has not yet
// entered them or has entered a different number.
var ErrNoTenancy = errors.New("identity: this sign-in is on no tenancy")

// Resolve turns a verified Live principal into a session.
func (r *Residents) Resolve(ctx context.Context, p auth.Principal) (Session, error) {
	person, err := r.principals.ClaimResident(ctx, p)
	if err != nil {
		return Session{}, err
	}
	return r.sessionFor(ctx, person.PartyID, person.Phone)
}

func (r *Residents) sessionFor(ctx context.Context, partyID, phone string) (Session, error) {
	residencies, err := r.principals.Residencies(ctx, partyID)
	if err != nil {
		return Session{}, err
	}
	s := Session{PartyID: partyID, Phone: phone}
	for _, res := range residencies {
		s.Residencies = append(s.Residencies, Residency{
			LeaseID: res.LeaseID, TenantID: res.TenantID,
			Organisation: res.Organisation, State: res.State,
		})
	}
	if len(s.Residencies) == 0 {
		return Session{}, ErrNoTenancy
	}
	return s, nil
}

// Middleware attaches the resident session, and refuses everything that is not
// one.
//
// Runs after auth.Middleware and instead of Resolver.Middleware — never after
// it. Resolver answers 409 for a person in two organisations, which for a renter
// with two landlords is the ordinary case rather than an error.
func (r *Residents) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		session, err := r.resolveRequest(req)
		switch {
		case err == nil:
			next.ServeHTTP(w, req.WithContext(WithSession(req.Context(), session)))
		case errors.Is(err, errImpersonationOff):
			// Development, misconfigured. Not 401: nothing is wrong with the
			// request, and serving an arbitrary renter would be the real fault.
			r.log.Error("the tenant surface has no way to identify a renter",
				"path", req.URL.Path)
			writeJSON(w, http.StatusServiceUnavailable,
				"the tenant surface is not configured on this deployment")
		case errors.Is(err, store.ErrUnknownPrincipal), errors.Is(err, ErrNoTenancy):
			// One answer for both, and it is the same one an unknown lease gets.
			// "You are not on a tenancy" and "your landlord entered a different
			// number" are the same fact to the person reading it, and telling
			// them apart tells a prober which numbers are tenants.
			r.log.Info("a verified sign-in reached the tenant surface with no tenancy",
				"surface", "live")
			writeJSON(w, http.StatusForbidden, "there is no tenancy on this number")
		case errors.Is(err, errNotLive):
			writeJSON(w, http.StatusForbidden, "this app is not the one you signed into")
		case errors.Is(err, auth.ErrNoPrincipal):
			writeJSON(w, http.StatusUnauthorized, "sign in again")
		default:
			r.log.Error("resolving a renter", "error", err)
			writeJSON(w, http.StatusInternalServerError, "could not resolve the sign-in")
		}
	})
}

var (
	errNotLive          = errors.New("identity: that token is not from the tenant app")
	errImpersonationOff = errors.New("identity: no renter to impersonate")
)

func (r *Residents) resolveRequest(req *http.Request) (Session, error) {
	if r.impersonate != "" {
		r.log.Warn("impersonating a renter — authentication is not enforced",
			"phone", r.impersonate, "path", req.URL.Path)
		party, err := r.principals.EnsureResident(req.Context(), r.impersonate)
		if err != nil {
			return Session{}, err
		}
		return r.sessionFor(req.Context(), party, r.impersonate)
	}

	p, ok := auth.From(req.Context())
	if !ok {
		// Unauthenticated and not impersonating. Reached when the resident routes
		// are mounted without authentication in front of them, which is a wiring
		// mistake and must not degrade into an open surface.
		return Session{}, errImpersonationOff
	}
	// Staff are refused rather than admitted: a support agent is not a renter,
	// and reading a tenant's money on their behalf is tenancy.Support()'s audited
	// path rather than an unrecorded one through this middleware.
	if p.Staff || p.Surface != auth.SurfaceLive {
		return Session{}, errNotLive
	}
	return r.Resolve(req.Context(), p)
}

func writeJSON(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
