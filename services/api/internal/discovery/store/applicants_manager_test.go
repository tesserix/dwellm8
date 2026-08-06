package store_test

import (
	"context"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The pack is collected by the firm managing the property, not by the owner
// sitting at a desk (#259). Everything hanging off it has to be writable under
// the mandate, or the manager's app can only read what it cannot fill in.

func managing(t *testing.T) context.Context {
	t.Helper()
	_, plat := pools(t)
	grant := isolationtest.SeedGrant(t, plat, isolationtest.Grant{
		Grantor:     isolationtest.OrgOwner,
		Grantee:     isolationtest.OrgFirm,
		Permissions: []string{"property.read", "property.write"},
	})
	return tenancy.WithGrant(tenancy.With(context.Background(), isolationtest.OrgFirm), grant)
}

func TestTheManagerFillsInThePackUnderTheMandate(t *testing.T) {
	packs, _, application := packFixture(t)
	firm := managing(t)

	if _, err := packs.SaveProfile(firm, application, store.Profile{FullName: "Meera Menon"}); err != nil {
		t.Fatalf("the manager could not start the pack: %v", err)
	}
	if err := packs.SavePeople(firm, application, []store.Person{
		{Role: "co_applicant", FullName: "Arun Menon", Relationship: "spouse"},
	}); err != nil {
		t.Fatalf("the manager could not record the household: %v", err)
	}
	if err := packs.SaveAddresses(firm, application, []store.Address{
		indianAddress("rented", 12, -1), indianAddress("family", 62, 12),
	}); err != nil {
		t.Fatalf("the manager could not record the history: %v", err)
	}

	// Replacing hits DELETE, which the first save never does.
	if err := packs.SavePeople(firm, application, []store.Person{
		{Role: "guarantor", FullName: "K Menon", Relationship: "father"},
	}); err != nil {
		t.Fatalf("the manager could not replace the household: %v", err)
	}

	history, err := packs.Addresses(firm, application)
	if err != nil || len(history.Addresses) != 2 || !history.Complete {
		t.Fatalf("history = %+v, %v; want both rows and no gaps", history, err)
	}
	household, err := packs.People(firm, application)
	if err != nil || len(household) != 1 {
		t.Fatalf("household = %+v, %v; want only the guarantor", household, err)
	}
}
