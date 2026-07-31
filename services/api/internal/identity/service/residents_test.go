package service_test

import (
	"context"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/identity/service"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
)

// A resident session, without a database. ADR-0029 §3.
//
// What is under test is the one function every handler on the tenant surface
// calls before it reads anything: Scope. If it ever sets the organisation
// without the renter, the whole surface reads the landlord's portfolio and every
// screen still renders.

func session() service.Session {
	return service.Session{
		PartyID: "d1111111-0000-0000-0000-000000000001",
		Residencies: []service.Residency{
			{LeaseID: "lease-a", TenantID: "org-a", Organisation: "Landlord A"},
			{LeaseID: "lease-b", TenantID: "org-b", Organisation: "Landlord B"},
		},
	}
}

func TestScopeSetsTheOrganisationAndTheRenterTogether(t *testing.T) {
	s := session()
	for _, r := range s.Residencies {
		ctx := s.Scope(context.Background(), r)

		org, ok := tenancy.From(ctx)
		if !ok || org != r.TenantID {
			t.Fatalf("scoped to %q, want %q", org, r.TenantID)
		}
		party, ok := tenancy.ResidentFrom(ctx)
		if !ok || party.String() != s.PartyID {
			t.Fatalf("scoped to renter %q, want %q — an organisation set without a renter reads "+
				"the landlord's whole portfolio", party, s.PartyID)
		}
	}
}

// A lease id that is not in the session is not theirs, and the answer is a plain
// false rather than a zero Residency somebody might use anyway.
func TestALeaseNotInTheSessionIsRefused(t *testing.T) {
	s := session()
	if _, ok := s.Residency("lease-c"); ok {
		t.Fatalf("a lease the renter is not on resolved — this is the check that stands between " +
			"changing an id in a URL and reading somebody else's tenancy")
	}
	got, ok := s.Residency("lease-b")
	if !ok || got.TenantID != "org-b" {
		t.Fatalf("lease-b resolved to %+v, want Landlord B", got)
	}
}

// An empty session is not a session. A context carrying one would otherwise
// scope reads to the organisation with no renter — which is the failure this
// package exists to make impossible.
func TestAnEmptySessionIsNotFound(t *testing.T) {
	ctx := service.WithSession(context.Background(), service.Session{})
	if _, ok := service.SessionFrom(ctx); ok {
		t.Fatalf("a session with no party resolved as present")
	}
	if _, ok := service.SessionFrom(context.Background()); ok {
		t.Fatalf("a context with no session resolved as present")
	}
}
