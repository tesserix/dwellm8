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

func TestPlatformRequiresAReason(t *testing.T) {
	// An exemption that leaves no trace is a back door, so the reason is
	// required by the signature rather than by convention.
	err := Platform(context.Background(), PlatformPool{}, "", func(context.Context, pgx.Tx) error { return nil })
	if err == nil {
		t.Fatal("Platform() accepted an empty reason")
	}
}
