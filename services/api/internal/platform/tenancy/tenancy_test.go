package tenancy

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestScopedRefusesWithoutATenant(t *testing.T) {
	// The point of the package: no tenant, no transaction. The database is
	// never reached, so a nil pool is safe here — and proves it.
	err := Scoped(context.Background(), nil, func(context.Context, pgx.Tx) error {
		t.Fatal("fn ran without a tenant")
		return nil
	})
	if !errors.Is(err, ErrNoTenant) {
		t.Fatalf("Scoped without a tenant = %v, want ErrNoTenant", err)
	}
}

func TestEmptyTenantIsNoTenant(t *testing.T) {
	// An empty string is the shape a missing header takes. It must not pass.
	ctx := With(context.Background(), ID(""))
	if _, ok := From(ctx); ok {
		t.Fatal("From() accepted an empty tenant")
	}
	if err := Scoped(ctx, nil, nil); !errors.Is(err, ErrNoTenant) {
		t.Fatalf("Scoped with an empty tenant = %v, want ErrNoTenant", err)
	}
}

func TestFromRoundTrips(t *testing.T) {
	ctx := With(context.Background(), ID("11111111-1111-1111-1111-111111111111"))
	got, ok := From(ctx)
	if !ok || got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("From() = %q, %v", got, ok)
	}
}

func TestGrantRoundTrips(t *testing.T) {
	ctx := WithGrant(With(context.Background(), ID("11111111-1111-1111-1111-111111111111")),
		GrantID("22222222-2222-2222-2222-222222222222"))

	// The tenant is unchanged by acting under a grant. A firm managing an
	// owner's units is still the firm; ADR-0005 is a window, not a costume.
	if got, _ := From(ctx); got != "11111111-1111-1111-1111-111111111111" {
		t.Fatalf("tenant = %q, want the acting organisation unchanged", got)
	}
	got, ok := GrantFrom(ctx)
	if !ok || got != "22222222-2222-2222-2222-222222222222" {
		t.Fatalf("GrantFrom() = %q, %v", got, ok)
	}
}

func TestNoGrantByDefault(t *testing.T) {
	ctx := With(context.Background(), ID("11111111-1111-1111-1111-111111111111"))
	if _, ok := GrantFrom(ctx); ok {
		t.Fatal("GrantFrom() found a grant in a context that was never given one")
	}
	// An empty grant is no grant: it reaches PostgreSQL as '', which
	// current_grant_id() coerces to NULL rather than failing the cast.
	if _, ok := GrantFrom(WithGrant(ctx, GrantID(""))); ok {
		t.Fatal("GrantFrom() accepted an empty grant")
	}
}

func TestPlatformRequiresAReason(t *testing.T) {
	// An exemption that leaves no trace is a back door, so the reason is
	// required by the signature rather than by convention.
	err := Platform(context.Background(), PlatformPool{}, "", func(context.Context, pgx.Tx) error { return nil })
	if err == nil {
		t.Fatal("Platform() accepted an empty reason")
	}
}
