// Package auth verifies Google Identity Platform tokens and turns one into a
// principal this API will act for. ADR-0027.
//
// # One user pool per surface
//
// Identity Platform's own multi-tenancy keeps the apps apart: a GIP tenant per
// surface, so the same phone number signing into Own and into Live is two
// separate user records with no way to reach each other. That isolation is
// Google's rather than ours, which matters — a claim we set is a claim we can
// get wrong, and the failure mode of getting it wrong is a tenant signing into
// the manager app.
//
// Dwellm8's own staff authenticate at the **project** level, outside every
// tenant. That is the product-owner exception, and it is visible in the token: a
// staff token carries no tenant claim at all, which is a thing GIP will not mint
// for anybody who signed in through a surface.
//
// # No SDK
//
// This service has one direct dependency and it is a database driver. Verifying
// a GIP token needs RS256 over Google's published certificates, an issuer, an
// audience and an expiry — that is stdlib crypto and about a hundred lines, and
// the alternative is the Firebase Admin SDK dragging in google.golang.org/api,
// gRPC and OpenCensus for it. See ADR-0027 §4 for the argument and for what we
// take on by writing it: the checks below are the ones a JWT verifier gets
// wrong, so each is a named test.
package auth

import (
	"fmt"
	"slices"
	"strings"
)

// Surface is one of the product's front doors. Each has its own GIP tenant and
// therefore its own user base.
type Surface string

const (
	// SurfaceOwn is the landlord app.
	SurfaceOwn Surface = "own"
	// SurfaceOps is the management-firm app.
	SurfaceOps Surface = "ops"
	// SurfaceLive is the tenant app.
	SurfaceLive Surface = "live"
	// SurfaceFind is the public marketplace, where a prospect signs in before
	// they are anybody's tenant.
	SurfaceFind Surface = "find"
	// SurfacePro is the vendor and technician app.
	SurfacePro Surface = "pro"
	// SurfaceAdmin is the internal console. Its GIP tenant exists for the same
	// reason the others do; Dwellm8 staff acting *across* organisations do not
	// use it, they authenticate at project level. See Staff.
	SurfaceAdmin Surface = "admin"
)

// Surfaces returns every surface, ordered.
func Surfaces() []Surface {
	return []Surface{SurfaceAdmin, SurfaceFind, SurfaceLive, SurfaceOps, SurfaceOwn, SurfacePro}
}

func (s Surface) Valid() bool { return slices.Contains(Surfaces(), s) }

// TenantID is the GIP tenant a surface's users live in.
//
// Derived rather than configured, so a deployment cannot point two surfaces at
// one pool by editing a values file — which would silently merge two user bases
// and be discovered by a tenant seeing a manager's home screen.
func (s Surface) TenantID(prefix string) string {
	return prefix + "-" + string(s)
}

// SurfaceFor maps a GIP tenant id back to the surface it belongs to, and reports
// whether it is one of ours at all.
//
// A token from a tenant we do not recognise is refused rather than treated as
// tenantless — a tenantless token is staff, and quietly promoting an unknown
// tenant to staff is the worst available failure.
func SurfaceFor(tenantID, prefix string) (Surface, error) {
	for _, s := range Surfaces() {
		want := s.TenantID(prefix)
		// GIP mints tenant ids as <displayName>-<random>, so the id a token
		// carries is dwellm8-own-x8f3a, never bare dwellm8-own. The separator
		// is required: dwellm8-own matches and dwellm8-ownx must not, because
		// only project admins can create tenants and the names they create are
		// exactly the derived ones.
		if tenantID == want || strings.HasPrefix(tenantID, want+"-") {
			return s, nil
		}
	}
	return "", fmt.Errorf("%w: %q is not a Dwellm8 user pool", ErrToken, tenantID)
}

// Principal is who the request is for.
//
// It is not yet an organisation: a GIP uid identifies a person and Dwellm8's
// tenancy is by organisation, and the step between them is a database lookup
// that this package deliberately does not perform. Verification says who signed
// in; membership says what they may act as, and conflating them is how a valid
// token becomes an authorisation.
type Principal struct {
	// UID is the GIP user id, unique within its tenant and not across tenants.
	UID string
	// Surface is which app they signed into. Empty for staff.
	Surface Surface
	// Staff is a Dwellm8 employee, authenticated at project level with no tenant.
	Staff bool
	// Phone is the verified number, where the sign-in was by phone. The
	// identifier Indian rental actually runs on.
	Phone string
	// Email is the verified address, where sign-in was by Google or Apple.
	Email string
	// SignInProvider is "phone", "google.com" or "apple.com" — recorded because
	// a support conversation starts with how somebody got in.
	SignInProvider string
}

// Key is how a principal is stored: a uid is unique only within its pool, so a
// uid on its own is not an identity.
func (p Principal) Key() string {
	if p.Staff {
		return "staff:" + p.UID
	}
	return string(p.Surface) + ":" + p.UID
}
