package service_test

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/identity/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/auth"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// The impersonating resolver is a development-only escape hatch: every
// request acts as one fixed person in one fixed organisation. Its middleware
// path never touches the principals store, which is what lets it be tested
// without a database — and what makes it worth testing carefully, since it is
// the one place tenancy can be acquired by configuration alone.

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestImpersonationNeedsAnOrganisationWithNoDefault(t *testing.T) {
	_, err := service.NewImpersonatingResolver(service.Impersonation{}, quietLog())
	if err == nil {
		t.Fatal("expected an error for impersonation with no organisation configured")
	}
}

func TestImpersonationDefaultsToTheOwnSurface(t *testing.T) {
	r, err := service.NewImpersonatingResolver(
		service.Impersonation{TenantID: tenancy.ID("org-1"), PartyID: "party-1"}, quietLog())
	if err != nil {
		t.Fatalf("NewImpersonatingResolver: %v", err)
	}

	var gotTenant tenancy.ID
	var gotPrincipal auth.Principal
	var sawTenant, sawPrincipal bool
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotTenant, sawTenant = tenancy.From(req.Context())
		gotPrincipal, sawPrincipal = auth.From(req.Context())
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Middleware(next).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if !sawTenant || gotTenant != tenancy.ID("org-1") {
		t.Fatalf("tenant in context = %q, ok=%v, want org-1", gotTenant, sawTenant)
	}
	if !sawPrincipal || gotPrincipal.UID != "party-1" || gotPrincipal.Surface != auth.SurfaceOwn {
		t.Fatalf("principal in context = %+v, ok=%v, want UID party-1 and surface own", gotPrincipal, sawPrincipal)
	}
}

func TestImpersonationCarriesTheConfiguredSurface(t *testing.T) {
	r, err := service.NewImpersonatingResolver(service.Impersonation{
		TenantID: tenancy.ID("org-2"), PartyID: "party-2", Surface: auth.SurfacePro,
	}, quietLog())
	if err != nil {
		t.Fatalf("NewImpersonatingResolver: %v", err)
	}

	var gotPrincipal auth.Principal
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		gotPrincipal, _ = auth.From(req.Context())
		w.WriteHeader(http.StatusOK)
	})

	rr := httptest.NewRecorder()
	r.Middleware(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if gotPrincipal.Surface != auth.SurfacePro {
		t.Fatalf("surface = %q, want %q", gotPrincipal.Surface, auth.SurfacePro)
	}
}

// The ordinary resolver runs after auth.Middleware, so a request that reaches
// it with no verified principal is unreachable in production and refused
// rather than assumed here.
func TestNoVerifiedPrincipalIsRefusedWithoutTouchingTheStore(t *testing.T) {
	r := service.NewResolver(nil, quietLog())

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { called = true })

	rr := httptest.NewRecorder()
	r.Middleware(next).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if called {
		t.Fatal("the next handler ran for a request with no verified principal")
	}
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rr.Code)
	}
}
