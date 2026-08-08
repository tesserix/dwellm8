package ops_test

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/tesserix/dwellm8/services/api/internal/platform/tenancy/isolationtest"
)

// What a property is like and what is near it (#354) — the answers a renter
// asks for before the rent, entered by the manager who stood in the flat.
//
// These tests commit, and a place cannot be deleted, so each run names its
// places uniquely rather than colliding with what the last run left.

func placeName(prefix string) string {
	return fmt.Sprintf("%s %d", prefix, time.Now().UnixNano()%1_000_000)
}

type placeList struct {
	Places []struct {
		ID         string   `json:"id"`
		Category   string   `json:"category"`
		Name       string   `json:"name"`
		DistanceM  int      `json:"distance_m"`
		TravelMode string   `json:"travel_mode"`
		Tags       []string `json:"tags"`
		Note       string   `json:"note"`
	} `json:"places"`
}

func TestTheFirmDescribesABuildingAndReadsItBack(t *testing.T) {
	mux := serve(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPut,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/detail",
		`{"about":"A quiet block set back from the road.","amenities":["lift","power_backup"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("describing the building: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+isolationtest.PropertyGranted)
	if w.Code != http.StatusOK {
		t.Fatalf("reading the property: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Property struct {
			About     string   `json:"about"`
			Amenities []string `json:"amenities"`
		} `json:"property"`
	}
	decode(t, w, &out)
	if out.Property.About != "A quiet block set back from the road." {
		t.Errorf("about = %q, want what was written", out.Property.About)
	}
	if len(out.Property.Amenities) != 2 {
		t.Errorf("amenities = %v, want the two that were set", out.Property.Amenities)
	}
}

func TestAnAmenityNobodyCanSearchForIsRefused(t *testing.T) {
	mux := serve(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPut,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/detail",
		`{"amenities":["helipad"]}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown amenity should be refused as the manager's mistake, got %d %s",
			w.Code, w.Body.String())
	}
}

func TestTheFirmDescribesAFlatAndReadsItBack(t *testing.T) {
	mux := serve(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPut,
		"/v1/ops/units/"+isolationtest.UnitGrantedA+"/detail",
		`{"about":"Living room over the park.","features":["modular_kitchen"],
		  "bathrooms":2,"balconies":1,"covered_parking":1,
		  "facing":"north","furnishing":"semi_furnished"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("describing the flat: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/units/"+isolationtest.UnitGrantedA)
	if w.Code != http.StatusOK {
		t.Fatalf("reading the unit: %d %s", w.Code, w.Body.String())
	}
	var out struct {
		Unit struct {
			About      string   `json:"about"`
			Features   []string `json:"features"`
			Bathrooms  *int     `json:"bathrooms"`
			Facing     string   `json:"facing"`
			Furnishing string   `json:"furnishing"`
		} `json:"unit"`
	}
	decode(t, w, &out)
	if out.Unit.Bathrooms == nil || *out.Unit.Bathrooms != 2 {
		t.Errorf("bathrooms = %v, want 2", out.Unit.Bathrooms)
	}
	if out.Unit.Furnishing != "semi_furnished" || out.Unit.Facing != "north" {
		t.Errorf("furnishing/facing = %q/%q", out.Unit.Furnishing, out.Unit.Facing)
	}
	if len(out.Unit.Features) != 1 {
		t.Errorf("features = %v, want the one that was set", out.Unit.Features)
	}
}

func TestAFurnishingThatIsNotOneOfTheThreeIsRefused(t *testing.T) {
	mux := serve(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPut,
		"/v1/ops/units/"+isolationtest.UnitGrantedA+"/detail", `{"furnishing":"mostly"}`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("an unknown furnishing should be refused, got %d %s", w.Code, w.Body.String())
	}
}

func TestTheFirmListsASchoolNearAPropertyNearestFirst(t *testing.T) {
	mux := serve(t)
	property := isolationtest.PropertyGranted
	near, far := placeName("Spotswood Primary"), placeName("Bayside College")

	for _, body := range []string{
		fmt.Sprintf(`{"category":"school","name":%q,"distance_m":3200,"travel_mode":"drive"}`, far),
		fmt.Sprintf(`{"category":"school","name":%q,"distance_m":600,"travel_mode":"walk",
		             "tags":["government","primary"]}`, near),
	} {
		w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
			"/v1/ops/properties/"+property+"/places", body)
		if w.Code != http.StatusCreated {
			t.Fatalf("adding a school: %d %s", w.Code, w.Body.String())
		}
	}

	w := call(t, mux, isolationtest.OrgOwner, http.MethodGet,
		"/v1/ops/properties/"+property+"/places")
	if w.Code != http.StatusOK {
		t.Fatalf("reading what is nearby: %d %s", w.Code, w.Body.String())
	}
	var out placeList
	decode(t, w, &out)

	var seenNear, seenFar int
	for i, p := range out.Places {
		switch p.Name {
		case near:
			seenNear = i + 1
			if p.DistanceM != 600 || p.TravelMode != "walk" || len(p.Tags) != 2 {
				t.Errorf("the near school reads back as filed, got %+v", p)
			}
		case far:
			seenFar = i + 1
		}
	}
	if seenNear == 0 || seenFar == 0 {
		t.Fatalf("both schools should be listed, got %+v", out.Places)
	}
	if seenNear > seenFar {
		t.Errorf("the 600 m walk should be listed before the 3.2 km drive")
	}
}

func TestTheSameSchoolIsNotListedTwice(t *testing.T) {
	mux := serve(t)
	name := placeName("Corner High")
	body := fmt.Sprintf(`{"category":"school","name":%q,"distance_m":800}`, name)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/places", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("adding the school: %d %s", w.Code, w.Body.String())
	}

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/places", body)
	if w.Code != http.StatusConflict {
		t.Fatalf("a repeat should be named as one, got %d %s", w.Code, w.Body.String())
	}
}

func TestADistanceBeyondTheCityIsNotNearby(t *testing.T) {
	mux := serve(t)

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/properties/"+isolationtest.PropertyGranted+"/places",
		fmt.Sprintf(`{"category":"airport","name":%q,"distance_m":90000}`, placeName("Far")))
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("90 km is not nearby, got %d %s", w.Code, w.Body.String())
	}
}

func TestAPlaceIsCorrectedAndThenRetired(t *testing.T) {
	mux := serve(t)
	property := isolationtest.PropertyGranted
	name := placeName("Metro")

	w := callWith(t, mux, isolationtest.OrgOwner, http.MethodPost,
		"/v1/ops/properties/"+property+"/places",
		fmt.Sprintf(`{"category":"metro","name":%q,"distance_m":2000}`, name))
	if w.Code != http.StatusCreated {
		t.Fatalf("adding: %d %s", w.Code, w.Body.String())
	}
	var made struct {
		Place struct {
			ID string `json:"id"`
		} `json:"place"`
	}
	decode(t, w, &made)
	if made.Place.ID == "" {
		t.Fatal("the place that was added has no id to correct it by")
	}

	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPatch,
		"/v1/ops/places/"+made.Place.ID,
		fmt.Sprintf(`{"category":"metro","name":%q,"distance_m":800,"travel_mode":"walk"}`, name))
	if w.Code != http.StatusOK {
		t.Fatalf("correcting: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/properties/"+property+"/places")
	var out placeList
	decode(t, w, &out)
	for _, p := range out.Places {
		if p.ID == made.Place.ID && p.DistanceM != 800 {
			t.Errorf("distance = %d, want the corrected 800", p.DistanceM)
		}
	}

	// A correction names what changed. Re-measuring the walk must not cost the
	// place its name, its kind or its tags.
	w = callWith(t, mux, isolationtest.OrgOwner, http.MethodPatch,
		"/v1/ops/places/"+made.Place.ID, `{"distance_m":950}`)
	if w.Code != http.StatusOK {
		t.Fatalf("correcting the distance alone: %d %s", w.Code, w.Body.String())
	}
	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/properties/"+property+"/places")
	decode(t, w, &out)
	for _, p := range out.Places {
		if p.ID != made.Place.ID {
			continue
		}
		if p.DistanceM != 950 || p.Name != name || p.Category != "metro" {
			t.Errorf("after correcting the distance: %+v", p)
		}
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodDelete, "/v1/ops/places/"+made.Place.ID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("retiring: %d %s", w.Code, w.Body.String())
	}

	w = call(t, mux, isolationtest.OrgOwner, http.MethodGet, "/v1/ops/properties/"+property+"/places")
	decode(t, w, &out)
	for _, p := range out.Places {
		if p.ID == made.Place.ID {
			t.Fatal("a retired place is still listed to a renter")
		}
	}
}
