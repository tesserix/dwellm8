package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy"
	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
	"github.com/tesserix/dwellm8/services/api/internal/property/domain"
	"github.com/tesserix/dwellm8/services/api/internal/property/store"
)

// What a property is like, and what is near it (#354). A renter asks this
// before they ask the rent, and until now the record had no answer.

func TestPropertyDetail(t *testing.T) {
	req, plat := ownershipPools(t)
	isolationtest.SeedPropertyTree(t, plat)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	s := store.New(req)

	t.Run("what a manager writes about the building reads back", func(t *testing.T) {
		want := domain.PropertyDetail{
			About:     "A quiet block set back from the main road, with the market at the corner.",
			Amenities: []string{"lift", "power_backup", "security"},
		}
		if err := s.SetPropertyDetail(ctx, isolationtest.PropertyGranted, want); err != nil {
			t.Fatalf("setting detail: %v", err)
		}

		got, err := s.Get(ctx, isolationtest.PropertyGranted)
		if err != nil {
			t.Fatalf("reading back: %v", err)
		}
		if got.About != want.About {
			t.Errorf("about = %q, want %q", got.About, want.About)
		}
		if len(got.Amenities) != 3 || got.Amenities[0] != "lift" {
			t.Errorf("amenities = %v, want the three that were set", got.Amenities)
		}
	})

	t.Run("an amenity the vocabulary does not know is refused", func(t *testing.T) {
		err := s.SetPropertyDetail(ctx, isolationtest.PropertyGranted, domain.PropertyDetail{
			Amenities: []string{"helipad"},
		})
		if !errors.Is(err, store.ErrNotAllowed) {
			t.Fatalf("an unknown amenity should be refused as a value the vocabulary lacks, got %v", err)
		}
	})
}

func TestUnitDetail(t *testing.T) {
	req, plat := ownershipPools(t)
	isolationtest.SeedPropertyTree(t, plat)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	s := store.New(req)

	t.Run("what a manager writes about the flat reads back", func(t *testing.T) {
		two := 2
		want := domain.UnitDetail{
			About:          "North facing, with the living room over the park.",
			Features:       []string{"modular_kitchen", "wardrobes"},
			Bathrooms:      &two,
			CoveredParking: &two,
			Facing:         "north",
			Furnishing:     "semi_furnished",
		}
		if err := s.SetUnitDetail(ctx, isolationtest.UnitGrantedA, want); err != nil {
			t.Fatalf("setting unit detail: %v", err)
		}

		got, err := s.Unit(ctx, isolationtest.UnitGrantedA)
		if err != nil {
			t.Fatalf("reading back: %v", err)
		}
		if got.About != want.About {
			t.Errorf("about = %q, want %q", got.About, want.About)
		}
		if got.Bathrooms == nil || *got.Bathrooms != 2 {
			t.Errorf("bathrooms = %v, want 2", got.Bathrooms)
		}
		if got.Furnishing != "semi_furnished" || got.Facing != "north" {
			t.Errorf("furnishing/facing = %q/%q, want semi_furnished/north", got.Furnishing, got.Facing)
		}
		if len(got.Features) != 2 {
			t.Errorf("features = %v, want two", got.Features)
		}
	})

	t.Run("a furnishing that is not one of the three is refused", func(t *testing.T) {
		err := s.SetUnitDetail(ctx, isolationtest.UnitGrantedB, domain.UnitDetail{
			Furnishing: "mostly",
		})
		if !errors.Is(err, store.ErrNotAllowed) {
			t.Fatalf("an unknown furnishing should be refused, got %v", err)
		}
	})
}

// These fixtures commit, and the schema forbids deleting a place, so a second
// run starts with the first run's rows still there. Retiring them is the
// product's own way to clear a board.
func clearPlaces(t *testing.T, ctx context.Context, s *store.Properties, propertyID string) {
	t.Helper()
	places, err := s.Places(ctx, propertyID)
	if err != nil {
		t.Fatalf("reading places to clear: %v", err)
	}
	for _, p := range places {
		if err := s.RetirePlace(ctx, p.ID); err != nil {
			t.Fatalf("clearing %s: %v", p.Name, err)
		}
	}
}

func TestPropertyPlaces(t *testing.T) {
	req, plat := ownershipPools(t)
	isolationtest.SeedPropertyTree(t, plat)
	ctx := tenancy.With(context.Background(), isolationtest.OrgOwner)
	s := store.New(req)
	property := isolationtest.PropertyOther
	clearPlaces(t, ctx, s, property)

	t.Run("nearest first, so the answer is the one that is asked for", func(t *testing.T) {
		far := domain.Place{
			Category: "school", Name: "Bayside P-12 College", DistanceM: 3200,
			TravelMode: "drive", Tags: []string{"government", "combined", "coed"},
		}
		near := domain.Place{
			Category: "school", Name: "Spotswood Primary School", DistanceM: 1000,
			TravelMode: "walk", Tags: []string{"government", "primary"},
		}
		for _, p := range []domain.Place{far, near} {
			if _, err := s.AddPlace(ctx, property, p); err != nil {
				t.Fatalf("adding %s: %v", p.Name, err)
			}
		}

		got, err := s.Places(ctx, property)
		if err != nil {
			t.Fatalf("reading places: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("places = %d, want 2", len(got))
		}
		if got[0].Name != near.Name {
			t.Errorf("first = %q, want the nearer %q", got[0].Name, near.Name)
		}
		if len(got[0].Tags) != 2 {
			t.Errorf("tags = %v, want the two that were set", got[0].Tags)
		}
	})

	t.Run("the same school is not listed twice", func(t *testing.T) {
		p := domain.Place{Category: "school", Name: "  spotswood primary school ", DistanceM: 900}
		if _, err := s.AddPlace(ctx, property, p); !errors.Is(err, store.ErrPlaceExists) {
			t.Fatalf("a duplicate should be named as one, not as a database error, got %v", err)
		}
	})

	t.Run("a distance beyond the city is refused", func(t *testing.T) {
		p := domain.Place{Category: "airport", Name: "Kempegowda", DistanceM: 90000}
		if _, err := s.AddPlace(ctx, property, p); !errors.Is(err, store.ErrNotAllowed) {
			t.Fatalf("90 km should be refused as not nearby, got %v", err)
		}
	})

	t.Run("a place that closes is retired, not deleted, and stops being listed", func(t *testing.T) {
		added, err := s.AddPlace(ctx, property,
			domain.Place{Category: "clinic", Name: "Corner Clinic", DistanceM: 300})
		if err != nil {
			t.Fatalf("adding: %v", err)
		}

		if err := s.RetirePlace(ctx, added.ID); err != nil {
			t.Fatalf("retiring: %v", err)
		}

		got, err := s.Places(ctx, property)
		if err != nil {
			t.Fatalf("reading places: %v", err)
		}
		for _, p := range got {
			if p.ID == added.ID {
				t.Fatal("a retired place is still being listed")
			}
		}
	})

	t.Run("a distance that was measured wrong can be corrected", func(t *testing.T) {
		added, err := s.AddPlace(ctx, property,
			domain.Place{Category: "metro", Name: "Indiranagar", DistanceM: 2000})
		if err != nil {
			t.Fatalf("adding: %v", err)
		}

		added.DistanceM = 800
		added.TravelMode = "walk"
		if err := s.UpdatePlace(ctx, added); err != nil {
			t.Fatalf("correcting: %v", err)
		}

		got, err := s.Places(ctx, property)
		if err != nil {
			t.Fatalf("reading places: %v", err)
		}
		for _, p := range got {
			if p.ID == added.ID && p.DistanceM != 800 {
				t.Errorf("distance = %d, want the corrected 800", p.DistanceM)
			}
		}
	})
}
