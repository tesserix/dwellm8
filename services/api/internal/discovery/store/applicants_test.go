package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/discovery/store"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// The applicant pack against PostgreSQL (#258): a draft that survives being
// half-finished, a correction that supersedes rather than overwrites what the
// manager saw, the people who will live there, and the boundary #103 draws —
// a pack belongs to the owner who collected it and to nobody else.

func packFixture(t *testing.T) (*store.Applicants, *store.Applications, string) {
	t.Helper()
	pool, plat := pools(t)
	apps, prospects, listing := applicationsFixture(t)
	applicant := verifiedGuest(t, prospects)
	moveIn := time.Now().AddDate(0, 1, 0).Truncate(24 * time.Hour)
	a, err := apps.Apply(context.Background(), listing, applicant.ID, moveIn, 11, nil, "")
	if err != nil {
		t.Fatalf("applying: %v", err)
	}
	return store.NewApplicants(pool, plat), apps, a.ID
}

func TestAPackStartsAsADraftAndSurvivesBeingHalfFinished(t *testing.T) {
	packs, _, application := packFixture(t)

	saved, err := packs.SaveProfile(owner(), application, store.Profile{
		FullName: "Meera Menon", Occupants: 2,
	})
	if err != nil {
		t.Fatalf("saving the pack: %v", err)
	}
	if saved.State != "draft" {
		t.Fatalf("state = %q, want draft", saved.State)
	}

	got, err := packs.Profile(owner(), application)
	if err != nil {
		t.Fatalf("reading the pack back: %v", err)
	}
	if got.FullName != "Meera Menon" || got.Occupants != 2 {
		t.Fatalf("pack = %+v; want the half-finished draft as saved", got)
	}
}

func TestACorrectionSupersedesWhatTheManagerSaw(t *testing.T) {
	packs, _, application := packFixture(t)

	first, err := packs.SaveProfile(owner(), application, store.Profile{FullName: "Meera Menon"})
	if err != nil {
		t.Fatalf("saving: %v", err)
	}
	second, err := packs.SaveProfile(owner(), application, store.Profile{
		FullName: "Meera Menon", TaxResidency: "non_resident",
	})
	if err != nil {
		t.Fatalf("correcting: %v", err)
	}
	if second.ID == first.ID {
		t.Fatal("a correction reused the row it replaced; ADR-0008 wants a new row")
	}
	if second.Corrects != first.ID {
		t.Fatalf("corrects = %q, want %q", second.Corrects, first.ID)
	}

	live, err := packs.Profile(owner(), application)
	if err != nil || live.ID != second.ID {
		t.Fatalf("live pack = %q, %v; want the correction %s", live.ID, err, second.ID)
	}
}

func TestSubmittingClosesTheDraft(t *testing.T) {
	packs, _, application := packFixture(t)

	if _, err := packs.SaveProfile(owner(), application, store.Profile{FullName: "Meera Menon"}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if err := packs.Submit(owner(), application); err != nil {
		t.Fatalf("submitting: %v", err)
	}

	got, err := packs.Profile(owner(), application)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if got.State != "submitted" || got.SubmittedAt.IsZero() {
		t.Fatalf("pack = %q at %v; want submitted with a time", got.State, got.SubmittedAt)
	}
}

func TestSubmittingAPackThatWasNeverStartedIsRefused(t *testing.T) {
	packs, _, application := packFixture(t)

	if err := packs.Submit(owner(), application); !errors.Is(err, store.ErrNoProfile) {
		t.Fatalf("submitting nothing = %v, want ErrNoProfile", err)
	}
}

func TestEveryoneMovingInIsTheirOwnRow(t *testing.T) {
	packs, _, application := packFixture(t)

	if _, err := packs.SaveProfile(owner(), application, store.Profile{
		FullName: "Meera Menon", Occupants: 3,
	}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	err := packs.SavePeople(owner(), application, []store.Person{
		{Role: "co_applicant", FullName: "Rahul Menon", Relationship: "spouse", Phone: "+919847033222"},
		{Role: "dependant", FullName: "Ananya Menon", Relationship: "daughter"},
	})
	if err != nil {
		t.Fatalf("saving the household: %v", err)
	}

	people, err := packs.People(owner(), application)
	if err != nil || len(people) != 2 {
		t.Fatalf("household = %d, %v; want two rows", len(people), err)
	}
}

func TestCorrectingThePackDoesNotLoseTheHousehold(t *testing.T) {
	packs, _, application := packFixture(t)

	if _, err := packs.SaveProfile(owner(), application, store.Profile{FullName: "Meera Menon"}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if err := packs.SavePeople(owner(), application, []store.Person{
		{Role: "co_applicant", FullName: "Rahul Menon", Relationship: "spouse"},
	}); err != nil {
		t.Fatalf("saving the household: %v", err)
	}
	if _, err := packs.SaveProfile(owner(), application, store.Profile{
		FullName: "Meera Menon", TaxResidency: "non_resident",
	}); err != nil {
		t.Fatalf("correcting: %v", err)
	}

	people, err := packs.People(owner(), application)
	if err != nil || len(people) != 1 || people[0].FullName != "Rahul Menon" {
		t.Fatalf("household after a correction = %+v, %v; want the spouse still there", people, err)
	}
}

func TestAPhoneThatIsNotE164IsRefused(t *testing.T) {
	packs, _, application := packFixture(t)

	if _, err := packs.SaveProfile(owner(), application, store.Profile{FullName: "Meera Menon"}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	err := packs.SavePeople(owner(), application, []store.Person{
		{Role: "co_applicant", FullName: "Rahul Menon", Phone: "9847033222"},
	})
	if !errors.Is(err, store.ErrPhone) {
		t.Fatalf("a bare ten-digit number gave %v; ADR-0029 wants ErrPhone", err)
	}
}

func TestSubmittingTwiceSaysSoRatherThanClaimingNothingIsThere(t *testing.T) {
	packs, _, application := packFixture(t)

	if _, err := packs.SaveProfile(owner(), application, store.Profile{FullName: "Meera Menon"}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	if err := packs.Submit(owner(), application); err != nil {
		t.Fatalf("submitting: %v", err)
	}
	if err := packs.Submit(owner(), application); !errors.Is(err, store.ErrAlreadySubmitted) {
		t.Fatalf("submitting twice = %v, want ErrAlreadySubmitted", err)
	}
}

func TestAPackTheFirmDoesNotActForIsNotThere(t *testing.T) {
	packs, _, application := packFixture(t)

	if _, err := packs.SaveProfile(owner(), application, store.Profile{FullName: "Meera Menon"}); err != nil {
		t.Fatalf("saving: %v", err)
	}
	outsider := tenancy.With(context.Background(), isolationtest.OrgOutsider)
	if _, err := packs.Profile(outsider, application); !errors.Is(err, store.ErrNoProfile) {
		t.Fatalf("another owner's manager read the pack: %v", err)
	}
}
